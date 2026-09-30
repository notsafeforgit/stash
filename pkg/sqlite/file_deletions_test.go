package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stashapp/stash/pkg/txn"
	"github.com/stretchr/testify/require"
)

const deletionCrashExit = 73

type crashAfterDeletionCommit struct{ *sqlite.Database }

func (m crashAfterDeletionCommit) Commit(ctx context.Context) error {
	if err := m.Database.Commit(ctx); err != nil {
		return err
	}
	os.Exit(deletionCrashExit) // Simulate process death before any post-commit hook.
	return nil
}

func TestDeletionCrashHelper(t *testing.T) {
	phase := os.Getenv("STASH_TEST_DELETION_CRASH")
	if phase == "" {
		return
	}
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(os.Getenv("STASH_TEST_DELETION_DATABASE")))
	r := db.Repository()
	var m txn.Manager = db
	if phase == "after_commit" {
		m = crashAfterDeletionCommit{db}
	}
	require.NoError(t, txn.WithTxn(context.Background(), m, func(ctx context.Context) error {
		d := file.NewDeleterWithTrash(os.Getenv("STASH_TEST_DELETION_TRASH"))
		d.RegisterHooks(ctx)
		path := os.Getenv("STASH_TEST_DELETION_SOURCE")
		f, err := r.File.FindByPath(ctx, path, true)
		require.NoError(t, err)
		require.NotNil(t, f)
		require.NoError(t, file.Destroy(ctx, r.File, f, d, true))
		if phase == "before_commit" {
			os.Exit(deletionCrashExit) // Leave an open transaction and staged file.
		}
		return nil
	}))
	t.Fatal("crash helper did not exit")
}

func newDeletionDatabase(t *testing.T) (*sqlite.Database, string, models.FileID) {
	t.Helper()
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "library.sqlite")))
	parent := t.TempDir()
	path := filepath.Join(parent, strings.Repeat("a", 251)+".mp4")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0644))
	r := db.Repository()
	f := &models.VideoFile{BaseFile: &models.BaseFile{Basename: filepath.Base(path)}}
	require.NoError(t, r.WithTxn(context.Background(), func(ctx context.Context) error {
		folder := &models.Folder{Path: parent}
		if err := r.Folder.Create(ctx, folder); err != nil {
			return err
		}
		f.ParentFolderID = folder.ID
		return r.File.Create(ctx, f)
	}))
	return db, path, f.ID
}

func requireDeletionState(t *testing.T, db *sqlite.Database, path string, id models.FileID, deleted bool) {
	t.Helper()
	r := db.Repository()
	require.NoError(t, r.WithReadTxn(context.Background(), func(ctx context.Context) error {
		files, err := r.File.Find(ctx, id)
		if deleted {
			require.Empty(t, files)
			require.ErrorIs(t, err, sql.ErrNoRows)
			return nil
		} else {
			require.Len(t, files, 1)
		}
		return err
	}))
	if deleted {
		require.NoFileExists(t, path)
	} else {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, "original", string(data))
	}
	require.NoDirExists(t, db.FileDeletionJournalPath())
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT COUNT(*) FROM file_deletions"))
}

func TestFileDeletionRecoversAfterProcessCrash(t *testing.T) {
	for _, phase := range []string{"before_commit", "after_commit"} {
		for _, trash := range []bool{false, true} {
			t.Run(phase+map[bool]string{false: "/delete", true: "/trash"}[trash], func(t *testing.T) {
				db, source, id := newDeletionDatabase(t)
				dbPath := db.DatabasePath()
				journal := db.FileDeletionJournalPath()
				require.NoError(t, db.Close())
				trashPath := ""
				if trash {
					trashPath = t.TempDir()
				}
				cmd := exec.Command(os.Args[0], "-test.run=^TestDeletionCrashHelper$")
				cmd.Env = append(os.Environ(),
					"STASH_TEST_DELETION_CRASH="+phase,
					"STASH_TEST_DELETION_DATABASE="+dbPath,
					"STASH_TEST_DELETION_SOURCE="+source,
					"STASH_TEST_DELETION_TRASH="+trashPath)
				output, err := cmd.CombinedOutput()
				var exitErr *exec.ExitError
				require.ErrorAs(t, err, &exitErr, "%s", output)
				require.Equal(t, deletionCrashExit, exitErr.ExitCode(), "%s", output)
				require.DirExists(t, journal)
				require.NoFileExists(t, source)
				// Opening the database is sufficient: no manual recovery call.
				db = sqlite.NewDatabase()
				require.NoError(t, db.Open(dbPath))
				defer db.Close()
				deleted := phase == "after_commit"
				requireDeletionState(t, db, source, id, deleted)
				if trash {
					matches, err := filepath.Glob(filepath.Join(trashPath, "stash-trash-*", filepath.Base(source)))
					require.NoError(t, err)
					if deleted {
						require.Len(t, matches, 1)
						data, err := os.ReadFile(matches[0])
						require.NoError(t, err)
						require.Equal(t, "original", string(data))
					} else {
						require.Empty(t, matches)
					}
				}
			})
		}
	}
}

func TestFileDeletionLiveCompletionPrunesJournal(t *testing.T) {
	for _, outcome := range []string{"commit", "rollback", "panic"} {
		t.Run(outcome, func(t *testing.T) {
			db, source, id := newDeletionDatabase(t)
			defer db.Close()
			r := db.Repository()
			failure := errors.New("abort deletion")
			run := func() error {
				return r.WithTxn(context.Background(), func(ctx context.Context) error {
					d := file.NewDeleter()
					d.RegisterHooks(ctx)
					require.NoError(t, r.File.Destroy(ctx, id))
					require.NoError(t, d.Files([]string{source}))
					require.DirExists(t, db.FileDeletionJournalPath())
					if outcome == "panic" {
						panic(failure)
					}
					if outcome == "rollback" {
						return failure
					}
					return nil
				})
			}
			switch outcome {
			case "panic":
				require.PanicsWithValue(t, failure, func() { _ = run() })
			case "rollback":
				require.ErrorIs(t, run(), failure)
			default:
				require.NoError(t, run())
			}
			requireDeletionState(t, db, source, id, outcome == "commit")
		})
	}
}

func TestFileDeletionIdleTransactionsAndOrphanMarkers(t *testing.T) {
	db, source, id := newDeletionDatabase(t)
	defer db.Close()
	r := db.Repository()
	require.NoError(t, r.WithTxn(context.Background(), func(ctx context.Context) error {
		file.NewDeleter().RegisterHooks(ctx)
		return nil
	}))
	requireDeletionState(t, db, source, id, false)
	// Crash after journal removal but before its marker was pruned.
	raw := openRawDB(t, db.DatabasePath())
	_, err := raw.Exec("INSERT INTO file_deletions (id) VALUES ('finished-operation')")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.NoError(t, db.RecoverFileDeletions())
	requireDeletionState(t, db, source, id, false)
}

func TestFileDeletionFailedTrashRetainedUntilRetry(t *testing.T) {
	db, source, id := newDeletionDatabase(t)
	defer db.Close()
	trash := filepath.Join(t.TempDir(), "trash")
	// A file at the configured trash path makes transfer fail on all platforms.
	require.NoError(t, os.WriteFile(trash, []byte("blocked"), 0644))
	r := db.Repository()
	require.NoError(t, r.WithTxn(context.Background(), func(ctx context.Context) error {
		d := file.NewDeleterWithTrash(trash)
		d.RegisterHooks(ctx)
		if err := r.File.Destroy(ctx, id); err != nil {
			return err
		}
		return d.Files([]string{source})
	}))
	require.DirExists(t, db.FileDeletionJournalPath())
	staged, err := filepath.Glob(filepath.Join(filepath.Dir(source), ".stash-delete-*", filepath.Base(source)))
	require.NoError(t, err)
	require.Len(t, staged, 1)
	data, err := os.ReadFile(staged[0])
	require.NoError(t, err)
	require.Equal(t, "original", string(data))
	raw := openRawDB(t, db.DatabasePath())
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT COUNT(*) FROM file_deletions"))
	require.NoError(t, raw.Close())
	require.NoError(t, os.Remove(trash))
	require.NoError(t, db.RecoverFileDeletions())
	requireDeletionState(t, db, source, id, true)
	trashed, err := filepath.Glob(filepath.Join(trash, "stash-trash-*", filepath.Base(source)))
	require.NoError(t, err)
	require.Len(t, trashed, 1)
	data, err = os.ReadFile(trashed[0])
	require.NoError(t, err)
	require.Equal(t, "original", string(data))
}

type failedDeletionRename struct{ file.OsFS }

func (failedDeletionRename) Rename(string, string) error { return os.ErrPermission }

func TestFileDeletionIgnoredStagingFailurePreventsCommit(t *testing.T) {
	db, source, id := newDeletionDatabase(t)
	defer db.Close()
	r := db.Repository()
	err := r.WithTxn(context.Background(), func(ctx context.Context) error {
		d := &file.Deleter{RenamerRemover: &failedDeletionRename{}}
		d.RegisterHooks(ctx)
		if err := r.File.Destroy(ctx, id); err != nil {
			return err
		}
		_ = d.Files([]string{source})
		return nil // Some generated-file cleanup callers only log errors.
	})
	require.ErrorIs(t, err, os.ErrPermission)
	requireDeletionState(t, db, source, id, false)
}
