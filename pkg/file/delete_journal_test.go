package file

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stretchr/testify/require"
)

func journalDeleter(t *testing.T) (*Deleter, string, map[string]bool) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "journal")
	committed := make(map[string]bool)
	d := NewDeleter()
	d.journal = &DeletionJournal{
		Directory: root,
		MarkCommitted: func(id string) error {
			committed[id] = true
			return nil
		},
	}
	return d, root, committed
}

func TestDeletionJournalRecoveryAndPruning(t *testing.T) {
	for _, outcome := range []string{"rollback", "commit", "trash"} {
		t.Run(outcome, func(t *testing.T) {
			d, root, committed := journalDeleter(t)
			parent := t.TempDir()
			source := filepath.Join(parent, "video.mp4")
			writeDeletionFixture(t, source, false, "media")
			trash := t.TempDir()
			if outcome == "trash" {
				d.TrashPath = trash
			}
			require.NoError(t, d.Files([]string{source}))
			requireDirectoryEntries(t, root, 1)
			if outcome == "rollback" {
				clear(committed)
			}
			remaining, err := RecoverDeletionJournal(root, committed)
			require.NoError(t, err)
			require.Empty(t, remaining)
			require.NoDirExists(t, root)
			if outcome == "rollback" {
				requireDeletionContents(t, source, false, "media")
				requireDirectoryEntries(t, parent, 1)
			} else {
				requireDirectoryEntries(t, parent, 0)
			}
			if outcome == "trash" {
				entry := requireDirectoryEntries(t, trash, 1)[0]
				requireDeletionContents(t, filepath.Join(trash, entry.Name(), "video.mp4"), false, "media")
			}
			// Retrying completed work neither recreates the journal nor affects data.
			remaining, err = RecoverDeletionJournal(root, committed)
			require.NoError(t, err)
			require.Empty(t, remaining)
			require.NoDirExists(t, root)
		})
	}
}

func TestDeletionJournalRestoreConflictKeepsBothFiles(t *testing.T) {
	d, root, _ := journalDeleter(t)
	source := filepath.Join(t.TempDir(), "video.mp4")
	writeDeletionFixture(t, source, false, "original")
	require.NoError(t, d.Files([]string{source}))
	staged := d.pending[0].Staged
	writeDeletionFixture(t, source, false, "replacement")
	remaining, err := RecoverDeletionJournal(root, nil)
	require.Error(t, err)
	require.Len(t, remaining, 1)
	requireDirectoryEntries(t, root, 1)
	requireDeletionContents(t, source, false, "replacement")
	requireDeletionContents(t, staged, false, "original")
	require.NoError(t, os.Remove(source))
	remaining, err = RecoverDeletionJournal(root, nil)
	require.NoError(t, err)
	require.Empty(t, remaining)
	requireDeletionContents(t, source, false, "original")
	require.NoDirExists(t, root)
}

func TestDeletionJournalRetainsOnlyFailedOperations(t *testing.T) {
	d, root, committed := journalDeleter(t)
	parent := t.TempDir()
	first := filepath.Join(parent, "first.mp4")
	second := filepath.Join(parent, "second.mp4")
	writeDeletionFixture(t, first, false, "first")
	writeDeletionFixture(t, second, false, "second")
	require.NoError(t, d.Files([]string{first, second}))
	r := d.pending[1]
	// Simulate replacement of a staged object. Recovery must not unlink it.
	retained := filepath.Join(r.StageDir, "retained")
	require.NoError(t, os.Rename(r.Staged, retained))
	writeDeletionFixture(t, r.Staged, false, "unrelated")
	remaining, err := RecoverDeletionJournal(root, committed)
	require.Error(t, err)
	require.Equal(t, map[string]bool{r.ID: true}, remaining)
	requireDirectoryEntries(t, root, 1)
	requireDeletionContents(t, r.Staged, false, "unrelated")
	requireDeletionContents(t, retained, false, "second")
	require.NoDirExists(t, d.pending[0].StageDir)
	require.NoError(t, os.Remove(r.Staged))
	require.NoError(t, os.Rename(retained, r.Staged))
	remaining, err = RecoverDeletionJournal(root, committed)
	require.NoError(t, err)
	require.Empty(t, remaining)
	require.NoDirExists(t, root)
	requireDirectoryEntries(t, parent, 0)
}

func TestDeletionJournalNestedPaths(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[commit], func(t *testing.T) {
			d, root, committed := journalDeleter(t)
			parent := t.TempDir()
			folder := filepath.Join(parent, "folder")
			writeDeletionFixture(t, folder, true, "contents")
			require.NoError(t, d.Files([]string{filepath.Join(folder, "child.txt")}))
			require.NoError(t, d.Dirs([]string{folder}))
			if !commit {
				clear(committed)
			}
			remaining, err := RecoverDeletionJournal(root, committed)
			require.NoError(t, err)
			require.Empty(t, remaining)
			require.NoDirExists(t, root)
			if commit {
				requireDirectoryEntries(t, parent, 0)
			} else {
				requireDeletionContents(t, folder, true, "contents")
				requireDirectoryEntries(t, parent, 1)
				requireDirectoryEntries(t, folder, 1)
			}
		})
	}
}

func TestDeletionJournalUnavailableParentRetainsWork(t *testing.T) {
	d, root, committed := journalDeleter(t)
	parent := filepath.Join(t.TempDir(), "mounted")
	require.NoError(t, os.Mkdir(parent, 0755))
	source := filepath.Join(parent, "video.mp4")
	writeDeletionFixture(t, source, false, "contents")
	require.NoError(t, d.Files([]string{source}))
	require.NoError(t, os.Rename(parent, parent+"-offline"))
	require.NoError(t, os.Mkdir(parent, 0755))
	remaining, err := RecoverDeletionJournal(root, committed)
	require.Error(t, err)
	require.Len(t, remaining, 1)
	requireDirectoryEntries(t, root, 1)
	require.NoError(t, os.Remove(parent))
	require.NoError(t, os.Rename(parent+"-offline", parent))
	remaining, err = RecoverDeletionJournal(root, committed)
	require.NoError(t, err)
	require.Empty(t, remaining)
	require.NoDirExists(t, root)
}

func TestDeletionJournalTrashCopyResumesAfterPublish(t *testing.T) {
	d, root, committed := journalDeleter(t)
	source := filepath.Join(t.TempDir(), "video.mp4")
	writeDeletionFixture(t, source, false, "contents")
	d.TrashPath = t.TempDir()
	require.NoError(t, d.Files([]string{source}))
	r := d.pending[0]
	trashDir, err := os.MkdirTemp(d.TrashPath, "stash-trash-")
	require.NoError(t, err)
	r.Destination = filepath.Join(trashDir, "video.mp4")
	r.TrashDirID, err = fsutil.FileIdentity(trashDir)
	require.NoError(t, err)
	r.TrashRootID, err = fsutil.FileIdentity(d.TrashPath)
	require.NoError(t, err)
	// Exercise the copy protocol regardless of whether the test machine has
	// two writable filesystems. Stop after publishing, before source removal.
	require.NoError(t, copyToTrash(r, r.Staged, func() error { return writeDeletionRecord(root, r) }))
	requireDeletionContents(t, r.Staged, false, "contents")
	requireDeletionContents(t, r.Destination, false, "contents")
	remaining, err := RecoverDeletionJournal(root, committed)
	require.NoError(t, err)
	require.Empty(t, remaining)
	require.NoFileExists(t, r.Staged)
	requireDeletionContents(t, r.Destination, false, "contents")
	requireDirectoryEntries(t, trashDir, 1)
	require.NoDirExists(t, root)
}

func TestDeletionJournalTrashCopyResumesBeforePublish(t *testing.T) {
	d, root, committed := journalDeleter(t)
	source := filepath.Join(t.TempDir(), "video.mp4")
	writeDeletionFixture(t, source, false, "contents")
	d.TrashPath = t.TempDir()
	require.NoError(t, d.Files([]string{source}))
	r := d.pending[0]
	trashDir, err := os.MkdirTemp(d.TrashPath, "stash-trash-")
	require.NoError(t, err)
	r.Destination = filepath.Join(trashDir, filepath.Base(source))
	r.TrashDirID, err = fsutil.FileIdentity(trashDir)
	require.NoError(t, err)
	r.TrashRootID, err = fsutil.DirectoryIdentity(d.TrashPath)
	require.NoError(t, err)
	// An empty copy reservation may survive a crash before its ID was saved.
	require.NoError(t, os.Mkdir(trashCopyDir(r), 0700))
	interrupted := errors.New("interrupted before publishing copy")
	err = copyToTrash(r, r.Staged, func() error {
		if err := writeDeletionRecord(root, r); err != nil {
			return err
		}
		if r.CopyID != "" {
			return interrupted
		}
		return nil
	})
	require.ErrorIs(t, err, interrupted)
	requireDeletionContents(t, r.Staged, false, "contents")
	require.NoFileExists(t, r.Destination)
	remaining, err := RecoverDeletionJournal(root, committed)
	require.NoError(t, err)
	require.Empty(t, remaining)
	requireDeletionContents(t, r.Destination, false, "contents")
	require.NoDirExists(t, r.StageDir)
	requireDirectoryEntries(t, trashDir, 1)
	require.NoDirExists(t, root)
}

func TestDeletionJournalCrossFilesystemTrash(t *testing.T) {
	volume := os.Getenv("STASH_TEST_TRASH_ROOT")
	if volume == "" {
		t.Skip("set STASH_TEST_TRASH_ROOT to a writable filesystem different from the temporary directory")
	}
	trash, err := os.MkdirTemp(volume, ".stash-trash-test-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(trash) })
	parent := t.TempDir()
	probe := filepath.Join(parent, "probe")
	writeDeletionFixture(t, probe, false, "probe")
	require.True(t, fsutil.IsCrossDeviceMove(fsutil.RenameNoReplace(probe, filepath.Join(trash, "probe"))), "test volumes must be on different filesystems")
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[directory], func(t *testing.T) {
			d, root, committed := journalDeleter(t)
			d.TrashPath = trash
			basename := strings.Repeat("é", 125) + "a.mp4"
			if directory {
				basename = ".stash-copy"
			}
			source := filepath.Join(parent, basename)
			writeDeletionFixture(t, source, directory, "contents")
			if directory {
				require.NoError(t, d.Dirs([]string{source}))
			} else {
				require.NoError(t, d.Files([]string{source}))
			}
			remaining, err := RecoverDeletionJournal(root, committed)
			require.NoError(t, err)
			require.Empty(t, remaining)
			require.NoDirExists(t, root)
			require.NoFileExists(t, source)
			require.NoDirExists(t, source)
			matches, err := filepath.Glob(filepath.Join(trash, "stash-trash-*", basename))
			require.NoError(t, err)
			require.Len(t, matches, 1)
			requireDeletionContents(t, matches[0], directory, "contents")
			requireDirectoryEntries(t, filepath.Dir(matches[0]), 1)
		})
	}
}

func TestDeletionJournalPrepareFailureLeavesSource(t *testing.T) {
	d, root, _ := journalDeleter(t)
	parent := t.TempDir()
	source := filepath.Join(parent, "video.mp4")
	writeDeletionFixture(t, source, false, "contents")
	failure := errors.New("database marker failed")
	d.journal.MarkCommitted = func(string) error { return failure }
	require.ErrorIs(t, d.Files([]string{source}), failure)
	requireDeletionContents(t, source, false, "contents")
	requireDirectoryEntries(t, parent, 1)
	remaining, err := RecoverDeletionJournal(root, nil)
	require.NoError(t, err)
	require.Empty(t, remaining)
	require.NoDirExists(t, root)
}

func TestDeletionJournalPreservesCorruptIntent(t *testing.T) {
	d, root, committed := journalDeleter(t)
	source := filepath.Join(t.TempDir(), "video.mp4")
	writeDeletionFixture(t, source, false, "contents")
	require.NoError(t, d.Files([]string{source}))
	r := d.pending[0]
	require.NoError(t, os.WriteFile(filepath.Join(root, r.ID+".journal"), []byte("corrupt"), 0600))
	remaining, err := RecoverDeletionJournal(root, committed)
	require.Error(t, err)
	require.True(t, remaining[r.ID])
	requireDeletionContents(t, r.Staged, false, "contents")
	requireDirectoryEntries(t, root, 1)
}

func TestDeletionJournalNestedRestoreConflict(t *testing.T) {
	d, root, _ := journalDeleter(t)
	folder := filepath.Join(t.TempDir(), "folder")
	writeDeletionFixture(t, folder, true, "original child")
	require.NoError(t, d.Files([]string{filepath.Join(folder, "child.txt")}))
	require.NoError(t, d.Dirs([]string{folder}))
	require.NoError(t, os.Mkdir(folder, 0755))
	remaining, err := RecoverDeletionJournal(root, nil)
	require.Error(t, err)
	require.Len(t, remaining, 2)
	// Do not restore a child into the unrelated replacement parent.
	requireDirectoryEntries(t, folder, 0)
	require.NoError(t, os.Remove(folder))
	remaining, err = RecoverDeletionJournal(root, nil)
	require.NoError(t, err)
	require.Empty(t, remaining)
	requireDeletionContents(t, folder, true, "original child")
	require.NoDirExists(t, root)
}

func TestDeletionJournalNestedDirectoryAliases(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[commit], func(t *testing.T) {
			d, root, committed := journalDeleter(t)
			folder := filepath.Join(t.TempDir(), "folder")
			writeDeletionFixture(t, folder, true, "contents")
			alias := filepath.Join(t.TempDir(), "alias")
			if err := os.Symlink(folder, alias); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			require.NoError(t, d.Files([]string{filepath.Join(alias, "child.txt")}))
			require.NoError(t, d.Dirs([]string{folder}))
			if !commit {
				clear(committed)
			}
			remaining, err := RecoverDeletionJournal(root, committed)
			require.NoError(t, err)
			require.Empty(t, remaining)
			require.NoDirExists(t, root)
			if commit {
				require.NoDirExists(t, folder)
			} else {
				requireDeletionContents(t, folder, true, "contents")
				requireDeletionContents(t, alias, true, "contents")
			}
		})
	}

}

func TestDeletionJournalPreservesFilenameBytes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows filenames use Unicode")
	}
	d, root, _ := journalDeleter(t)
	source := filepath.Join(t.TempDir(), "video-\xff.mp4")
	writeDeletionFixture(t, source, false, "contents")
	require.NoError(t, d.Files([]string{source}))
	remaining, err := RecoverDeletionJournal(root, nil)
	require.NoError(t, err)
	require.Empty(t, remaining)
	requireDeletionContents(t, source, false, "contents")
	require.NoDirExists(t, root)
}

func TestDeletionJournalTrashRepeatedLongBasenames(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{false: "files", true: "directories"}[directory], func(t *testing.T) {
			trash := t.TempDir()
			source := filepath.Join(t.TempDir(), strings.Repeat("é", 125)+"a.mp4")
			for _, contents := range []string{"first", "second", "third"} {
				d, root, committed := journalDeleter(t)
				d.TrashPath = trash
				writeDeletionFixture(t, source, directory, contents)
				if directory {
					require.NoError(t, d.Dirs([]string{source}))
				} else {
					require.NoError(t, d.Files([]string{source}))
				}
				remaining, err := RecoverDeletionJournal(root, committed)
				require.NoError(t, err)
				require.Empty(t, remaining)
				require.NoDirExists(t, root)
			}
			var contents []string
			for _, entry := range requireDirectoryEntries(t, trash, 3) {
				path := filepath.Join(trash, entry.Name(), filepath.Base(source))
				if directory {
					path = filepath.Join(path, "child.txt")
				}
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				contents = append(contents, string(data))
			}
			require.ElementsMatch(t, []string{"first", "second", "third"}, contents)
		})
	}
}
