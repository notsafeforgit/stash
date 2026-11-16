// Package keyframecache keeps disposable keyframe indexes and scrub stills in
// one bounded SQLite file instead of one small file per video. Every entry can
// be recomputed from its media file, so a missing, unreadable or corrupt cache
// is discarded rather than repaired.
package keyframecache

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/mattn/go-sqlite3"
)

const (
	schemaVersion = 1
	// Fixed per-row cost added to each value when accounting for its key,
	// row header and index entry.
	rowOverhead = 64
	// Reads refresh the recency of an entry at most this often, so a hot entry
	// does not cause a write for every request.
	touchInterval = 10 * time.Minute
	// A failed open is retried no more often than this.
	reopenDelay = time.Minute
)

const schema = `
CREATE TABLE IF NOT EXISTS entries (
	id INTEGER PRIMARY KEY,
	kind TEXT NOT NULL,
	key TEXT NOT NULL,
	value BLOB NOT NULL,
	size INTEGER NOT NULL,
	accessed INTEGER NOT NULL,
	UNIQUE(kind, key)
);
CREATE INDEX IF NOT EXISTS entries_lru ON entries(kind, accessed, id);
PRAGMA user_version = 1;`

// Store is safe for concurrent use. The database is opened on first use.
type Store struct {
	path   string
	limits map[string]int64
	now    func() time.Time

	mu         sync.Mutex
	db         *sql.DB
	totals     map[string]int64
	closed     bool
	openFailed time.Time
}

// Open returns a store at path without touching the filesystem. budgets maps
// each entry kind to its maximum accounted bytes; each kind is evicted
// independently, so a burst of one kind cannot flush another.
func Open(path string, budgets map[string]int64) *Store {
	limits := make(map[string]int64, len(budgets))
	for kind, budget := range budgets {
		if budget > 0 {
			limits[kind] = budget
		}
	}
	return &Store{path: path, limits: limits, now: time.Now}
}

var errClosed = errors.New("keyframe cache closed")

// Get returns nil without an error when the entry is absent.
func (s *Store) Get(ctx context.Context, kind, key string) ([]byte, error) {
	if _, ok := s.limits[kind]; !ok {
		return nil, fmt.Errorf("unknown keyframe cache kind %q", kind)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	var (
		id       int64
		value    []byte
		accessed int64
	)
	err = db.QueryRowContext(ctx, "SELECT id, value, accessed FROM entries WHERE kind = ? AND key = ?", kind, key).Scan(&id, &value, &accessed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, s.fail(err)
	}
	if now := s.now().Unix(); now-accessed >= int64(touchInterval/time.Second) {
		if _, err := db.ExecContext(ctx, "UPDATE entries SET accessed = ? WHERE id = ?", now, id); err != nil {
			return value, s.fail(err)
		}
	}
	return value, nil
}

// Put replaces an entry, then evicts the least recently used entries of the
// same kind until it is within budget. Values larger than an eighth of the
// budget are not stored.
func (s *Store) Put(ctx context.Context, kind, key string, value []byte) error {
	limit, ok := s.limits[kind]
	if !ok {
		return fmt.Errorf("unknown keyframe cache kind %q", kind)
	}
	size := int64(len(value)+len(key)) + rowOverhead
	if size > limit/8 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.open()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return s.fail(err)
	}
	var previous int64
	err = tx.QueryRowContext(ctx, "SELECT size FROM entries WHERE kind = ? AND key = ?", kind, key).Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		_ = tx.Rollback()
		return s.fail(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO entries (kind, key, value, size, accessed) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(kind, key) DO UPDATE SET value = excluded.value, size = excluded.size, accessed = excluded.accessed`,
		kind, key, value, size, s.now().Unix())
	if err == nil {
		err = tx.Commit()
	} else {
		_ = tx.Rollback()
	}
	if err != nil {
		return s.fail(err)
	}
	s.totals[kind] += size - previous
	if s.totals[kind] > limit {
		return s.evict(ctx, db, kind, limit*9/10)
	}
	return nil
}

func (s *Store) evict(ctx context.Context, db *sql.DB, kind string, target int64) error {
	for s.totals[kind] > target {
		ids, freed, err := oldestEntries(ctx, db, kind)
		if err != nil {
			return s.fail(err)
		}
		if len(ids) == 0 {
			s.totals[kind] = 0
			break
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return s.fail(err)
		}
		removed := int64(0)
		for i, id := range ids {
			if s.totals[kind]-removed <= target {
				break
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM entries WHERE id = ?", id); err != nil {
				_ = tx.Rollback()
				return s.fail(err)
			}
			removed += freed[i]
		}
		if err := tx.Commit(); err != nil {
			return s.fail(err)
		}
		s.totals[kind] -= removed
	}
	// Return freed pages to the filesystem so the file stays near its budget.
	if _, err := db.ExecContext(ctx, "PRAGMA incremental_vacuum"); err != nil {
		return s.fail(err)
	}
	return nil
}

func oldestEntries(ctx context.Context, db *sql.DB, kind string) (ids, sizes []int64, err error) {
	rows, err := db.QueryContext(ctx, "SELECT id, size FROM entries WHERE kind = ? ORDER BY accessed, id LIMIT 256", kind)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, size int64
		if err := rows.Scan(&id, &size); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		sizes = append(sizes, size)
	}
	return ids, sizes, rows.Err()
}

// Close releases the database. Later calls return an error, which callers
// treat as a cache miss.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

func (s *Store) open() (*sql.DB, error) {
	if s.closed {
		return nil, errClosed
	}
	if s.db != nil {
		return s.db, nil
	}
	if !s.openFailed.IsZero() && s.now().Sub(s.openFailed) < reopenDelay {
		return nil, errors.New("keyframe cache unavailable")
	}
	db, err := s.openDatabase()
	if err != nil {
		// The cache holds only derived data: start again from an empty file.
		s.removeFiles()
		db, err = s.openDatabase()
	}
	if err != nil {
		s.openFailed = s.now()
		return nil, err
	}
	s.openFailed = time.Time{}
	s.db = db
	return db, nil
}

func (s *Store) openDatabase() (*sql.DB, error) {
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	uri := url.URL{Scheme: "file", Path: s.path, RawQuery: "_journal=WAL&_sync=NORMAL&_busy_timeout=1000&_auto_vacuum=incremental"}
	db, err := sql.Open("sqlite3", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var version int
	err = db.QueryRow("PRAGMA user_version").Scan(&version)
	if err == nil && version != 0 && version != schemaVersion {
		err = fmt.Errorf("unsupported keyframe cache version %d", version)
	}
	if err == nil {
		_, err = db.Exec(schema)
	}
	totals := map[string]int64{}
	if err == nil {
		err = func() error {
			rows, err := db.Query("SELECT kind, coalesce(sum(size), 0) FROM entries GROUP BY kind")
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var kind string
				var total int64
				if err := rows.Scan(&kind, &total); err != nil {
					return err
				}
				totals[kind] = total
			}
			return rows.Err()
		}()
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	s.totals = totals
	return db, nil
}

// fail discards a corrupt database. Other errors are returned unchanged.
func (s *Store) fail(err error) error {
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) && (sqliteErr.Code == sqlite3.ErrCorrupt || sqliteErr.Code == sqlite3.ErrNotADB) {
		if s.db != nil {
			_ = s.db.Close()
			s.db = nil
		}
		s.removeFiles()
	}
	return err
}

func (s *Store) removeFiles() {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = os.Remove(s.path + suffix)
	}
}
