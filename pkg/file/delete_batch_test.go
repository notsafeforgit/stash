package file

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBatchDeletionDefersMovesUntilFlush(t *testing.T) {
	for _, outcome := range []string{"commit", "rollback", "trash"} {
		t.Run(outcome, func(t *testing.T) {
			d, root, committed := journalDeleter(t)
			d.batch = true
			parent := t.TempDir()
			paths := []string{filepath.Join(parent, "first.mp4"), filepath.Join(parent, "second.mp4")}
			for _, path := range paths {
				writeDeletionFixture(t, path, false, path)
			}
			if outcome == "trash" {
				d.TrashPath = t.TempDir()
			}
			require.NoError(t, d.Files(paths))
			require.NoError(t, d.Files(paths)) // Shared paths are staged only once.
			require.NoDirExists(t, root)
			require.Empty(t, committed)
			for _, path := range paths {
				requireDeletionContents(t, path, false, path)
			}
			rr := newDeletionRenamerRemover()
			rename := rr.RenameFn
			rr.RenameFn = func(from, to string) error {
				// Before even the first rename, every published record is readable
				// and its marker has been added to the enclosing transaction.
				entries := requireDirectoryEntries(t, root, len(paths))
				for _, entry := range entries {
					record, err := readDeletionRecord(filepath.Join(root, entry.Name()))
					require.NoError(t, err)
					require.True(t, committed[record.ID])
				}
				return rename(from, to)
			}
			d.RenamerRemover = rr
			require.NoError(t, d.Flush(context.Background()))
			require.NoError(t, d.Flush(context.Background())) // No duplicate staging.
			for _, path := range paths {
				require.NoFileExists(t, path)
			}
			if outcome == "rollback" {
				clear(committed)
			}
			remaining, err := RecoverDeletionJournal(root, committed)
			require.NoError(t, err)
			require.Empty(t, remaining)
			require.NoDirExists(t, root)
			for _, path := range paths {
				if outcome == "rollback" {
					requireDeletionContents(t, path, false, path)
				} else {
					require.NoFileExists(t, path)
				}
				if outcome == "trash" {
					matches, err := filepath.Glob(filepath.Join(d.TrashPath, "stash-trash-*", filepath.Base(path)))
					require.NoError(t, err)
					require.Len(t, matches, 1)
					requireDeletionContents(t, matches[0], false, path)
				}
			}
		})
	}
}

func TestBatchDeletionFailureRestoresEveryOriginal(t *testing.T) {
	for _, phase := range []string{"prepare", "mark", "rename", "cancel"} {
		t.Run(phase, func(t *testing.T) {
			d, root, committed := journalDeleter(t)
			d.batch = true
			parent := t.TempDir()
			paths := []string{filepath.Join(parent, "first.mp4"), filepath.Join(parent, "other.mp4")}
			for _, path := range paths {
				writeDeletionFixture(t, path, false, path)
			}
			require.NoError(t, d.Files(paths))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			rr := newDeletionRenamerRemover()
			rename, mkdir := rr.RenameFn, rr.MkdirTempFn
			calls := 0
			switch phase {
			case "prepare":
				rr.MkdirTempFn = func(dir, pattern string) (string, error) {
					calls++
					if calls == 2 {
						return "", os.ErrPermission
					}
					return mkdir(dir, pattern)
				}
			case "mark":
				d.journal.MarkCommitted = func(string) error { return os.ErrPermission }
			case "rename", "cancel":
				rr.RenameFn = func(from, to string) error {
					calls++
					if calls == 2 {
						return os.ErrPermission
					}
					if phase == "cancel" {
						cancel()
					}
					return rename(from, to)
				}
			}
			d.RenamerRemover = rr
			err := d.Flush(ctx)
			if phase == "cancel" {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorIs(t, err, os.ErrPermission)
			}
			require.Error(t, d.Flush(context.Background())) // A later flush cannot hide failure.
			clear(committed)
			remaining, err := RecoverDeletionJournal(root, committed)
			require.NoError(t, err)
			require.Empty(t, remaining)
			require.NoDirExists(t, root)
			for _, path := range paths {
				requireDeletionContents(t, path, false, path)
			}
			requireDirectoryEntries(t, parent, len(paths))
		})
	}
}

func TestBatchDeletionRejectsChangedPaths(t *testing.T) {
	for _, replacement := range []string{"source", "parent", "before_rename"} {
		t.Run(replacement, func(t *testing.T) {
			d, root, _ := journalDeleter(t)
			d.batch = true
			parent := filepath.Join(t.TempDir(), "media")
			require.NoError(t, os.Mkdir(parent, 0700))
			source := filepath.Join(parent, "video.mp4")
			writeDeletionFixture(t, source, false, "original")
			require.NoError(t, d.Files([]string{source}))
			replace := func() {
				require.NoError(t, os.Rename(source, source+".retained"))
				writeDeletionFixture(t, source, false, "replacement")
			}
			switch replacement {
			case "source":
				replace()
			case "parent":
				require.NoError(t, os.Rename(parent, parent+".retained"))
				require.NoError(t, os.Mkdir(parent, 0700))
				writeDeletionFixture(t, source, false, "replacement")
			case "before_rename":
				mark := d.journal.MarkCommitted
				d.journal.MarkCommitted = func(id string) error {
					replace()
					return mark(id)
				}
			}
			require.ErrorContains(t, d.Flush(context.Background()), "replaced")
			requireDeletionContents(t, source, false, "replacement")
			// There was no move of an original; recovery must not delete the
			// replacement even when the empty reservation has a journal record.
			_, _ = RecoverDeletionJournal(root, nil)
			requireDeletionContents(t, source, false, "replacement")
			retained := source + ".retained"
			if replacement == "parent" {
				retained = filepath.Join(parent+".retained", filepath.Base(source))
			}
			requireDeletionContents(t, retained, false, "original")
		})
	}
}

func TestBatchDeletionNestedPaths(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(fmt.Sprint(commit), func(t *testing.T) {
			d, root, committed := journalDeleter(t)
			d.batch = true
			parent := t.TempDir()
			folder := filepath.Join(parent, "folder")
			writeDeletionFixture(t, folder, true, "contents")
			// The caller may queue a parent before its child. Flush orders them.
			require.NoError(t, d.Dirs([]string{folder}))
			require.NoError(t, d.Files([]string{filepath.Join(folder, "child.txt")}))
			require.NoError(t, d.Flush(context.Background()))
			if !commit {
				clear(committed)
			}
			remaining, err := RecoverDeletionJournal(root, committed)
			require.NoError(t, err)
			require.Empty(t, remaining)
			if commit {
				requireDirectoryEntries(t, parent, 0)
			} else {
				requireDeletionContents(t, folder, true, "contents")
			}
		})
	}
}

func TestBatchDeletionUnexpectedStagingContentsRemainPending(t *testing.T) {
	d, root, committed := journalDeleter(t)
	d.batch = true
	parent := t.TempDir()
	for i := range 2 {
		folder := filepath.Join(parent, fmt.Sprint(i))
		require.NoError(t, os.Mkdir(folder, 0700))
		path := filepath.Join(folder, "video.mp4")
		writeDeletionFixture(t, path, false, "original")
		require.NoError(t, d.Files([]string{path}))
	}
	require.NoError(t, d.Flush(context.Background()))
	r := d.pending[0]
	unexpected := filepath.Join(r.StageDir, "unrelated")
	writeDeletionFixture(t, unexpected, false, "unrelated")
	remaining, err := RecoverDeletionJournal(root, committed)
	require.Error(t, err)
	require.Equal(t, map[string]bool{r.ID: true}, remaining)
	requireDeletionContents(t, unexpected, false, "unrelated")
	require.NoDirExists(t, d.pending[1].StageDir)
	require.NoError(t, os.Remove(unexpected))
	remaining, err = RecoverDeletionJournal(root, committed)
	require.NoError(t, err)
	require.Empty(t, remaining)
	require.NoDirExists(t, root)
}

func TestBatchDeletionRecoveryAcrossFolders(t *testing.T) {
	d, root, committed := journalDeleter(t)
	d.batch = true
	parent := t.TempDir()
	for i := range 24 {
		folder := filepath.Join(parent, fmt.Sprint(i))
		require.NoError(t, os.Mkdir(folder, 0700))
		path := filepath.Join(folder, "image.jpg")
		writeDeletionFixture(t, path, false, "original")
		writeDeletionFixture(t, filepath.Join(folder, "untouched.jpg"), false, "unrelated")
		require.NoError(t, d.Files([]string{path}))
	}
	require.NoError(t, d.Flush(context.Background()))
	// A journal may contain interrupted transactions with different outcomes.
	for i, record := range d.pending {
		if i%2 == 0 {
			delete(committed, record.ID)
		}
	}
	remaining, err := RecoverDeletionJournal(root, committed)
	require.NoError(t, err)
	require.Empty(t, remaining)
	require.NoDirExists(t, root)
	for _, record := range d.pending {
		if committed[record.ID] {
			require.NoFileExists(t, record.Original)
		} else {
			requireDeletionContents(t, record.Original, false, "original")
		}
		requireDeletionContents(t, filepath.Join(filepath.Dir(record.Original), "untouched.jpg"), false, "unrelated")
	}
}

// An optional root puts only these disposable fixtures on the media filesystem.
// It never discovers, modifies or removes pre-existing media.
func BenchmarkDeletion120(b *testing.B) {
	for _, batch := range []bool{false, true} {
		b.Run(map[bool]string{false: "serial", true: "batch"}[batch], func(b *testing.B) {
			parent, err := os.MkdirTemp(os.Getenv("STASH_TEST_DELETION_BENCH_ROOT"), ".stash-delete-benchmark-")
			require.NoError(b, err)
			defer os.RemoveAll(parent)
			for b.Loop() {
				b.StopTimer()
				paths := make([]string, 120)
				for i := range paths {
					folder := parent
					if os.Getenv("STASH_TEST_DELETION_BENCH_LAYOUT") == "scattered" {
						folder = filepath.Join(parent, fmt.Sprint(i))
						require.NoError(b, os.MkdirAll(folder, 0700))
					}
					paths[i] = filepath.Join(folder, fmt.Sprintf("image-%03d.jpg", i))
					require.NoError(b, os.WriteFile(paths[i], []byte("fixture"), 0600))
				}
				d := NewDeleter()
				d.batch = batch
				committed := map[string]bool{}
				d.journal = &DeletionJournal{
					Directory:     filepath.Join(parent, "journal"),
					MarkCommitted: func(id string) error { committed[id] = true; return nil },
				}
				b.StartTimer()
				require.NoError(b, d.Files(paths))
				require.NoError(b, d.Flush(context.Background()))
				remaining, err := RecoverDeletionJournal(d.journal.Directory, committed)
				require.NoError(b, err)
				if len(remaining) != 0 {
					b.Fatal(errors.New("unfinished deletion"))
				}
			}
		})
	}
}
