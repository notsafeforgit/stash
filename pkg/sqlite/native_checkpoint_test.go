package sqlite_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestNativeCheckpointPreservesInterruptedDeletion(t *testing.T) {
	for _, phase := range []string{"before_commit", "after_commit"} {
		t.Run(phase, func(t *testing.T) {
			db, source, _ := newDeletionDatabase(t)
			path, journal := db.DatabasePath(), db.FileDeletionJournalPath()
			require.NoError(t, db.Close())
			command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestDeletionCrashHelper$")
			command.Env = append(os.Environ(), "STASH_TEST_DELETION_CRASH="+phase,
				"STASH_TEST_DELETION_DATABASE="+path, "STASH_TEST_DELETION_SOURCE="+source)
			output, err := command.CombinedOutput()
			var exited *exec.ExitError
			require.ErrorAs(t, err, &exited, string(output))
			require.Equal(t, deletionCrashExit, exited.ExitCode())
			before := snapshotTreeHashes(t, journal)
			staged, err := filepath.Glob(filepath.Join(filepath.Dir(source), ".stash-delete-*"))
			require.NoError(t, err)
			require.Len(t, staged, 1)
			mediaBefore := snapshotTreeHashes(t, staged[0])
			destination := t.TempDir()
			snapshot := filepath.Join(destination, "native ? # snapshot.sqlite")
			var retained *sqlite.NativeCheckpoint
			want := 0
			if phase == "after_commit" {
				want = 1
			}
			require.NoError(t, sqlite.WithNativeCheckpoint(t.Context(), path, func(c *sqlite.NativeCheckpoint) error {
				retained = c
				require.Equal(t, path, c.DatabasePath())
				require.Equal(t, journal, c.FileDeletionJournalPath())
				require.Len(t, c.CommittedDeletionIDs(), want)
				if ids := c.CommittedDeletionIDs(); len(ids) != 0 {
					ids[0] = "caller mutation"
					require.NotEqual(t, ids, c.CommittedDeletionIDs())
				}
				if err := fsutil.CopyPathDurable(journal, filepath.Join(destination, "raw-journal")); err != nil {
					return err
				}
				if err := fsutil.CopyPathDurable(staged[0], filepath.Join(destination, "staged-media")); err != nil {
					return err
				}
				return c.CopyDatabase(snapshot, nil)
			}))
			report, err := sqlite.VerifyNativeSnapshot(t.Context(), snapshot)
			require.NoError(t, err)
			require.EqualValues(t, want, report.PendingFileDeletions)
			require.False(t, report.FilesystemRecoveryVerified)
			require.Equal(t, before, snapshotTreeHashes(t, journal))
			require.Equal(t, mediaBefore, snapshotTreeHashes(t, staged[0]))
			require.NoFileExists(t, source)
			require.ErrorContains(t, retained.CopyDatabase(filepath.Join(destination, "too-late.sqlite"), nil), "no longer active")
			require.NoFileExists(t, filepath.Join(destination, "too-late.sqlite"))
		})
	}
}

func TestNativeCheckpointExcludesConcurrentDeletionStaging(t *testing.T) {
	db, source, id := newDeletionDatabase(t)
	defer db.Close()
	r := db.Repository()
	started := make(chan struct{})
	finished := make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	destination := t.TempDir()
	snapshot := filepath.Join(destination, "snapshot.sqlite")
	require.NoError(t, sqlite.WithNativeCheckpoint(ctx, db.DatabasePath(), func(c *sqlite.NativeCheckpoint) error {
		go func() {
			close(started)
			finished <- r.WithTxn(ctx, func(ctx context.Context) error {
				d := file.NewDeleter()
				d.RegisterHooks(ctx)
				if err := r.File.Destroy(ctx, id); err != nil {
					return err
				}
				return d.Files([]string{source})
			})
		}()
		<-started
		select {
		case err := <-finished:
			t.Fatalf("deletion bypassed checkpoint exclusion: %v", err)
		case <-time.After(75 * time.Millisecond):
		}
		require.FileExists(t, source)
		require.NoDirExists(t, c.FileDeletionJournalPath())
		if err := fsutil.CopyPathDurable(source, filepath.Join(destination, "media.mp4")); err != nil {
			return err
		}
		return c.CopyDatabase(snapshot, nil)
	}))
	require.NoError(t, <-finished)
	require.NoFileExists(t, source)
	require.FileExists(t, filepath.Join(destination, "media.mp4"))
	copyDB := openRawDB(t, snapshot)
	defer copyDB.Close()
	require.EqualValues(t, 1, queryUint(t, copyDB, "SELECT count(*) FROM files"))
	require.Zero(t, queryUint(t, copyDB, "SELECT count(*) FROM file_deletions"))
}

func TestNativeCheckpointRefusesForeignDirtyAndMissingSources(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "base.sqlite")
	writeEmptyNativeFixture(t, fixture)
	for _, mutation := range []string{
		"UPDATE native_schema SET lineage='foreign'",
		"UPDATE schema_migrations SET dirty=1",
		"UPDATE schema_migrations SET version=1000000",
		"UPDATE schema_migrations SET version=9999999",
	} {
		t.Run(mutation, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "invalid.sqlite")
			require.NoError(t, fsutil.CopyPathDurable(fixture, path))
			raw := openRawDB(t, path)
			_, err := raw.Exec(mutation)
			require.NoError(t, err)
			require.NoError(t, raw.Close())
			err = sqlite.WithNativeCheckpoint(t.Context(), path, func(*sqlite.NativeCheckpoint) error {
				t.Fatal("invalid source reached capture")
				return nil
			})
			require.Error(t, err)
		})
	}
	missing := filepath.Join(t.TempDir(), "missing.sqlite")
	require.Error(t, sqlite.WithNativeCheckpoint(t.Context(), missing, func(*sqlite.NativeCheckpoint) error { return nil }))
	require.NoFileExists(t, missing)
	alias := filepath.Join(t.TempDir(), "alias.sqlite")
	require.NoError(t, os.Symlink(fixture, alias))
	require.ErrorContains(t, sqlite.WithNativeCheckpoint(t.Context(), alias, func(*sqlite.NativeCheckpoint) error { return nil }), "regular database")
	require.Error(t, sqlite.WithNativeCheckpoint(t.Context(), fixture, nil))
}

func TestNativeCheckpointCancellationKeepsGuardUntilCaptureStops(t *testing.T) {
	path := filepath.Join(t.TempDir(), "native.sqlite")
	writeEmptyNativeFixture(t, path)
	raw := openRawDB(t, path)
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	_, err := raw.Exec("PRAGMA busy_timeout=5000")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started, finished := make(chan struct{}), make(chan error, 1)
	err = sqlite.WithNativeCheckpoint(ctx, path, func(*sqlite.NativeCheckpoint) error {
		cancel()
		go func() {
			close(started)
			_, err := raw.Exec("INSERT INTO file_deletions(id) VALUES('after-cancel')")
			finished <- err
		}()
		<-started
		select {
		case err := <-finished:
			t.Fatalf("cancellation released the guard while capture was active: %v", err)
		case <-time.After(75 * time.Millisecond):
		}
		return nil // Even an uncooperative callback cannot report success.
	})
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, <-finished)
}

func TestNativeCheckpointCopyFailureCancellationAndOutputProtection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "native.sqlite")
	writeEmptyNativeFixture(t, path)
	destination := t.TempDir()
	existing := filepath.Join(destination, "existing.sqlite")
	require.NoError(t, os.WriteFile(existing, []byte("keep"), 0600))
	withSidecar := filepath.Join(destination, "sidecar.sqlite")
	require.NoError(t, os.WriteFile(withSidecar+"-wal", []byte("keep WAL"), 0600))
	want := errors.New("reserve exhausted")
	err := sqlite.WithNativeCheckpoint(t.Context(), path, func(c *sqlite.NativeCheckpoint) error {
		require.Error(t, c.CopyDatabase(existing, nil))
		require.ErrorContains(t, c.CopyDatabase(withSidecar, nil), "existing SQLite sidecars")
		failed := filepath.Join(destination, "failed.sqlite")
		checks := 0
		err := c.CopyDatabase(failed, func(remaining int64) error {
			checks++
			if checks == 1 {
				require.Positive(t, remaining)
				return nil
			}
			return want
		})
		require.ErrorIs(t, err, want)
		require.NoFileExists(t, failed)
		return want
	})
	require.ErrorIs(t, err, want)
	body, err := os.ReadFile(existing)
	require.NoError(t, err)
	require.Equal(t, "keep", string(body))
	require.NoFileExists(t, withSidecar)
	body, err = os.ReadFile(withSidecar + "-wal")
	require.NoError(t, err)
	require.Equal(t, "keep WAL", string(body))
	ctx, cancel := context.WithCancel(t.Context())
	cancelled := filepath.Join(destination, "cancelled.sqlite")
	err = sqlite.WithNativeCheckpoint(ctx, path, func(c *sqlite.NativeCheckpoint) error {
		return c.CopyDatabase(cancelled, func(int64) error {
			cancel()
			return nil
		})
	})
	require.ErrorIs(t, err, context.Canceled)
	require.NoFileExists(t, cancelled)
	// Both callback failure and cancellation release the writer lock.
	require.NoError(t, sqlite.WithNativeCheckpoint(t.Context(), path, func(c *sqlite.NativeCheckpoint) error {
		return c.CopyDatabase(filepath.Join(destination, "retry.sqlite"), nil)
	}))
}
