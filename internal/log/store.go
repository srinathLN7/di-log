package log

import (
	"bufio"
	"encoding/binary"
	"os"
	"sync"
)

/*
Record	 - data stored in the log
Store  	 - the file we store records
Log		 - the abstraction that ties all segments together
*/

// define the encoding that we persist record sizes and index entries in
var (
	enc = binary.BigEndian
)

// number of bytes to store each records length
const (
	lenWidth = 8
)

// Store : A simple wrapper around a file with APIs to append and read bytes to and from the file
type store struct {
	*os.File
	mu   sync.Mutex
	buf  *bufio.Writer
	size uint64
}

// newStore : create a store for the given file
func newStore(f *os.File) (*store, error) {
	fi, err := os.Stat(f.Name())
	if err != nil {
		return nil, err
	}

	size := uint64(fi.Size())
	return &store{
		File: f,
		size: size,
		buf:  bufio.NewWriter(f),
	}, nil

}

// Append : persits the given bytes to the store uses the binary.Write function to encode the length as a 64-bit unsigned integer (uint64)
// using big-endian byte order
func (s *store) Append(p []byte) (n, pos uint64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// current position where store holds the record in the file
	pos = s.size

	// write the length of the record => when reading from record, we know how many bytes to read
	if err := binary.Write(s.buf, enc, uint64(len(p))); err != nil {
		return 0, 0, err
	}

	// write to the store buffer handler instead of file directly to improve performance (reduce system calls)
	// write the actual record data to the buffer
	w, err := s.buf.Write(p)
	if err != nil {
		return 0, 0, err
	}

	// update the total no. of bytes written to file incl. the prefixed length of the byte
	w += lenWidth

	// update the new size of store after appending the records
	s.size += uint64(w)

	return uint64(w), pos, nil
}

// Read: returns the record stored at the given position
func (s *store) Read(pos uint64) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// flush the writter buffer to the disk before trying to read the data from the store
	if err := s.buf.Flush(); err != nil {
		return nil, err
	}

	// find the number of bytes we have to read to get the whole record
	// this information is present in the first byte of the encoded record
	size := make([]byte, lenWidth)
	if _, err := s.File.ReadAt(size, int64(pos)); err != nil {
		return nil, err
	}

	// convert the read bytes into `uint64` using enc.
	// size buffer contains the length of the specific record in offset `pos`
	b := make([]byte, enc.Uint64(size))
	if _, err := s.File.ReadAt(b, int64(pos+lenWidth)); err != nil {
		return nil, err
	}

	return b, nil
}

// ReadAt : reads len(p) bytes into p beginning at the `off` offset in the stores file
func (s *store) ReadAt(p []byte, off int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.buf.Flush(); err != nil {
		return 0, err
	}

	return s.File.ReadAt(p, off)
}

// close : persists any buffered data before closing the file
func (s *store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.buf.Flush()
	if err != nil {
		return err
	}

	return s.File.Close()
}
