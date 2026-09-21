package sqlite

// Read acceleration is deliberately outside the library schema. The observer
// stays on one connection (SQLite data_version values cannot be compared across
// connections), and read snapshots are pinned between two observations. Neither
// a cached count nor a search index from a different snapshot may narrow a read.

import (
	"container/list"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/logger"
)

const readAccelerationKey key = 100
const maxCachedCounts = 256
const maxSearchChanges = 8192

type readSnapshot struct {
	cache          *readAcceleration
	revision       uint64
	searchRevision uint64
	writable       bool
	tracked        bool
}

type countCacheEntry struct {
	key   [32]byte
	count int
}

type searchChanges map[string]map[int]struct{}

type readAcceleration struct {
	mu             sync.Mutex
	observer       *sql.Conn
	main           *sqlx.DB
	index          *sqlx.DB
	owner          string
	dataVersion    int64
	revision       uint64
	searchRevision uint64
	counts         map[[32]byte]*list.Element
	countOrder     list.List
	fullRebuild    bool
	changes        searchChanges
	wake           chan struct{}
	cancel         context.CancelFunc
	done           chan struct{}
}

func newReadAcceleration(main *sqlx.DB, path string) (*readAcceleration, error) {
	ctx, cancel := context.WithCancel(context.Background())
	observer, err := main.Conn(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	a := &readAcceleration{
		observer: observer, main: main, owner: rand.Text(), revision: 1, searchRevision: 1,
		counts: make(map[[32]byte]*list.Element), fullRebuild: true,
		changes: make(searchChanges), wake: make(chan struct{}, 1), cancel: cancel, done: make(chan struct{}),
	}
	if a.dataVersion, err = a.version(ctx); err != nil {
		cancel()
		observer.Close()
		return nil, err
	}
	a.index, err = openSearchIndex(path + ".search.sqlite")
	if err != nil {
		// Builds without FTS5, read-only directories, or damaged caches must not
		// prevent opening a library. Count caching still works.
		logger.Warnf("Search index unavailable; using database search: %v", err)
	}
	go a.run(ctx)
	a.signal()
	return a, nil
}

func (a *readAcceleration) close() {
	a.cancel()
	<-a.done
	a.observer.Close()
	if a.index != nil {
		a.index.Close()
	}
}

func (a *readAcceleration) version(ctx context.Context) (int64, error) {
	var version int64
	err := a.observer.QueryRowContext(ctx, "PRAGMA main.data_version").Scan(&version)
	return version, err
}

// Called with mu held. Unknown writers (including an upstream process) cannot
// use our connection-local change journal, so their commits require a rebuild.
func (a *readAcceleration) observe(ctx context.Context) bool {
	v, err := a.version(ctx)
	if err != nil || v != a.dataVersion {
		a.invalidate(true)
		a.dataVersion = v
	}
	return err == nil
}

func (a *readAcceleration) invalidate(search bool) {
	a.revision++
	clear(a.counts)
	a.countOrder.Init()
	if search {
		a.searchRevision++
		a.fullRebuild = true
		clear(a.changes)
		a.signal()
	}
}

func (a *readAcceleration) signal() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// pin must be called with mu held, after acquiring a transaction from the pool.
// Acquiring a pooled connection while holding mu could deadlock other readers.
func (a *readAcceleration) pin(ctx context.Context, tx *sqlx.Tx) *readSnapshot {
	if !a.observe(ctx) {
		return nil
	}
	v := a.dataVersion
	var root int
	if err := tx.GetContext(ctx, &root, "SELECT rootpage FROM main.sqlite_schema LIMIT 1"); err != nil {
		return nil
	}
	if !a.observe(ctx) || a.dataVersion != v {
		return nil
	}
	return &readSnapshot{cache: a, revision: a.revision, searchRevision: a.searchRevision}
}

func (a *readAcceleration) begin(ctx context.Context, tx *sqlx.Tx, writable bool) context.Context {
	if writable {
		tracked := false
		if a.index != nil {
			tracked = prepareSearchChanges(ctx, tx) == nil
		}
		return context.WithValue(ctx, readAccelerationKey, &readSnapshot{cache: a, writable: true, tracked: tracked})
	}
	a.mu.Lock()
	snapshot := a.pin(ctx, tx)
	a.mu.Unlock()
	if snapshot == nil {
		return ctx
	}
	return context.WithValue(ctx, readAccelerationKey, snapshot)
}

func (a *readAcceleration) commit(ctx context.Context, tx *sqlx.Tx, snapshot *readSnapshot) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.observe(ctx)
	var changes searchChanges
	var err error
	if snapshot.tracked {
		changes, err = collectSearchChanges(ctx, tx)
	}
	var writerVersion int64
	writerVersionErr := tx.GetContext(ctx, &writerVersion, "PRAGMA main.data_version")
	if commitErr := tx.Commit(); commitErr != nil {
		return commitErr
	}
	a.invalidate(a.index != nil && (!snapshot.tracked || err != nil))
	if len(changes) > 0 && !a.fullRebuild {
		a.searchRevision++
		for table, ids := range changes {
			if a.changes[table] == nil {
				a.changes[table] = make(map[int]struct{})
			}
			for id := range ids {
				a.changes[table][id] = struct{}{}
			}
			if len(a.changes[table]) > maxSearchChanges {
				a.fullRebuild = true
				clear(a.changes)
				break
			}
		}
		a.signal()
	} else if len(changes) > 0 {
		// A rebuild already in flight must not be published as this revision.
		a.searchRevision++
		a.signal()
	}
	// data_version does not count commits, and our writer's own commit does
	// not change its value. Observe both connections, in this order, to detect
	// an external commit racing between our Commit and the observer's read.
	v, versionErr := a.version(ctx)
	var afterWriterVersion int64
	conn, _ := ctx.Value(writeConnectionKey).(*sqlx.Conn)
	if conn == nil {
		writerVersionErr = fmt.Errorf("missing writer connection")
	} else if readErr := conn.GetContext(ctx, &afterWriterVersion, "PRAGMA main.data_version"); readErr != nil {
		writerVersionErr = readErr
	}
	if versionErr != nil || writerVersionErr != nil || writerVersion != afterWriterVersion {
		a.invalidate(true)
	}
	a.dataVersion = v
	return nil
}

func snapshotFromContext(ctx context.Context) *readSnapshot {
	s, _ := ctx.Value(readAccelerationKey).(*readSnapshot)
	if s == nil || s.writable {
		return nil
	}
	return s
}

func countCacheKey(query string, args []interface{}) ([32]byte, bool) {
	// Relative dates and nondeterministic predicates can change without a write.
	lower := strings.ToLower(query)
	for _, token := range []string{"'now'", "current_", "random(", "randomblob("} {
		if strings.Contains(lower, token) {
			return [32]byte{}, false
		}
	}
	hash := sha256.New()
	hash.Write([]byte(query))
	for _, arg := range args {
		encoded, err := json.Marshal(arg)
		if err != nil {
			return [32]byte{}, false
		}
		// JSON alone merges integer and floating-point bindings. SQLite can
		// distinguish them when applying text affinity.
		fmt.Fprintf(hash, "\x00%T:%s", arg, encoded)
	}
	var key [32]byte
	copy(key[:], hash.Sum(nil))
	return key, true
}

func (s *readSnapshot) count(key [32]byte) (int, bool) {
	a := s.cache
	a.mu.Lock()
	defer a.mu.Unlock()
	if s.revision == a.revision {
		if entry := a.counts[key]; entry != nil {
			a.countOrder.MoveToFront(entry)
			return entry.Value.(countCacheEntry).count, true
		}
	}
	return 0, false
}

func (s *readSnapshot) rememberCount(key [32]byte, count int) {
	a := s.cache
	a.mu.Lock()
	defer a.mu.Unlock()
	if s.revision != a.revision {
		return
	}
	if entry := a.counts[key]; entry != nil {
		a.countOrder.Remove(entry)
	}
	a.counts[key] = a.countOrder.PushFront(countCacheEntry{key, count})
	if a.countOrder.Len() > maxCachedCounts {
		oldest := a.countOrder.Back()
		delete(a.counts, oldest.Value.(countCacheEntry).key)
		a.countOrder.Remove(oldest)
	}
}

func (a *readAcceleration) run(ctx context.Context) {
	defer close(a.done)
	if a.index == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.wake:
		}
		// Coalesce scan/import bursts without blocking reads or writes.
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if err := a.refreshSearchIndex(ctx); err != nil && ctx.Err() == nil {
			logger.Warnf("Search index refresh failed; using database search: %v", err)
			a.mu.Lock()
			a.fullRebuild = true
			a.mu.Unlock()
			// A transient failure should recover, without retrying on every read.
			timer.Reset(30 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				a.signal()
			}
		}
	}
}
