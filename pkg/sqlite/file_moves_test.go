package sqlite_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stashapp/stash/pkg/txn"
	"github.com/stretchr/testify/require"
)

type moveCrashFS struct {
	file.DirMakerStatRenamer
	phase string
	calls int
}

func (f *moveCrashFS) Rename(from, to string) error {
	f.calls++
	if f.phase == "before_stage" && f.calls == 1 {
		os.Exit(deletionCrashExit)
	}
	if f.phase == "failed_publish" && f.calls == 2 {
		return errors.New("injected publication failure")
	}
	err := f.DirMakerStatRenamer.Rename(from, to)
	if err == nil && ((f.phase == "after_stage" && f.calls == 1) ||
		(f.phase == "after_publish" && f.calls == 2) || (f.phase == "second_stage" && f.calls == 3)) {
		os.Exit(deletionCrashExit)
	}
	return err
}

func moveFixtureFile(t *testing.T, db *sqlite.Database, source, destination, phase string) error {
	return moveFixtureFiles(t, db, []string{source}, []string{destination}, phase)
}

func moveFixtureFiles(t *testing.T, db *sqlite.Database, sources, destinations []string, phase string) error {
	t.Helper()
	source, destination := sources[0], destinations[0]
	r := db.Repository()
	var m txn.Manager = db
	if phase == "after_commit" {
		m = crashAfterDeletionCommit{db}
	}
	return txn.WithTxnUntilComplete(t.Context(), m, func(ctx context.Context) error {
		roots := []string{filepath.Dir(source), filepath.Dir(destination)}
		if relative, err := filepath.Rel(roots[0], roots[1]); err == nil && filepath.IsLocal(relative) {
			roots = roots[:1]
		}
		mover := file.NewMover(r.File, r.Folder, roots)
		mover.RegisterHooks(ctx)
		mover.Renamer = &moveCrashFS{DirMakerStatRenamer: mover.Renamer, phase: phase}
		folder, err := file.GetOrCreateFolderHierarchy(ctx, r.Folder, filepath.Dir(destination), roots)
		if err != nil {
			return err
		}
		if err := mover.CreateFolderHierarchy(folder.Path); err != nil {
			return err
		}
		for i, source := range sources {
			f, err := r.File.FindByPath(ctx, source, true)
			if err != nil {
				return err
			}
			if err := mover.Move(ctx, f, folder, filepath.Base(destinations[i])); err != nil {
				return err
			}
		}
		if phase == "before_commit" {
			os.Exit(deletionCrashExit)
		}
		if phase == "rollback" {
			return errors.New("injected rollback")
		}
		return nil
	})
}

func TestMoveCrashHelper(t *testing.T) {
	phase := os.Getenv("STASH_TEST_MOVE_CRASH")
	if phase == "" {
		return
	}
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(os.Getenv("STASH_TEST_MOVE_DATABASE")))
	sources, destinations := []string{os.Getenv("STASH_TEST_MOVE_SOURCE")}, []string{os.Getenv("STASH_TEST_MOVE_DESTINATION")}
	if os.Getenv("STASH_TEST_MOVE_BATCH") != "" {
		sources = append(sources, filepath.Join(filepath.Dir(sources[0]), "second.mp4"))
		destinations = append(destinations, filepath.Join(filepath.Dir(destinations[0]), "second.mp4"))
	}
	require.NoError(t, moveFixtureFiles(t, db, sources, destinations, phase))
	t.Fatal("crash helper did not exit")
}

func crashMove(t *testing.T, db *sqlite.Database, source, destination, phase string) string {
	t.Helper()
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	cmd := exec.Command(os.Args[0], "-test.run=^TestMoveCrashHelper$")
	cmd.Env = append(os.Environ(), "STASH_TEST_MOVE_CRASH="+phase, "STASH_TEST_MOVE_DATABASE="+path,
		"STASH_TEST_MOVE_SOURCE="+source, "STASH_TEST_MOVE_DESTINATION="+destination)
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit, "%s", output)
	require.Equal(t, deletionCrashExit, exit.ExitCode(), "%s", output)
	return path
}

func requireMoveState(t *testing.T, db *sqlite.Database, id models.FileID, source, destination string, committed bool) {
	t.Helper()
	want, absent := source, destination
	if committed {
		want, absent = destination, source
	}
	r := db.Repository()
	require.NoError(t, r.WithReadTxn(t.Context(), func(ctx context.Context) error {
		files, err := r.File.Find(ctx, id)
		if err != nil {
			return err
		}
		require.Len(t, files, 1)
		require.Equal(t, want, files[0].Base().Path)
		return nil
	}))
	body, err := os.ReadFile(want)
	require.NoError(t, err)
	require.Equal(t, "original", string(body))
	require.NoFileExists(t, absent)
	require.NoDirExists(t, db.FileMoveJournalPath())
	for _, parent := range []string{filepath.Dir(source), filepath.Dir(destination)} {
		dirs, err := filepath.Glob(filepath.Join(parent, ".stash-move-*"))
		require.NoError(t, err)
		require.Empty(t, dirs)
	}
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM file_moves"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestFileMoveCrashRecovery(t *testing.T) {
	for _, phase := range []string{"before_stage", "after_stage", "after_publish", "before_commit", "after_commit"} {
		t.Run(phase, func(t *testing.T) {
			db, source, id := newDeletionDatabase(t)
			destination := filepath.Join(filepath.Dir(source), "moved.mp4")
			path := crashMove(t, db, source, destination, phase)
			// The checkpoint must reject both committed and uncommitted intents
			// instead of silently producing an archive without their recovery.
			called := false
			err := sqlite.WithNativeCheckpoint(t.Context(), path, func(*sqlite.NativeCheckpoint) error {
				called = true
				return nil
			})
			require.ErrorContains(t, err, "move recovery")
			require.False(t, called)
			for range 2 {
				db = sqlite.NewDatabase()
				require.NoError(t, db.Open(path))
				requireMoveState(t, db, id, source, destination, phase == "after_commit")
				require.NoError(t, db.Close())
			}
		})
	}
}

func TestFileMoveBatchRecoversSharedDirectories(t *testing.T) {
	for _, phase := range []string{"second_stage", "before_commit", "after_commit"} {
		t.Run(phase, func(t *testing.T) {
			t.Setenv("STASH_TEST_MOVE_BATCH", "1")
			db, source, id := newDeletionDatabase(t)
			destination := filepath.Join(filepath.Dir(source), "new", "nested", "moved.mp4")
			secondSource := filepath.Join(filepath.Dir(source), "second.mp4")
			require.NoError(t, os.WriteFile(secondSource, []byte("original"), 0600))
			r := db.Repository()
			var second models.BaseFile
			require.NoError(t, r.WithTxn(t.Context(), func(ctx context.Context) error {
				first, err := r.File.FindByPath(ctx, source, true)
				if err != nil {
					return err
				}
				second = models.BaseFile{ParentFolderID: first.Base().ParentFolderID, Basename: "second.mp4"}
				return r.File.Create(ctx, &second)
			}))
			path := crashMove(t, db, source, destination, phase)
			db = sqlite.NewDatabase()
			require.NoError(t, db.Open(path))
			defer db.Close()
			requireMoveState(t, db, id, source, destination, phase == "after_commit")
			requireMoveState(t, db, second.ID, secondSource, filepath.Join(filepath.Dir(destination), "second.mp4"), phase == "after_commit")
			if phase != "after_commit" {
				require.NoDirExists(t, filepath.Join(filepath.Dir(source), "new"))
			}
		})
	}
}

func TestFileMovePreservesArchiveEntryHierarchy(t *testing.T) {
	for _, phase := range []string{"before_commit", "after_commit"} {
		t.Run(phase, func(t *testing.T) {
			db, source, id := newDeletionDatabase(t)
			destination := filepath.Join(filepath.Dir(source), "moved.zip")
			r := db.Repository()
			child := &models.ImageFile{BaseFile: &models.BaseFile{Basename: "image.jpg", DirEntry: models.DirEntry{ZipFileID: &id}}}
			require.NoError(t, r.WithTxn(t.Context(), func(ctx context.Context) error {
				// Moving the container transfers catalog entries without
				// decompressing or modifying its bytes.
				folder := &models.Folder{Path: source, DirEntry: models.DirEntry{ZipFileID: &id}}
				if err := r.Folder.Create(ctx, folder); err != nil {
					return err
				}
				child.ParentFolderID = folder.ID
				return r.File.Create(ctx, child)
			}))
			path := crashMove(t, db, source, destination, phase)
			db = sqlite.NewDatabase()
			require.NoError(t, db.Open(path))
			defer db.Close()
			requireMoveState(t, db, id, source, destination, phase == "after_commit")
			want := source
			if phase == "after_commit" {
				want = destination
			}
			r = db.Repository()
			require.NoError(t, r.WithReadTxn(t.Context(), func(ctx context.Context) error {
				files, err := r.File.Find(ctx, child.ID)
				if err != nil {
					return err
				}
				require.Len(t, files, 1)
				require.Equal(t, filepath.Join(want, "image.jpg"), files[0].Base().Path)
				require.Equal(t, id, *files[0].Base().ZipFileID)
				folders, err := r.Folder.FindByZipFileID(ctx, id)
				require.NoError(t, err)
				require.Len(t, folders, 1)
				require.Equal(t, want, folders[0].Path)
				return nil
			}))
		})
	}
}

func TestFileMoveCommitRollbackAndFailure(t *testing.T) {
	for _, phase := range []string{"commit", "rollback", "failed_publish"} {
		t.Run(phase, func(t *testing.T) {
			db, source, id := newDeletionDatabase(t)
			defer db.Close()
			destination := filepath.Join(filepath.Dir(source), strings.Repeat("b", 251)+".mp4")
			err := moveFixtureFile(t, db, source, destination, phase)
			if phase == "commit" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			requireMoveState(t, db, id, source, destination, phase == "commit")
		})
	}
}

func TestFileMoveCrossDeviceRecovery(t *testing.T) {
	root := os.Getenv("STASH_TEST_MOVE_ROOT")
	if root == "" {
		t.Skip("set STASH_TEST_MOVE_ROOT to a writable filesystem different from the temp directory")
	}
	for _, phase := range []string{"commit", "rollback", "before_commit", "after_commit"} {
		t.Run(phase, func(t *testing.T) {
			dir, err := os.MkdirTemp(root, ".move-test-")
			require.NoError(t, err)
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			db, source, id := newDeletionDatabase(t)
			destination := filepath.Join(dir, filepath.Base(source))
			if phase == "before_commit" || phase == "after_commit" {
				path := crashMove(t, db, source, destination, phase)
				db = sqlite.NewDatabase()
				require.NoError(t, db.Open(path))
			} else {
				err = moveFixtureFile(t, db, source, destination, phase)
				if phase == "commit" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			}
			defer db.Close()
			requireMoveState(t, db, id, source, destination, phase == "commit" || phase == "after_commit")
		})
	}
}

func TestFileMoveConflictRetainsRecoveryAndBlocksAdmission(t *testing.T) {
	db, source, id := newDeletionDatabase(t)
	destination := filepath.Join(filepath.Dir(source), "moved.mp4")
	path := crashMove(t, db, source, destination, "before_commit")
	require.NoError(t, os.WriteFile(source, []byte("replacement"), 0600))
	db = sqlite.NewDatabase()
	require.NoError(t, db.Open(path))
	defer db.Close()
	require.DirExists(t, db.FileMoveJournalPath())
	body, err := os.ReadFile(source)
	require.NoError(t, err)
	require.Equal(t, "replacement", string(body))
	body, err = os.ReadFile(destination)
	require.NoError(t, err)
	require.Equal(t, "original", string(body))
	require.ErrorContains(t, moveFixtureFile(t, db, source, filepath.Join(filepath.Dir(source), "another.mp4"), "commit"), "move recovery")
	require.NoFileExists(t, filepath.Join(filepath.Dir(source), "another.mp4"))
	r := db.Repository()
	require.ErrorContains(t, r.WithTxn(t.Context(), func(ctx context.Context) error {
		return r.File.Destroy(ctx, id)
	}), "move recovery")
	require.ErrorContains(t, r.WithTxn(t.Context(), func(ctx context.Context) error {
		files, err := r.File.Find(ctx, id)
		if err != nil {
			return err
		}
		files[0].Base().Size++
		return r.File.Update(ctx, files[0])
	}), "move recovery")
	require.ErrorContains(t, r.WithTxn(t.Context(), func(ctx context.Context) error {
		folder, err := r.Folder.FindByPath(ctx, filepath.Dir(source), true)
		if err != nil {
			return err
		}
		return r.Folder.Destroy(ctx, folder.ID)
	}), "move recovery")
	require.NoError(t, r.WithTxn(t.Context(), func(ctx context.Context) error {
		// Recovery of one conflicting file must not stall metadata work.
		tag := models.NewTag()
		tag.Name = "unrelated metadata"
		return r.Tag.Create(ctx, &models.CreateTagInput{Tag: &tag})
	}))
	require.NoError(t, r.WithTxn(t.Context(), func(ctx context.Context) error {
		folder, err := r.Folder.FindByPath(ctx, filepath.Dir(source), true)
		if err != nil {
			return err
		}
		folder.ModTime = folder.ModTime.Add(time.Second)
		return r.Folder.Update(ctx, folder)
	}))
	require.NoError(t, r.WithTxn(t.Context(), func(ctx context.Context) error {
		files, err := r.File.Find(ctx, id)
		if err != nil {
			return err
		}
		other := &models.BaseFile{ParentFolderID: files[0].Base().ParentFolderID, Basename: "unrelated.mp4"}
		return r.File.Create(ctx, other)
	}))
	// Explicitly resolve the fixture's conflicting external file, then retry.
	require.NoError(t, os.Remove(source))
	require.NoError(t, db.RecoverFileMoves())
	require.NoError(t, db.RecoverFileMoves())
	requireMoveState(t, db, id, source, destination, false)
}

func TestFileMoveOrphanMarkerMustBeRecoveredBeforeAdmission(t *testing.T) {
	db, source, id := newDeletionDatabase(t)
	defer db.Close()
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec("INSERT INTO file_moves(id) VALUES(?)", uuid.NewString())
	require.NoError(t, err)
	destination := filepath.Join(filepath.Dir(source), "moved.mp4")
	require.ErrorContains(t, moveFixtureFile(t, db, source, destination, "commit"), "move recovery")
	requireMoveState(t, db, id, source, destination, false)
	require.NoError(t, moveFixtureFile(t, db, source, destination, "commit"))
	requireMoveState(t, db, id, source, destination, true)
}

func TestFileMoveNewDirectoriesRecoverOnRollback(t *testing.T) {
	for _, phase := range []string{"rollback", "before_commit"} {
		t.Run(phase, func(t *testing.T) {
			db, source, id := newDeletionDatabase(t)
			destination := filepath.Join(filepath.Dir(source), "new", "nested", "moved.mp4")
			if phase == "before_commit" {
				path := crashMove(t, db, source, destination, phase)
				db = sqlite.NewDatabase()
				require.NoError(t, db.Open(path))
			} else {
				require.Error(t, moveFixtureFile(t, db, source, destination, phase))
			}
			defer db.Close()
			requireMoveState(t, db, id, source, destination, false)
			require.NoDirExists(t, filepath.Join(filepath.Dir(source), "new"))
		})
	}
}

type pausedMoveFS struct {
	file.DirMakerStatRenamer
	ctx       context.Context
	staged    chan struct{}
	cancelled chan struct{}
	resume    <-chan struct{}
}

func (f *pausedMoveFS) Rename(from, to string) error {
	err := f.DirMakerStatRenamer.Rename(from, to)
	if err == nil {
		close(f.staged)
		<-f.ctx.Done()
		close(f.cancelled)
		<-f.resume
	}
	return err
}

func TestFileMoveCancellationKeepsWriterLockUntilFilesystemWorkStops(t *testing.T) {
	db, source, id := newDeletionDatabase(t)
	defer db.Close()
	r := db.Repository()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	resume, release := context.WithCancel(context.Background())
	defer release()
	staged, cancelled := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	destination := filepath.Join(filepath.Dir(source), "moved.mp4")
	go func() {
		done <- txn.WithTxnUntilComplete(ctx, db, func(ctx context.Context) error {
			f, err := r.File.FindByPath(ctx, source, true)
			if err != nil {
				return err
			}
			folder, err := r.Folder.Find(ctx, f.Base().ParentFolderID)
			if err != nil {
				return err
			}
			mover := file.NewMover(r.File, r.Folder, []string{folder.Path})
			mover.RegisterHooks(ctx)
			mover.Renamer = &pausedMoveFS{DirMakerStatRenamer: mover.Renamer, ctx: ctx, staged: staged, cancelled: cancelled, resume: resume.Done()}
			return mover.Move(ctx, f, folder, filepath.Base(destination))
		})
	}()
	select {
	case <-staged:
	case err := <-done:
		t.Fatalf("move failed before staging: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("move did not stage")
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(10 * time.Second):
		t.Fatal("move did not observe cancellation")
	}
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	_, err := raw.Exec("PRAGMA busy_timeout=50")
	require.NoError(t, err)
	_, lockErr := raw.Exec("BEGIN IMMEDIATE")
	if lockErr == nil {
		_, _ = raw.Exec("ROLLBACK")
	}
	release()
	require.ErrorIs(t, <-done, context.Canceled)
	require.ErrorContains(t, lockErr, "locked", "recovery must not acquire the writer while the cancelled move is still active")
	requireMoveState(t, db, id, source, destination, false)
}

func TestFileMoveRejectsPathsOutsideLibrary(t *testing.T) {
	for _, name := range []string{"../escaped.mp4", `..\escaped.mp4`, "/escaped.mp4", "a/b.mp4", ".", "..", "file:stream.mp4"} {
		t.Run(name, func(t *testing.T) {
			db, source, _ := newDeletionDatabase(t)
			defer db.Close()
			r := db.Repository()
			err := r.WithTxn(t.Context(), func(ctx context.Context) error {
				f, err := r.File.FindByPath(ctx, source, true)
				if err != nil {
					return err
				}
				folder, err := r.Folder.Find(ctx, f.Base().ParentFolderID)
				if err != nil {
					return err
				}
				mover := file.NewMover(r.File, r.Folder, []string{filepath.Dir(source)})
				mover.RegisterHooks(ctx)
				return mover.Move(ctx, f, folder, name)
			})
			require.ErrorContains(t, err, "single filename")
			require.FileExists(t, source)
			require.NoDirExists(t, db.FileMoveJournalPath())
		})
	}
}
