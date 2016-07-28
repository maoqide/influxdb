package tsm1

import (
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/influxdata/influxdb/influxql"
	"github.com/influxdata/influxdb/models"
)

var (
	ErrCacheMemoryExceeded    = fmt.Errorf("cache maximum memory size exceeded")
	ErrCacheInvalidCheckpoint = fmt.Errorf("invalid checkpoint")
	ErrSnapshotInProgress     = fmt.Errorf("snapshot in progress")
)

// entry is a set of values and some metadata.
type entry struct {
	mu       sync.RWMutex
	values   Values // All stored values.
	needSort bool   // true if the values are out of order and require deduping.
}

// newEntry returns a new instance of entry.
func newEntry() *entry {
	return globalEntryBatch.Get()
	//return &entry{}
}

type entryBatch struct {
	mu      *sync.Mutex
	backing []entry
	pos     int64
}

var entrymax int64 = 1e7
var globalEntryBatch = entryBatch{
	mu:      &sync.Mutex{},
	backing: make([]entry, entrymax),
	pos:     0,
}

func (eb *entryBatch) Get() *entry {
	eb.mu.Lock()
	if eb.pos == entrymax {
		eb.pos = 0
		eb.backing = nil // abandon it for the GC
		eb.backing = make([]entry, entrymax)
	}
	ret := &eb.backing[eb.pos]
	eb.pos++
	eb.mu.Unlock()
	return ret

}

// add adds the given values to the entry.
func (e *entry) add(values []Value) {
	// See if the new values are sorted or contain duplicate timestamps
	var (
		prevTime int64
		needSort bool
	)

	for _, v := range values {
		if v.UnixNano() <= prevTime {
			needSort = true
			break
		}
		prevTime = v.UnixNano()
	}

	// if there are existing values make sure they're all less than the first of
	// the new values being added
	e.mu.Lock()
	if needSort {
		e.needSort = needSort
	}
	if len(e.values) == 0 {
		e.values = values
	} else {
		l := len(e.values)
		lastValTime := e.values[l-1].UnixNano()
		if lastValTime >= values[0].UnixNano() {
			e.needSort = true
		}
		e.values = append(e.values, values...)
	}
	e.mu.Unlock()
}

// deduplicate sorts and orders the entry's values. If values are already deduped and
// and sorted, the function does no work and simply returns.
func (e *entry) deduplicate() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.needSort || len(e.values) == 0 {
		return
	}
	e.values = e.values.Deduplicate()
	e.needSort = false
}

// count returns number of values for this entry
func (e *entry) count() int {
	e.mu.RLock()
	n := len(e.values)
	e.mu.RUnlock()
	return n
}

// filter removes all values between min and max inclusive
func (e *entry) filter(min, max int64) {
	e.mu.Lock()
	e.values = e.values.Exclude(min, max)
	e.mu.Unlock()
}

// size returns the size of this entry in bytes
func (e *entry) size() int {
	e.mu.RLock()
	sz := e.values.Size()
	e.mu.RUnlock()
	return sz
}

// Statistics gathered by the Cache.
const (
	// levels - point in time measures

	statCacheMemoryBytes = "memBytes"      // level: Size of in-memory cache in bytes
	statCacheDiskBytes   = "diskBytes"     // level: Size of on-disk snapshots in bytes
	statSnapshots        = "snapshotCount" // level: Number of active snapshots.
	statCacheAgeMs       = "cacheAgeMs"    // level: Number of milliseconds since cache was last snapshoted at sample time

	// counters - accumulative measures

	statCachedBytes         = "cachedBytes"         // counter: Total number of bytes written into snapshots.
	statWALCompactionTimeMs = "WALCompactionTimeMs" // counter: Total number of milliseconds spent compacting snapshots
)

// Cache maintains an in-memory store of Values for a set of keys.
type OwnedStringID int64
type Cache struct {
	commit               sync.Mutex
	mu                   sync.RWMutex
	store                map[OwnedStringID]*entry
	internedOwnedStrings map[OwnedString]OwnedStringID
	internedOwnedStringsReverse map[OwnedStringID]OwnedString
	size                 uint64
	maxSize              uint64

	// snapshots are the cache objects that are currently being written to tsm files
	// they're kept in memory while flushing so they can be queried along with the cache.
	// they are read only and should never be modified
	snapshot     *Cache
	snapshotSize uint64
	snapshotting bool

	// This number is the number of pending or failed WriteSnaphot attempts since the last successful one.
	snapshotAttempts int

	stats        *CacheStatistics
	lastSnapshot time.Time

	arena *CacheLocalArena
	ownedStringIDAcc int64
}

func (c *Cache) nextID() OwnedStringID {
	return OwnedStringID(atomic.AddInt64(&c.ownedStringIDAcc, 1))
}

var globalCacheArena *CacheLocalArena
var once = sync.Once{}

// NewCache returns an instance of a cache which will use a maximum of maxSize bytes of memory.
// Only used for engine caches, never for snapshots
func NewCache(maxSize uint64, path string) *Cache {
	once.Do(func() {
		globalCacheArena = NewCacheLocalArena()
	})
	c := &Cache{
		maxSize:              maxSize,
		store:                make(map[OwnedStringID]*entry),
		internedOwnedStrings: make(map[OwnedString]OwnedStringID),
		internedOwnedStringsReverse: make(map[OwnedStringID]OwnedString),
		stats:                &CacheStatistics{},
		lastSnapshot:         time.Now(),
		arena:                globalCacheArena,
	}
	c.UpdateAge()
	c.UpdateCompactTime(0)
	c.updateCachedBytes(0)
	c.updateMemSize(0)
	c.updateSnapshots()
	return c
}

func reclaimStore(cla *CacheLocalArena, m0 map[OwnedString]OwnedStringID) {
	start := time.Now().UnixNano()
	println("RECLAIMING STORE")
	for os := range m0 {
		//delete(m0, os)
		//delete(m1, os)
		cla.Dec(os, 1)
	}
	//wg.Wait()
	//if len(m0) != 0 {
	//	panic("nonempty m0 on reclaimStore")
	//}
	//if len(m1) != 0 {
	//	panic("nonempty m1 on reclaimStore")
	//}
	took := time.Now().UnixNano() - start
	println("RECLAIMED STORE in ", took / 1e6, "ms")
}

// CacheStatistics hold statistics related to the cache.
type CacheStatistics struct {
	MemSizeBytes        int64
	DiskSizeBytes       int64
	SnapshotCount       int64
	CacheAgeMs          int64
	CachedBytes         int64
	WALCompactionTimeMs int64
}

// Statistics returns statistics for periodic monitoring.
func (c *Cache) Statistics(tags map[string]string) []models.Statistic {
	return []models.Statistic{{
		Name: "tsm1_cache",
		Tags: tags,
		Values: map[string]interface{}{
			statCacheMemoryBytes:    atomic.LoadInt64(&c.stats.MemSizeBytes),
			statCacheDiskBytes:      atomic.LoadInt64(&c.stats.DiskSizeBytes),
			statSnapshots:           atomic.LoadInt64(&c.stats.SnapshotCount),
			statCacheAgeMs:          atomic.LoadInt64(&c.stats.CacheAgeMs),
			statCachedBytes:         atomic.LoadInt64(&c.stats.CachedBytes),
			statWALCompactionTimeMs: atomic.LoadInt64(&c.stats.WALCompactionTimeMs),
		},
	}}
}

// Write writes the set of values for the key to the cache. This function is goroutine-safe.
// It returns an error if the cache has exceeded its max size.
func (c *Cache) Write(key string, values []Value) error {
	c.mu.Lock()

	// Enough room in the cache?
	addedSize := Values(values).Size()
	newSize := c.size + uint64(addedSize)
	if c.maxSize > 0 && newSize+c.snapshotSize > c.maxSize {
		c.mu.Unlock()
		return ErrCacheMemoryExceeded
	}

	c.write(key, values)
	c.size = newSize
	c.mu.Unlock()

	// Update the memory size stat
	c.updateMemSize(int64(addedSize))

	return nil
}

// WriteMulti writes the map of keys and associated values to the cache. This function is goroutine-safe.
// It returns an error if the cache has exceeded its max size.
func (c *Cache) WriteMulti(values map[string][]Value) error {
	totalSz := 0
	for _, v := range values {
		totalSz += Values(v).Size()
	}

	// Enough room in the cache?
	c.mu.RLock()
	newSize := c.size + uint64(totalSz)
	if c.maxSize > 0 && newSize+c.snapshotSize > c.maxSize {
		c.mu.RUnlock()
		return ErrCacheMemoryExceeded
	}
	c.mu.RUnlock()

	for k, v := range values {
		c.entry(k).add(v)
	}
	c.mu.Lock()
	c.size += uint64(totalSz)
	c.mu.Unlock()

	// Update the memory size stat
	c.updateMemSize(int64(totalSz))

	return nil
}

// Snapshot will take a snapshot of the current cache, add it to the slice of caches that
// are being flushed, and reset the current cache with new values
func (c *Cache) Snapshot() (*Cache, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.snapshotting {
		return nil, ErrSnapshotInProgress
	}

	c.snapshotting = true
	c.snapshotAttempts++ // increment the number of times we tried to do this

	// If no snapshot exists, create a new one, otherwise update the existing snapshot
	if c.snapshot == nil {
		c.snapshot = &Cache{
			store:                make(map[OwnedStringID]*entry, len(c.store)),
			internedOwnedStrings: make(map[OwnedString]OwnedStringID, len(c.internedOwnedStrings)),
			internedOwnedStringsReverse: make(map[OwnedStringID]OwnedString, len(c.internedOwnedStringsReverse)),
			arena:                globalCacheArena,
		}
	}

	// Append the current cache values to the snapshot
	for osId, e := range c.store {
		os := c.internedOwnedStringsReverse[osId]
		e.mu.RLock()
		osId2, ok := c.snapshot.internedOwnedStrings[os]
		if ok {
			c.snapshot.store[osId2].add(e.values)
		} else {
			c.snapshot.arena.Inc(os, 1)
			osId2 = c.snapshot.nextID()
			c.snapshot.store[osId2] = e
			c.snapshot.internedOwnedStrings[os] = osId2
			c.snapshot.internedOwnedStringsReverse[osId2] = os
		}
		c.snapshotSize += uint64(Values(e.values).Size())
		if e.needSort {
			c.snapshot.store[osId2].needSort = true
		}
		e.mu.RUnlock()
	}

	snapshotSize := c.size // record the number of bytes written into a snapshot

	// Reset the cache
	println("RECLAIMING STORE FROM SNAPSHOT")
	oldM0 := c.internedOwnedStrings
	oldArena := c.arena
	c.store = nil
	c.internedOwnedStrings = nil
	c.internedOwnedStringsReverse = nil

	reclaimStore(oldArena, oldM0)

	c.store = make(map[OwnedStringID]*entry, len(oldM0))
	c.internedOwnedStrings = make(map[OwnedString]OwnedStringID, len(oldM0))
	c.internedOwnedStringsReverse = make(map[OwnedStringID]OwnedString, len(oldM0))

	c.size = 0
	c.lastSnapshot = time.Now()

	c.updateMemSize(-int64(snapshotSize)) // decrement the number of bytes in cache
	c.updateCachedBytes(snapshotSize)     // increment the number of bytes added to the snapshot
	c.updateSnapshots()

	return c.snapshot, nil
}

// Deduplicate sorts the snapshot before returning it. The compactor and any queries
// coming in while it writes will need the values sorted
func (c *Cache) Deduplicate() {
	c.mu.RLock()
	for _, e := range c.store {
		e.deduplicate()
	}
	c.mu.RUnlock()
}

// ClearSnapshot will remove the snapshot cache from the list of flushing caches and
// adjust the size
func (c *Cache) ClearSnapshot(success bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.snapshotting = false

	if success {
		c.snapshotAttempts = 0
		c.snapshotSize = 0
		old := c.snapshot
		c.snapshot = nil

		c.updateSnapshots()

		oldM0 := old.internedOwnedStrings
		oldArena := old.arena
		println("RECLAIMING STORE FROM CLEARSNAPSHOT")
		reclaimStore(oldArena, oldM0)

	}
}

// Size returns the number of point-calcuated bytes the cache currently uses.
func (c *Cache) Size() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.size
}

// MaxSize returns the maximum number of bytes the cache may consume.
func (c *Cache) MaxSize() uint64 {
	return c.maxSize
}

// Keys returns a sorted slice of all keys under management by the cache.
func (c *Cache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	a := make([]string, len(c.store))
	i := 0
	// we have to heap allocate new strings so that downstream consumers
	// have expected behavior
	buf := []byte{}
	for os := range c.internedOwnedStrings {
		buf = buf[:0]
		buf = append(buf, os...)
		heapString := string(buf)
		a[i] = heapString
		i++
	}
	sort.Strings(a)
	return a
}

// Values returns a copy of all values, deduped and sorted, for the given key.
func (c *Cache) Values(key string) Values {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.merged(key)
}

// Delete will remove the keys from the cache
func (c *Cache) Delete(keys []string) {
	c.DeleteRange(keys, math.MinInt64, math.MaxInt64)
}

// DeleteRange will remove the values for all keys containing points
// between min and max from the cache.
func (c *Cache) DeleteRange(strangerKeys []string, min, max int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, sk := range strangerKeys {
		// Make sure key exist in the cache, skip if it does not
		// this is not an ownership change
		osId, ok := c.internedOwnedStrings[OwnedString(sk)]
		if !ok {
			continue
		}

		e := c.store[osId]

		origSize := e.size()
		if min == math.MinInt64 && max == math.MaxInt64 {
			os := c.internedOwnedStringsReverse[osId]
			c.size -= uint64(origSize)
			delete(c.store, osId)
			delete(c.internedOwnedStrings, os)
			delete(c.internedOwnedStringsReverse, osId)
			c.arena.Dec(os, 1)
			continue
		}

		e.filter(min, max)
		if e.count() == 0 {
			os := c.internedOwnedStringsReverse[osId]
			delete(c.store, osId)
			delete(c.internedOwnedStrings, os)
			delete(c.internedOwnedStringsReverse, osId)
			c.arena.Dec(os, 1)

			c.size -= uint64(origSize)
			continue
		}

		c.size -= uint64(origSize - e.size())
	}
}

func (c *Cache) SetMaxSize(size uint64) {
	c.mu.Lock()
	c.maxSize = size
	c.mu.Unlock()
}

// merged returns a copy of hot and snapshot values. The copy will be merged, deduped, and
// sorted. It assumes all necessary locks have been taken. If the caller knows that the
// the hot source data for the key will not be changed, it is safe to call this function
// with a read-lock taken. Otherwise it must be called with a write-lock taken.
func (c *Cache) merged(strangerKey string) Values {
	// this is not an ownership change
	osId, ok := c.internedOwnedStrings[OwnedString(strangerKey)]
	var e *entry
	if !ok {
		if c.snapshot == nil {
			// No values in hot cache or snapshots.
			return nil
		}
	} else {
		e = c.store[osId]
		e.deduplicate()
	}

	// Build the sequence of entries that will be returned, in the correct order.
	// Calculate the required size of the destination buffer.
	var entries []*entry
	sz := 0

	if c.snapshot != nil {
		snapshotEntriesId, ok := c.snapshot.internedOwnedStrings[OwnedString(strangerKey)]
		if ok {
			snapshotEntries := c.snapshot.store[snapshotEntriesId]
			snapshotEntries.deduplicate() // guarantee we are deduplicated
			entries = append(entries, snapshotEntries)
			sz += snapshotEntries.count()
		}
	}

	if e != nil {
		entries = append(entries, e)
		sz += e.count()
	}

	// Any entries? If not, return.
	if sz == 0 {
		return nil
	}

	// Create the buffer, and copy all hot values and snapshots. Individual
	// entries are sorted at this point, so now the code has to check if the
	// resultant buffer will be sorted from start to finish.
	var needSort bool
	values := make(Values, sz)
	n := 0
	for _, e := range entries {
		e.mu.RLock()
		if !needSort && n > 0 && len(e.values) > 0 {
			needSort = values[n-1].UnixNano() >= e.values[0].UnixNano()
		}
		n += copy(values[n:], e.values)
		e.mu.RUnlock()
	}
	values = values[:n]

	if needSort {
		values = values.Deduplicate()
	}

	return values
}

type DataTypeResult struct {
	Val influxql.DataType
	Err error
}

func (c *Cache) KeysAndTypes() ([]string, []DataTypeResult) {
	heapKeys := make([]string, len(c.store))
	dtrs := make([]DataTypeResult, len(c.store))
	buf := []byte{}
	i := 0
	for osId, e := range c.store {
		os := c.internedOwnedStringsReverse[osId]
		buf = buf[:0]
		buf = append(buf, os...)
		heapString := string(buf)

		dt, err := e.values.InfluxQLType()
		dtr := DataTypeResult{Val: dt, Err: err}

		heapKeys[i] = heapString
		dtrs[i] = dtr
		i++
	}
	return heapKeys, dtrs
}

// Store returns the underlying cache store. This is not goroutine safe!
// Protect access by using the Lock and Unlock functions on Cache.
//func (c *Cache) Store() map[string]*entry {
//	return nil
//	//return c.store
//}

func (c *Cache) RLock() {
	c.mu.RLock()
}

func (c *Cache) RUnlock() {
	c.mu.RUnlock()
}

// values returns the values for the key. It doesn't lock and assumes the data is
// already sorted. Should only be used in compact.go in the CacheKeyIterator
func (c *Cache) values(strangerKey string) Values {
	// this is not an ownership change
	osId, ok := c.internedOwnedStrings[OwnedString(strangerKey)]
	if !ok {
		return nil
	}
	e := c.store[osId]
	return e.values
}

// write writes the set of values for the key to the cache. This function assumes
// the lock has been taken and does not enforce the cache size limits.
func (c *Cache) write(strangerKey string, values []Value) {
	// this is not an ownership change
	osId, ok := c.internedOwnedStrings[OwnedString(strangerKey)]
	var e *entry
	if ok {
		e = c.store[osId]
	} else {
		// this is an ownership change
		e = newEntry()
		os := c.arena.GetOwnedString(strangerKey)
		osId := c.nextID()
		c.store[osId] = e
		c.internedOwnedStrings[os] = osId
		c.internedOwnedStringsReverse[osId] = os
	}
	e.add(values)
}

func (c *Cache) entry(strangerKey string) *entry {
	// low-contention path: entry exists, no write operations needed:
	var e *entry
	c.mu.RLock()
	// this is not an ownership change
	osId, ok := c.internedOwnedStrings[OwnedString(strangerKey)]
	if ok {
		e = c.store[osId]
	}
	c.mu.RUnlock()

	if ok {
		return e
	}

	// high-contention path: entry doesn't exist (probably), create a new
	// one after checking again:
	c.mu.Lock()

	osId, ok = c.internedOwnedStrings[OwnedString(strangerKey)]
	if ok {
		e = c.store[osId]
	} else {
		// this is an ownership change
		os := c.arena.GetOwnedString(strangerKey)
		osId := c.nextID()
		e = newEntry()
		c.store[osId] = e
		c.internedOwnedStrings[os] = osId
		c.internedOwnedStringsReverse[osId] = os
	}

	c.mu.Unlock()

	return e
}

// CacheLoader processes a set of WAL segment files, and loads a cache with the data
// contained within those files.  Processing of the supplied files take place in the
// order they exist in the files slice.
type CacheLoader struct {
	files []string

	Logger *log.Logger
}

// NewCacheLoader returns a new instance of a CacheLoader.
func NewCacheLoader(files []string) *CacheLoader {
	return &CacheLoader{
		files:  files,
		Logger: log.New(os.Stderr, "[cacheloader] ", log.LstdFlags),
	}
}

// Load returns a cache loaded with the data contained within the segment files.
// If, during reading of a segment file, corruption is encountered, that segment
// file is truncated up to and including the last valid byte, and processing
// continues with the next segment file.
func (cl *CacheLoader) Load(cache *Cache) error {
	for _, fn := range cl.files {
		if err := func() error {
			f, err := os.OpenFile(fn, os.O_CREATE|os.O_RDWR, 0666)
			if err != nil {
				return err
			}

			// Log some information about the segments.
			stat, err := os.Stat(f.Name())
			if err != nil {
				return err
			}
			cl.Logger.Printf("reading file %s, size %d", f.Name(), stat.Size())

			r := NewWALSegmentReader(f)
			defer r.Close()

			for r.Next() {
				entry, err := r.Read()
				if err != nil {
					n := r.Count()
					cl.Logger.Printf("file %s corrupt at position %d, truncating", f.Name(), n)
					if err := f.Truncate(n); err != nil {
						return err
					}
					break
				}

				switch t := entry.(type) {
				case *WriteWALEntry:
					if err := cache.WriteMulti(t.Values); err != nil {
						return err
					}
				case *DeleteRangeWALEntry:
					cache.DeleteRange(t.Keys, t.Min, t.Max)
				case *DeleteWALEntry:
					cache.Delete(t.Keys)
				}
			}

			return nil
		}(); err != nil {
			return err
		}
	}
	return nil
}

// SetLogOutput sets the logger used for all messages. It must not be called
// after the Open method has been called.
func (cl *CacheLoader) SetLogOutput(w io.Writer) {
	cl.Logger = log.New(w, "[cacheloader] ", log.LstdFlags)
}

// Updates the age statistic
func (c *Cache) UpdateAge() {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ageStat := int64(time.Now().Sub(c.lastSnapshot) / time.Millisecond)
	atomic.StoreInt64(&c.stats.CacheAgeMs, ageStat)
}

// Updates WAL compaction time statistic
func (c *Cache) UpdateCompactTime(d time.Duration) {
	atomic.AddInt64(&c.stats.WALCompactionTimeMs, int64(d/time.Millisecond))
}

// Update the cachedBytes counter
func (c *Cache) updateCachedBytes(b uint64) {
	atomic.AddInt64(&c.stats.CachedBytes, int64(b))
}

// Update the memSize level
func (c *Cache) updateMemSize(b int64) {
	atomic.AddInt64(&c.stats.MemSizeBytes, b)
}

// Update the snapshotsCount and the diskSize levels
func (c *Cache) updateSnapshots() {
	// Update disk stats
	atomic.StoreInt64(&c.stats.DiskSizeBytes, int64(c.snapshotSize))
	atomic.StoreInt64(&c.stats.SnapshotCount, int64(c.snapshotAttempts))
}
