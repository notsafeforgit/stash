package sqlite

import (
	"context"
	"crypto/sha256"
	hexencoding "encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/jmoiron/sqlx"
)

// NativeSnapshotReport binds semantic validation to the unchanged database
// artifact. A valid database does not establish a coordinated media/journal or
// producer backup, and pending filesystem operations are never executed here.
type NativeSnapshotReport struct {
	Format                     string `json:"format"`
	Version                    int    `json:"version"`
	Lineage                    string `json:"lineage"`
	SchemaVersion              uint   `json:"schema_version"`
	SHA256                     string `json:"sha256"`
	Bytes                      int64  `json:"bytes"`
	DatabaseVerified           bool   `json:"database_verified"`
	PendingFileDeletions       int64  `json:"pending_file_deletions"`
	FilesystemRecoveryVerified bool   `json:"filesystem_recovery_verified"`
}

func snapshotSidecars(path string) error {
	for _, suffix := range []string{"-wal", "-journal", "-shm"} {
		info, err := os.Lstat(path + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || suffix != "-shm" && info.Size() != 0 {
			return errors.New("native verification requires a standalone SQLite snapshot without a nonempty WAL or rollback journal")
		}
	}
	return nil
}

func snapshotDigest(ctx context.Context, file *os.File) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	digest := sha256.New()
	buffer := make([]byte, 1<<20)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := file.Read(buffer)
		if n > 0 {
			_, _ = digest.Write(buffer[:n])
		}
		if errors.Is(err, io.EOF) {
			return hexencoding.EncodeToString(digest.Sum(nil)), nil
		}
		if err != nil {
			return "", err
		}
	}
}

func snapshotIntegrity(ctx context.Context, tx *sqlx.Tx) error {
	rows, err := tx.QueryxContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	var integrity string
	if !rows.Next() {
		return errors.New("native snapshot has no SQLite integrity result")
	}
	if err := rows.Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" || rows.Next() || rows.Err() != nil {
		return errors.New("native snapshot failed SQLite integrity verification")
	}
	return nil
}

// VerifyNativeSnapshot checks a closed, SQLite-aware native snapshot. It never
// calls Database.Open, migrates data, initializes repositories/caches, loads
// configuration or performs deletion recovery. SQLite may create its ordinary
// empty WAL/shared-memory reader sidecars; they are not archive components.
// Use an isolated restored copy, not the live library or a raw copy of its WAL.
func VerifyNativeSnapshot(ctx context.Context, path string) (*NativeSnapshotReport, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	before, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() < 100 {
		return nil, errors.New("native snapshot must be an existing regular SQLite database")
	}
	if err := snapshotSidecars(abs); err != nil {
		return nil, err
	}
	file, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, opened) {
		return nil, errors.New("native snapshot changed before verification")
	}
	digest, err := snapshotDigest(ctx, file)
	if err != nil {
		return nil, err
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(abs), RawQuery: "mode=ro&_query_only=1&_fk=1&_busy_timeout=5000"}
	conn, err := sqlx.Open(sqlite3Driver, uri.String())
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	// Keep a read snapshot open until both file digests and the sidecar checks
	// agree. An accidental writer cannot checkpoint committed WAL changes away
	// while this reader is active. Lineage validation uses its own read handle.
	tx, err := conn.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var versions []struct {
		Version uint `db:"version"`
		Dirty   bool `db:"dirty"`
	}
	if err := tx.SelectContext(ctx, &versions, "SELECT version,dirty FROM schema_migrations LIMIT 2"); err != nil {
		return nil, fmt.Errorf("reading native snapshot migration ledger: %w", err)
	}
	if len(versions) != 1 || versions[0].Dirty || versions[0].Version != GetRequiredSchemaVersion() {
		return nil, errors.New("native snapshot requires this release's exact clean schema; verification never migrates it")
	}
	var lineage []string
	if err := tx.SelectContext(ctx, &lineage, "SELECT lineage FROM native_schema LIMIT 2"); err != nil {
		return nil, err
	}
	if len(lineage) != 1 || lineage[0] != NativeSchemaLineage {
		return nil, errors.New("native snapshot has a foreign or incomplete lineage")
	}
	if err := snapshotIntegrity(ctx, tx); err != nil {
		return nil, err
	}
	var foreignKeys bool
	if err := tx.GetContext(ctx, &foreignKeys, "SELECT EXISTS(SELECT 1 FROM pragma_foreign_key_check)"); err != nil {
		return nil, err
	}
	if foreignKeys {
		return nil, errors.New("native snapshot has broken foreign keys")
	}
	// This is the same full domain/receipt/provenance validation used before a
	// normal native open, without its writable connection or recovery effects.
	if err := validateDatabaseLineage(abs); err != nil {
		return nil, err
	}
	var pending int64
	if err := tx.GetContext(ctx, &pending, "SELECT count(*) FROM file_deletions"); err != nil {
		return nil, err
	}
	finalDigest, err := snapshotDigest(ctx, file)
	if err != nil {
		return nil, err
	}
	after, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || digest != finalDigest {
		return nil, errors.New("native snapshot changed during verification")
	}
	if err := snapshotSidecars(abs); err != nil {
		return nil, err
	}
	return &NativeSnapshotReport{
		Format: NativeSchemaLineage + ".snapshot-verification", Version: 1,
		Lineage: NativeSchemaLineage, SchemaVersion: versions[0].Version,
		SHA256: digest, Bytes: before.Size(), DatabaseVerified: true,
		PendingFileDeletions: pending, FilesystemRecoveryVerified: false,
	}, nil
}
