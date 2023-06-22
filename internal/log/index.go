package log

import (
	"io"
	"os"

	"github.com/tysonmote/gommap"
)

var (
	offWidth uint64 = 4
	posWidth uint64 = 8
	entWidth        = offWidth + posWidth
)

// Index - the file we store index entries in
// Index consists of a persisted file, memory mapped file and a size associated with it
type index struct {
	file *os.File
	mmap gommap.MMap
	size uint64
}

// newIndex : creates an index for the given file
func newIndex(f *os.File, c Config) (*index, error) {
	idx := &index{
		file: f,
	}

	fi, err := os.Stat(f.Name())
	if err != nil {
		return nil, err
	}

	// size => size of the index and where to write the next entry appended to the index
	idx.size = uint64(fi.Size())
	if err = os.Truncate(
		f.Name(), int64(c.Segment.MaxIndexBytes),
	); err != nil {
		return nil, err
	}

	// Memory mapping
	// 	file.Fd(): file.Fd() returns the file descriptor of the file object. The file descriptor represents an open file in the operating system.
	// By passing this value, we indicate which file should be mapped into memory.
	// gommap.PROT_READ|gommap.PROT_WRITE: This argument specifies the protection level for the memory-mapped region.
	// By combining the gommap.PROT_READ and gommap.PROT_WRITE flags using the bitwise OR operator (|), we
	// indicate that the mapped memory should be readable and writable. This allows us to perform both read and write
	// operations on the mapped file.

	// gommap.MAP_SHARED: This argument specifies the type of mapping to create. In this case, gommap.MAP_SHARED indicates that the changes made to the mapped memory will be visible to other processes or threads that also
	// have the file mapped. This allows for shared access to the mapped file.
	// The gommap.Map function returns two values: mmap and err. mmap is a byte slice representing the memory-mapped file, and err indicates any error that occurred during the mapping process.

	if idx.mmap, err = gommap.Map(
		idx.file.Fd(),
		gommap.PROT_READ|gommap.PROT_WRITE,
		gommap.MAP_SHARED,
	); err != nil {
		return nil, err
	}

	return idx, nil
}

// Close() closes the file
func (i *index) Close() error {

	// checks if the memory-mapped file has synced its data to the persisted file
	if err := i.mmap.Sync(gommap.MS_SYNC); err != nil {
		return err
	}

	// check if the persisted file has flused its contents to stable storage
	if err := i.file.Sync(); err != nil {
		return err
	}

	// check if it trauncates the persisted file to the amount of data that is actually in it
	if err := i.file.Truncate(int64(i.size)); err != nil {
		return err
	}

	return i.file.Close()
}

// Read: takes in an offset and returns associated record's position in the store
func (i *index) Read(in int64) (out uint32, pos uint64, err error) {

	// if index is empty throw End of File error
	if i.size == 0 {
		return 0, 0, io.EOF
	}

	// if user wants to read the last record in index
	if in == -1 {
		out = uint32((i.size / entWidth) - 1)
	} else {
		out = uint32(in)
	}

	pos = uint64(out) * entWidth

	// if required position is beyond the bounds of index
	if i.size < pos+entWidth {
		return 0, 0, io.EOF
	}

	// retrieve the record info by converting bytes to Uint32

	// Read the output index
	out = enc.Uint32(i.mmap[pos : pos+offWidth])

	// Read the record position
	pos = enc.Uint64(i.mmap[pos+offWidth : pos+entWidth])
	return out, pos, nil
}

// Write : Appends the given offset and position to the index
func (i *index) Write(off uint32, pos uint64) error {

	// check if we have space to write the entry
	if uint64(len(i.mmap)) < i.size+entWidth {
		return io.EOF
	}

	// encode the offset and position and write to memory map
	enc.PutUint32(i.mmap[i.size:i.size+offWidth], off)
	enc.PutUint64(i.mmap[i.size+offWidth:i.size+entWidth], pos)
	i.size += uint64(entWidth)

	return nil
}

// Name: returns index file path
func (i *index) Name() string {
	return i.file.Name()
}
