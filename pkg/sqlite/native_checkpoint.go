package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/stashapp/stash/pkg/fsutil"
)

// NativeCheckpoint is valid only inside WithNativeCheckpoint's callback. The
// source writer lock excludes application deletion staging/recovery as well as
// database writes. Filesystem/config capture must finish inside that callback;
// external download/media writers still need their own boundary verification.
type NativeCheckpoint struct {
	mu        sync.Mutex
	active    bool
	ctx       context.Context
	reader    *sql.Conn
	database  string
	journal   string
	committed []string
}

func (c *NativeCheckpoint) DatabasePath() string            { return c.database }
func (c *NativeCheckpoint) FileDeletionJournalPath() string { return c.journal }
func (c *NativeCheckpoint) CommittedDeletionIDs() []string  { return slices.Clone(c.committed) }

func checkpointURI(path, mode string) string {
	values := url.Values{"mode": {mode}, "_busy_timeout": {"5000"}, "_fk": {"1"}}
	if mode == "ro" {
		values.Set("_query_only", "1")
	} else {
		values.Set("_txlock", "immediate")
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: values.Encode()}
	return uri.String()
}

// WithNativeCheckpoint opens an existing native database without application
// initialization or deletion recovery. BEGIN IMMEDIATE is the same interprocess
// exclusion used by deletion transactions and recovery. The guard never commits
// a database change. The callback must not try to write to this library or run
// recovery; it owns any non-database files it creates and must seal only after
// this function succeeds. This is a capture primitive, not a complete backup.
func WithNativeCheckpoint(ctx context.Context, database string, capture func(*NativeCheckpoint) error) (retErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if capture == nil {
		return errors.New("native checkpoint requires a capture callback")
	}
	path, err := filepath.Abs(database)
	if err != nil {
		return err
	}
	before, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() || before.Size() < 100 {
		return errors.New("native checkpoint requires an existing regular database")
	}
	for _, suffix := range []string{"-wal", "-journal", "-shm"} {
		info, err := os.Lstat(path + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("native checkpoint requires regular SQLite sidecars")
		}
	}
	writer, err := sql.Open(checkpointSQLiteDriver, checkpointURI(path, "rw"))
	if err != nil {
		return err
	}
	defer writer.Close()
	writer.SetMaxOpenConns(1)
	// database/sql automatically rolls back a transaction when its BeginTx
	// context is cancelled. Keep this guard alive until the callback (including
	// in-flight filesystem copies) has actually stopped. Acquisition remains
	// bounded by SQLite's busy timeout; all capture queries/copies use ctx.
	guard, err := writer.BeginTx(context.WithoutCancel(ctx), nil)
	if err != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return fmt.Errorf("acquiring native checkpoint writer lock: %w", err)
	}
	defer func() {
		if err := guard.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			retErr = errors.Join(retErr, err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	var version uint
	var dirty, validLineage bool
	var versions int
	err = guard.QueryRowContext(ctx, "SELECT count(*),version,dirty FROM schema_migrations").Scan(&versions, &version, &dirty)
	if err != nil {
		return err
	}
	if versions != 1 || dirty || version != GetRequiredSchemaVersion() {
		return errors.New("native checkpoint requires this release's exact clean schema")
	}
	if err := guard.QueryRowContext(ctx, "SELECT count(*)=1 AND min(lineage)=? AND min(singleton)=1 FROM native_schema", NativeSchemaLineage).Scan(&validLineage); err != nil {
		return err
	}
	if !validLineage {
		return errors.New("native checkpoint requires the native database lineage")
	}
	committed, err := checkpointDeletionIDs(ctx, guard)
	if err != nil {
		return err
	}
	readDB, err := sql.Open(checkpointSQLiteDriver, checkpointURI(path, "ro"))
	if err != nil {
		return err
	}
	defer readDB.Close()
	reader, err := readDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer reader.Close()
	// No Database.Open call: even an interrupted, uncommitted deletion keeps its
	// staged bytes and raw journal available to the capture callback.
	c := &NativeCheckpoint{active: true, ctx: ctx, reader: reader, database: path,
		journal: (&Database{dbPath: path}).FileDeletionJournalPath(), committed: committed}
	defer func() {
		c.mu.Lock()
		c.active = false
		c.mu.Unlock()
	}()
	if err := capture(c); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	after, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) {
		return errors.New("native source database was replaced during checkpoint capture")
	}
	return nil
}

func checkpointDeletionIDs(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id FROM file_deletions ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// CopyDatabase uses SQLite's online backup API through a separate read-only
// connection while the source writer guard is held. Unlike VACUUM INTO on the
// guarded connection, this works inside the checkpoint. No existing output is
// overwritten. checkSpace, when provided, runs before allocation and each step;
// it must preserve the coordinator's disk reserve for the remaining bytes.
func (c *NativeCheckpoint) CopyDatabase(destination string, checkSpace func(int64) error) (retErr error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active {
		return errors.New("native checkpoint is no longer active")
	}
	if err := c.ctx.Err(); err != nil {
		return err
	}
	var pages, pageSize int64
	if err := c.reader.QueryRowContext(c.ctx, "PRAGMA page_count").Scan(&pages); err != nil {
		return err
	}
	if err := c.reader.QueryRowContext(c.ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return err
	}
	if checkSpace != nil {
		if err := checkSpace(pages * pageSize); err != nil {
			return err
		}
	}
	path, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-journal", "-shm"} {
		if _, err := os.Lstat(path + suffix); !errors.Is(err, os.ErrNotExist) {
			if err != nil {
				return err
			}
			return errors.New("native checkpoint destination has existing SQLite sidecars")
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	defer func() {
		if retErr != nil {
			_ = os.Remove(path)
		}
	}()
	target, err := sql.Open(checkpointSQLiteDriver, checkpointURI(path, "rw"))
	if err != nil {
		return err
	}
	defer target.Close()
	output, err := target.Conn(c.ctx)
	if err != nil {
		return err
	}
	defer output.Close()
	err = c.reader.Raw(func(sourceDriver any) error {
		return output.Raw(func(targetDriver any) (copyErr error) {
			backup, err := targetDriver.(*sqlite3.SQLiteConn).Backup("main", sourceDriver.(*sqlite3.SQLiteConn), "main")
			if err != nil {
				return err
			}
			defer func() { copyErr = errors.Join(copyErr, backup.Close()) }()
			for {
				if err := c.ctx.Err(); err != nil {
					return err
				}
				if checkSpace != nil {
					if err := checkSpace(int64(backup.Remaining()) * pageSize); err != nil {
						return err
					}
				}
				remaining := backup.Remaining()
				done, err := backup.Step(256)
				if err != nil || done {
					return err
				}
				if backup.Remaining() != remaining {
					continue
				}
				// BUSY/LOCKED steps report no progress. Keep cancellation bounded
				// without a CPU spin while SQLite waits on another reader.
				select {
				case <-c.ctx.Done():
					return c.ctx.Err()
				case <-time.After(time.Millisecond):
				}
			}
		})
	})
	if err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	if err := target.Close(); err != nil {
		return err
	}
	f, err = os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return errors.Join(f.Sync(), fsutil.SyncDir(filepath.Dir(path)))
}
