package log

import (
	"io"
	"io/ioutil"
	"os"
	"testing"

	api "github.com/srinathLN7/proglog/api/v1"
	"github.com/stretchr/testify/require"
)

func TestSegment(t *testing.T) {
	dir, _ := ioutil.TempDir("", "segment-test")
	defer os.RemoveAll(dir)

	want := &api.Record{Value: []byte("hello world!!! good morning")}

	c := Config{}
	c.Segment.MaxStoreBytes = 1024
	c.Segment.MaxIndexBytes = entWidth * 3

	// setting base offset to 16
	s, err := newSegment(dir, 16, c)
	require.NoError(t, err)

	require.Equal(t, uint64(16), s.nextOffset, s.nextOffset)
	require.False(t, s.IsMaxed())

	for i := uint64(0); i < 3; i++ {
		off, err := s.Append(want)
		require.NoError(t, err)
		require.Equal(t, 16+i, off)

		record, err := s.Read(off)
		require.NoError(t, err)
		require.Equal(t, want.Value, record.Value)
	}

	// maxed index
	require.True(t, s.IsMaxed())

	_, err = s.Append(want)

	/*
		If the values of MaxIndexBytes and MaxStoreBytes are swapped, it means the size limit for the index file is set based on the value of MaxStoreBytes,
		and vice versa. As a result, the segment will behave incorrectly when checking for available space in the index and store files. In the failing test
		scenario, when MaxIndexBytes and MaxStoreBytes are swapped, the segment incorrectly determines that there is enough space in the index file (based
		on the swapped value), but the store file has reached its maximum capacity. This leads to an incorrect error condition, resulting in the test failure.

	*/

	require.Equal(t, io.EOF, err)

	c.Segment.MaxStoreBytes = uint64(len(want.Value) * 3)
	c.Segment.MaxIndexBytes = 1024

	// call the segment again with the same base offset and dir and check if segment's is able to load state
	// from persisted index and log files
	s, err = newSegment(dir, 16, c)
	require.NoError(t, err)

	require.True(t, s.IsMaxed())

	err = s.Remove()
	require.NoError(t, err)
	s, err = newSegment(dir, 16, c)
	require.NoError(t, err)
	require.False(t, s.IsMaxed())
}
