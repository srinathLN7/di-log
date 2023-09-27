package log

import (
	"io"
	"io/ioutil"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"

	api "github.com/srinathLN7/proglog/api/v1"
)

/*

Log: the abstraction that ties all segments together. It consists of a list of segments and a pointer to the active segment to append writes to.
We store the segments in the directory.

RWMutex is used instead of Mutex to allow multiple concurrent `read` operations when there is no `write` holding the locks.
Mutex provides exclusive access, allowing only one goroutine at a time, while RWMutex provides concurrent read access (when no writes are holding locks)
but exclusive write access.

Mutex and RWMutex are both mutual exclusion locks in Go, but they differ in how they handle concurrency.
Mutex is a simple lock that allows only one goroutine to access a shared resource at a time. When a goroutine acquires a Mutex lock with Lock(),
it blocks all other goroutines that try to acquire the same lock until the lock is released with Unlock()
.
On the other hand, RWMutex is a reader/writer mutual exclusion lock that allows multiple readers or a single writer to access a shared resource.
When a goroutine acquires an RWMutex lock with RLock(), it allows other goroutines to also acquire the same lock with RLock(), which means multiple
goroutines can read the same data at the same time. However, when a goroutine acquires an RWMutex lock with Lock(), it blocks all other goroutines
that try to acquire the same lock, including those that try to acquire it with RLock()
.
Mutex is useful when there is only one reader or writer at a time, while RWMutex is useful when there are multiple readers and only one writer.
Mutex ensures that only one goroutine can read or write at a time by acquiring the lock, while RWMutex allows multiple goroutines to read the same
data at the same time, which can improve concurrency.

Regarding the difference between mu.Lock() and rwmu.Lock(), the difference is that mu.Lock() acquires an exclusive lock, while rw.Lock()
acquires a write lock that blocks all other goroutines that try to acquire the same lock, including those that try to acquire it with RLock()

*/

type Log struct {
	mu sync.RWMutex

	Dir    string
	Config Config

	activeSegment *segment
	segments      []*segment
}

// NewLog: set defaults for the configs (if caller didn't specify anything), creates a log instance, and sets up that instance
func NewLog(dir string, c Config) (*Log, error) {
	if c.Segment.MaxStoreBytes == 0 {
		c.Segment.MaxStoreBytes = 1024
	}

	if c.Segment.MaxIndexBytes == 0 {
		c.Segment.MaxIndexBytes = 1024
	}

	l := &Log{
		Dir:    dir,
		Config: c,
	}

	return l, l.setup()
}

/*

The setup() function is responsible for initializing the log by identifying existing log segments and creating corresponding segment instances.
It begins by reading the contents of the log directory using ioutil.ReadDir(l.Dir). This returns a list of files in the directory.
Next, a slice called baseOffsets is created to store the base offsets extracted from the segment file names. Each file name is parsed to
extract the base offset by removing the file extension and converting the resulting string to an unsigned integer using strconv.ParseUint().
The baseOffsets slice is then sorted in ascending order using sort.Slice() with a custom comparison function. A loop is used to iterate through
the sorted baseOffsets slice. For each base offset, the newSegment() function is called to create a new segment instance with the given offset.
After creating the segments, the activeSegment pointer is set to the last segment created, ensuring that subsequent write operations will be directed
to the active segment. If no existing segments were found (i.e., the baseOffsets slice is empty), a new segment is created with the initial offset
specified in the log's configuration.

*/

func (l *Log) setup() error {

	files, err := ioutil.ReadDir(l.Dir)
	if err != nil {
		return err
	}

	var baseOffsets []uint64
	for _, file := range files {
		offStr := strings.TrimSuffix(
			file.Name(),
			path.Ext(file.Name()),
		)
		off, _ := strconv.ParseUint(offStr, 10, 0)
		baseOffsets = append(baseOffsets, off)
	}

	// sort the slice in ascending order
	sort.Slice(baseOffsets, func(i, j int) bool {
		return baseOffsets[i] < baseOffsets[j]
	})

	// create a new segment for each baseoffset
	for i := 0; i < len(baseOffsets); i++ {
		if err = l.newSegment(baseOffsets[i]); err != nil {
			return err
		}

		i++
	}

	if l.segments == nil {
		if err = l.newSegment(
			l.Config.Segment.InitialOffset,
		); err != nil {
			return err
		}
	}

	return nil
}

// newSegment: creates a new segment instance with the given base offset (off).
func (l *Log) newSegment(off uint64) error {
	s, err := newSegment(l.Dir, off, l.Config)
	if err != nil {
		return err
	}

	l.segments = append(l.segments, s)
	l.activeSegment = s
	return nil
}

// Append: appends a record to the log
func (l *Log) Append(record *api.Record) (uint64, error) {

	highestOffset, err := l.HighestOffset()
	if err != nil {
		return 0, err
	}

	// allow exclusive write access
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.activeSegment.IsMaxed() {
		err = l.newSegment(highestOffset + 1)
		if err != nil {
			return 0, err
		}
	}

	off, err := l.activeSegment.Append(record)
	if err != nil {
		return 0, err
	}

	return off, nil
}

// Read: reads the record stored at the given offset
func (l *Log) Read(off uint64) (*api.Record, error) {

	// acquried a shared lock to allow multiple go routines to read from the log concurrently
	l.mu.RLock()
	defer l.mu.RUnlock()

	var s *segment
	for _, segment := range l.segments {

		// if offeset falls within the segment range
		if segment.baseOffset <= off && off < segment.nextOffset {
			s = segment
			break
		}
	}

	if s == nil || s.nextOffset <= off {
		return nil, api.ErrOffsetOutOfRange{Offset: off}
	}

	return s.Read(off)
}

// Close : iterates over the segments and closes them
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, segment := range l.segments {
		if err := segment.Close(); err != nil {
			return err
		}
	}

	return nil
}

// Remove : closes the log and then removes its data
func (l *Log) Remove() error {
	if err := l.Close(); err != nil {
		return err
	}

	return os.RemoveAll(l.Dir)
}

// Reset : removes the log and creates a new log to replace it
func (l *Log) Reset() error {
	if err := l.Remove(); err != nil {
		return err
	}

	return l.setup()
}

// LowestOffset: returns the lowest offset of the log
func (l *Log) LowestOffset() (uint64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return l.segments[0].baseOffset, nil
}

// HighestOffset: returns the highest offset of the log
func (l *Log) HighestOffset() (uint64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// get the offset from the latest segment
	off := l.segments[len(l.segments)-1].nextOffset
	if off == 0 {
		return 0, nil
	}

	return off - 1, nil
}

// Truncate: removes all segments whose highest offset is lower than the `lowest`
// This is critical call since we do not have disks with infinite space
func (l *Log) Truncate(lowest uint64) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var segments []*segment
	for _, s := range l.segments {
		if s.nextOffset <= lowest+1 {
			if err := s.Remove(); err != nil {
				return err
			}
			continue
		}

		// append the segments that are not removed
		segments = append(segments, s)
	}

	// update the log segments after truncating the old ones
	l.segments = segments
	return nil
}

// Reader: returns an io.Reader to read the whole log
func (l *Log) Reader() io.Reader {
	l.mu.RLock()
	defer l.mu.RUnlock()
	readers := make([]io.Reader, len(l.segments))
	for i, segment := range l.segments {
		readers[i] = &originReader{segment.store, 0}
	}
	return io.MultiReader(readers...)
}

type originReader struct {
	*store
	off int64
}

func (o *originReader) Read(p []byte) (int, error) {
	n, err := o.ReadAt(p, o.off)
	o.off += int64(n)
	return n, err
}
