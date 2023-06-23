package log

import (
	"io/ioutil"
	"os"
	"testing"

	api "github.com/srinathLN7/proglog/api/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestLog : defines a table of tests to test the log
// Avoids code repeatition to create a new log for every test case
func TestLog(t *testing.T) {
	for sceanario, fn := range map[string]func(
		t *testing.T, log *Log){
		"append and read a record succeeds": testAppendRead,
		"offset out of range error":         testOutofRangeErr,
		"init with existing segments":       testInitExisting,
		"reader":                            testReader,
		"truncate":                          testTruncate,
	} {
		t.Run(sceanario, func(t *testing.T) {
			dir, err := ioutil.TempDir("", "store-test")
			require.NoError(t, err)
			defer os.RemoveAll(dir)

			c := Config{}
			c.Segment.MaxStoreBytes = 32
			log, err := NewLog(dir, c)
			require.NoError(t, err)

			fn(t, log)
		})
	}
}

// testAppendRead: tests if we can successfully append to and read from the log
func testAppendRead(t *testing.T, log *Log) {
	append := &api.Record{
		Value: []byte("hello world"),
	}

	// append the log
	off, err := log.Append(append)
	require.NoError(t, err)
	require.Equal(t, uint64(0), off)

	// read the log from the same offset
	read, err := log.Read(off)
	require.NoError(t, err)
	require.Equal(t, append.Value, read.Value)
}

// testOutofRangeErr : tests if the log returns an error when we try to read an offset that is
// outside of the range of offsets the log has stored
func testOutofRangeErr(t *testing.T, log *Log) {
	read, err := log.Read(1)
	require.Nil(t, read)
	require.Error(t, err)
}

// testInitExisting : tests that upon creating a log, the log bootstraps itself from the data stored by prioir log instances
func testInitExisting(t *testing.T, o *Log) {
	append := &api.Record{
		Value: []byte("hello world"),
	}

	// append 3 records to the original log before closing it
	for i := 0; i < 3; i++ {
		_, err := o.Append(append)
		require.NoError(t, err)
	}

	require.NoError(t, o.Close())

	// check if lowest offset is set to 0
	off, err := o.LowestOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(0), off)

	// check if highest offset is set to 2 (after appending three records)
	off, err = o.HighestOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(2), off)

	// create a new log instance configured with the same directory as old log
	n, err := NewLog(o.Dir, o.Config)
	require.NoError(t, err)

	// check if new log lowest offset is 0
	off, err = n.LowestOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(0), off)

	// check if new log highest offset is 2 (after appending three records)
	off, err = n.HighestOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(2), off)
}

// testReader: tests if we can read the full, raw log as it is stored on disk
// This is req'd so that we can snapshot and restore logs in Finite-State Machine
func testReader(t *testing.T, log *Log) {
	append := &api.Record{
		Value: []byte("hello world"),
	}

	off, err := log.Append(append)
	require.NoError(t, err)
	require.Equal(t, uint64(0), off)

	reader := log.Reader()
	b, err := ioutil.ReadAll(reader)
	require.NoError(t, err)

	read := &api.Record{}

	//remember the first 8 bytes (lenWidth) contain the actual byte length of records
	err = proto.Unmarshal(b[lenWidth:], read)
	require.NoError(t, err)
	require.Equal(t, append.Value, read.Value)
}

// testTruncate: tests if we can truncate the log and remove old segments we no longer need
func testTruncate(t *testing.T, log *Log) {
	append := &api.Record{
		Value: []byte("hello world"),
	}

	for i := 0; i < 3; i++ {
		_, err := log.Append(append)
		require.NoError(t, err)
	}

	// remove all segments whose highest offset is lower than 1
	// i.e. remove segment with offset 0
	err := log.Truncate(1)
	require.NoError(t, err)

	// try to read the record with offset 0 should throw an error
	_, err = log.Read(0)
	require.Error(t, err)
}
