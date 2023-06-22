package log

import (
	"fmt"
	"os"
	"path"

	api "github.com/srinathLN7/proglog/api/v1"
	"google.golang.org/protobuf/proto"
)

// segment  - the abstraction that ties store and index
type segment struct {
	store                  *store
	index                  *index
	baseOffset, nextOffset uint64
	config                 Config
}

// newSegment : creates a new segment when the current active segment hits max size
func newSegment(dir string, baseOffset uint64, c Config) (*segment, error) {
	s := &segment{
		baseOffset: baseOffset,
		config:     c,
	}

	var err error
	storeFile, err := os.OpenFile(
		path.Join(dir, fmt.Sprintf("%d%s", baseOffset, ".store")),
		os.O_RDWR|os.O_CREATE|os.O_APPEND,
		0644,
	)

	if err != nil {
		return nil, err
	}

	if s.store, err = newStore(storeFile); err != nil {
		return nil, err
	}

	indexFile, err := os.OpenFile(
		path.Join(dir, fmt.Sprintf("%d%s", baseOffset, ".index")),
		os.O_RDWR|os.O_CREATE,
		0644,
	)

	if err != nil {
		return nil, err
	}

	if s.index, err = newIndex(indexFile, c); err != nil {
		return nil, err
	}

	/*
		    The below code checks if there was an error while reading the index entry.
			If an error occurred, it means there are no entries in the index, and thus, no records have been appended yet.
			If there is an error (no entries), the s.nextOffset is set to the baseOffset. This means that the next record
			to be appended will start at the base offset of the segment. If there is no error (entries exist), it means there
			are existing records in the segment. In this case, the s.nextOffset is calculated as baseOffset + uint64(off) + 1.
			Here's what each component of the calculation represents:
			baseOffset: The base offset of the segment.
			uint64(off): The offset of the last entry in the index, converted to uint64.
			1: Adding 1 ensures that the next offset is incremented by 1 to point to the position after the last existing record in the segment.
			This is because offsets are typically zero-based, so the next offset will be the offset of the last record plus
	*/

	if off, _, err := s.index.Read(-1); err != nil {
		s.nextOffset = baseOffset
	} else {
		s.nextOffset = baseOffset + uint64(off) + 1
	}

	return s, nil
}

// Append : writes the record to the segment and returns the newly appended record's offset
func (s *segment) Append(record *api.Record) (offset uint64, err error) {
	cur := s.nextOffset
	record.Offset = cur

	// serialize the record to bytes
	p, err := proto.Marshal(record)
	if err != nil {
		return 0, err
	}

	// append the record to the store
	_, pos, err := s.store.Append(p)
	if err != nil {
		return 0, err
	}

	// write the index entry
	// index offsets are relative to base offsets
	if err = s.index.Write(uint32(s.nextOffset-uint64(s.baseOffset)), pos); err != nil {
		return 0, err
	}

	// prepare for next record to be appended
	s.nextOffset++
	return cur, nil
}

func (s *segment) Read(off uint64) (*api.Record, error) {

	// get the records position in store from the index entries
	_, pos, err := s.index.Read(int64(off - s.baseOffset))
	if err != nil {
		return nil, err
	}

	// retrieve the record from the store at the obtained position
	p, err := s.store.Read(pos)
	if err != nil {
		return nil, err
	}

	record := &api.Record{}
	err = proto.Unmarshal(p, record)
	return record, err
}

// IsMaxed returns whether the segment has reached its max size either in store or in index
func (s *segment) IsMaxed() bool {
	return s.store.size >= s.config.Segment.MaxStoreBytes ||
		s.index.size >= s.config.Segment.MaxIndexBytes
}

// Close : closes the index and store files
func (s *segment) Close() error {

	if err := s.index.Close(); err != nil {
		return err
	}

	if err := s.store.Close(); err != nil {
		return err
	}

	return nil
}

// Remove : removes the segment and removes the index and store files
func (s *segment) Remove() error {
	if err := s.Close(); err != nil {
		return err
	}

	if err := os.Remove(s.index.Name()); err != nil {
		return err
	}

	if err := os.Remove(s.store.Name()); err != nil {
		return err
	}

	return nil
}
