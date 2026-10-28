package sqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestDatabaseBackupStagesOnDestinationFilesystem(t *testing.T) {
	sourceDir, backupDir := t.TempDir(), t.TempDir()
	path := filepath.Join(sourceDir, "library.sqlite")
	writeEmptyNativeFixture(t, path)
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(path))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	attachmentSQL(t, db, "INSERT INTO scenes(id,title,created_at,updated_at) VALUES(31,'preserved',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)")

	// The former staging path can be occupied without blocking the backup or
	// changing anything beside the live database.
	name := "backup # \"quoted\".sqlite"
	oldStaging := filepath.Join(sourceDir, name)
	require.NoError(t, os.WriteFile(oldStaging, []byte("keep this file"), 0600))
	backup := filepath.Join(backupDir, name)
	require.NoError(t, db.Backup(backup))
	contents, err := os.ReadFile(oldStaging)
	require.NoError(t, err)
	require.Equal(t, "keep this file", string(contents))
	entries, err := os.ReadDir(backupDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, name, entries[0].Name())
	raw := openRawDB(t, backup)
	defer raw.Close()
	var title, integrity string
	require.NoError(t, raw.QueryRow("SELECT title FROM scenes WHERE id=31").Scan(&title))
	require.Equal(t, "preserved", title)
	require.NoError(t, raw.QueryRow("PRAGMA quick_check").Scan(&integrity))
	require.Equal(t, "ok", integrity)
}

func TestDatabaseBackupPreservesOccupiedDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.sqlite")
	writeEmptyNativeFixture(t, path)
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(path))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	for _, symlink := range []bool{false, true} {
		directory := t.TempDir()
		original := filepath.Join(directory, "original")
		require.NoError(t, os.WriteFile(original, []byte("previous backup"), 0600))
		backup := original
		if symlink {
			backup = filepath.Join(directory, "link")
			require.NoError(t, os.Symlink(original, backup))
		}
		require.ErrorIs(t, db.Backup(backup), os.ErrExist)
		contents, err := os.ReadFile(original)
		require.NoError(t, err)
		require.Equal(t, "previous backup", string(contents))
		staging, err := filepath.Glob(filepath.Join(directory, ".stash-backup-*"))
		require.NoError(t, err)
		require.Empty(t, staging)
	}
}

func TestConcurrentDatabaseBackupsDoNotReplaceEachOther(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.sqlite")
	writeEmptyNativeFixture(t, path)
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(path))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	directory := t.TempDir()
	backup := filepath.Join(directory, "backup.sqlite")
	start, results := make(chan struct{}), make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			results <- db.Backup(backup)
		}()
	}
	close(start)
	first, second := <-results, <-results
	if first == nil {
		require.Error(t, second)
	} else {
		require.NoError(t, second)
	}
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "backup.sqlite", entries[0].Name())
	raw := openRawDB(t, backup)
	defer raw.Close()
	var integrity string
	require.NoError(t, raw.QueryRow("PRAGMA quick_check").Scan(&integrity))
	require.Equal(t, "ok", integrity)
}

func TestDatabaseLiteralPathsSurviveReopenAndClosedBackup(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"library #first.sqlite", "library #second.sqlite", "library %2F.sqlite", "library & café.sqlite", `library "quoted".sqlite`} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(directory, name)
			writeEmptyNativeFixture(t, path)
			db := sqlite.NewDatabase()
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			require.NoError(t, db.Open(path))
			attachmentSQL(t, db, "INSERT INTO scenes(id,title,created_at,updated_at) VALUES(31,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)", name)
			require.NoError(t, db.Close())

			// Read the named file independently. A URI fragment must not send
			// the write to a shared, truncated filename outside that file.
			raw := openRawDB(t, path)
			var title string
			require.NoError(t, raw.QueryRow("SELECT title FROM scenes WHERE id=31").Scan(&title))
			require.Equal(t, name, title)
			require.NoError(t, raw.Close())

			backup := filepath.Join(directory, "backup "+name)
			require.NoError(t, db.Backup(backup), "a closed database must reopen the same literal path for backup")
			for _, selected := range []string{path, backup} {
				require.NoError(t, db.Open(selected))
				repo := db.Repository()
				require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
					scene, err := repo.Scene.Find(ctx, 31)
					require.NoError(t, err)
					require.NotNil(t, scene)
					require.Equal(t, name, scene.Title)
					return nil
				}))
				require.NoError(t, db.Close())
			}
		})
	}
	require.NoFileExists(t, filepath.Join(directory, "library "), "opening a literal hash must not create a truncated database")
}

func TestDatabaseBackupDoesNotWaitForApplicationWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.sqlite")
	writeEmptyNativeFixture(t, path)
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(path))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	attachmentSQL(t, db, "INSERT INTO scenes(id,title,created_at,updated_at) VALUES(31,'committed',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)")

	// Keep the application's writer occupied, including an uncommitted change.
	// A WAL reader can still copy the previously committed view. Using writeDB
	// for the backup would deadlock here until the writer is released.
	backup := filepath.Join(t.TempDir(), "backup.sqlite")
	done := make(chan error, 1)
	started := false
	repo := db.Repository()
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		scene, err := repo.Scene.Find(ctx, 31)
		if err != nil {
			return err
		}
		scene.Title = "committed after backup"
		if err := repo.Scene.Update(ctx, scene); err != nil {
			return err
		}
		started = true
		go func() { done <- db.Backup(backup) }()
		select {
		case err := <-done:
			done <- err
			return err
		case <-time.After(5 * time.Second):
			t.Error("backup waited for the application's write connection")
			return nil // release the writer so the backup goroutine can finish
		}
	})
	if started {
		require.NoError(t, <-done)
	}
	require.NoError(t, err)

	for _, snapshot := range []struct {
		path, title string
	}{{backup, "committed"}, {path, "committed after backup"}} {
		raw := openRawDB(t, snapshot.path)
		var title, integrity string
		require.NoError(t, raw.QueryRow("SELECT title FROM scenes WHERE id=31").Scan(&title))
		require.Equal(t, snapshot.title, title)
		require.NoError(t, raw.QueryRow("PRAGMA quick_check").Scan(&integrity))
		require.Equal(t, "ok", integrity)
		require.NoError(t, raw.Close())
	}
}
