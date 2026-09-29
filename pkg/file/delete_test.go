package file

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeDeletionFixture(t *testing.T, path string, directory bool, contents string) {
	t.Helper()
	if directory {
		require.NoError(t, os.Mkdir(path, 0755))
		path = filepath.Join(path, "child.txt")
	}
	require.NoError(t, os.WriteFile(path, []byte(contents), 0644))
}

func requireDeletionContents(t *testing.T, path string, directory bool, contents string) {
	t.Helper()
	if directory {
		path = filepath.Join(path, "child.txt")
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, contents, string(data))
}

func requireDirectoryEntries(t *testing.T, path string, count int) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(path)
	require.NoError(t, err)
	require.Len(t, entries, count)
	return entries
}

func TestDeleterLongNames(t *testing.T) {
	for _, name := range []struct {
		label    string
		basename string
	}{
		{"ascii", strings.Repeat("a", 251) + ".mp4"},
		{"unicode", strings.Repeat("é", 125) + "a.mp4"},
	} {
		for _, kind := range []string{"file", "directory"} {
			for _, action := range []string{"commit", "rollback"} {
				t.Run(name.label+"/"+kind+"/"+action, func(t *testing.T) {
					parent := t.TempDir()
					source := filepath.Join(parent, name.basename)
					directory := kind == "directory"
					writeDeletionFixture(t, source, directory, "original contents")

					d := NewDeleter()
					mark := d.Files
					if directory {
						mark = d.Dirs
					}
					require.NoError(t, mark([]string{source}))
					require.NoFileExists(t, source)
					require.NoDirExists(t, source)

					entries := requireDirectoryEntries(t, parent, 1)
					require.True(t, entries[0].IsDir())
					staged := filepath.Join(parent, entries[0].Name(), name.basename)
					requireDeletionContents(t, staged, directory, "original contents")

					if action == "rollback" {
						d.Rollback()
						requireDeletionContents(t, source, directory, "original contents")
						require.Equal(t, name.basename, requireDirectoryEntries(t, parent, 1)[0].Name())
					} else {
						d.Commit()
						requireDirectoryEntries(t, parent, 0)
					}
				})
			}
		}
	}
}

func TestDeleterTrashAndBypass(t *testing.T) {
	for _, action := range []string{"commit", "rollback"} {
		t.Run(action, func(t *testing.T) {
			parent := t.TempDir()
			trash := t.TempDir()
			d := NewDeleterWithTrash(trash)
			basename := strings.Repeat("a", 251) + ".mp4"
			original := filepath.Join(parent, basename)
			oldTrash := filepath.Join(trash, basename)
			writeDeletionFixture(t, original, false, "media")
			writeDeletionFixture(t, oldTrash, false, "already in trash")
			require.NoError(t, d.Files([]string{original}))

			album := filepath.Join(parent, "album")
			writeDeletionFixture(t, album, true, "album contents")
			require.NoError(t, d.Dirs([]string{album}))

			generatedFile := filepath.Join(parent, "preview.jpg")
			generatedDir := filepath.Join(parent, "previews")
			writeDeletionFixture(t, generatedFile, false, "preview")
			writeDeletionFixture(t, generatedDir, true, "previews")
			require.NoError(t, d.FilesWithoutTrash([]string{generatedFile}))
			require.NoError(t, d.DirsWithoutTrash([]string{generatedDir}))

			if action == "commit" {
				d.Commit()
				requireDirectoryEntries(t, parent, 0)
				entries := requireDirectoryEntries(t, trash, 3)
				for _, entry := range entries {
					if entry.IsDir() {
						dir := filepath.Join(trash, entry.Name())
						name := requireDirectoryEntries(t, dir, 1)[0].Name()
						switch name {
						case basename:
							requireDeletionContents(t, filepath.Join(dir, name), false, "media")
						case "album":
							requireDeletionContents(t, filepath.Join(dir, name), true, "album contents")
						default:
							t.Fatalf("unexpected trashed path %q", name)
						}
					}
				}
				// Completed paths must not be restored by a later hook.
				d.Rollback()
				requireDirectoryEntries(t, parent, 0)
			} else {
				d.Rollback()
				requireDirectoryEntries(t, parent, 4)
				requireDeletionContents(t, original, false, "media")
				requireDeletionContents(t, album, true, "album contents")
				requireDeletionContents(t, generatedFile, false, "preview")
				requireDeletionContents(t, generatedDir, true, "previews")
				requireDirectoryEntries(t, trash, 1)
				d.Commit()
				requireDirectoryEntries(t, parent, 4)
			}
			requireDeletionContents(t, oldTrash, false, "already in trash")
		})
	}
}

func TestDeleterStagingFailure(t *testing.T) {
	for _, operation := range []string{"mkdir", "rename"} {
		t.Run(operation, func(t *testing.T) {
			parent := t.TempDir()
			first := filepath.Join(parent, "first.mp4")
			second := filepath.Join(parent, "second.mp4")
			writeDeletionFixture(t, first, false, "first")
			writeDeletionFixture(t, second, false, "second")
			failure := errors.New("injected staging failure")
			rr := newRenamerRemoverImpl()
			if operation == "mkdir" {
				calls := 0
				rr.MkdirTempFn = func(dir, pattern string) (string, error) {
					calls++
					if calls == 2 {
						return "", failure
					}
					return os.MkdirTemp(dir, pattern)
				}
			} else {
				rr.RenameFn = func(oldpath, newpath string) error {
					if oldpath == second {
						return failure
					}
					return os.Rename(oldpath, newpath)
				}
			}
			d := &Deleter{RenamerRemover: rr}
			require.ErrorIs(t, d.Files([]string{first, second}), failure)
			requireDirectoryEntries(t, parent, 2)
			requireDeletionContents(t, second, false, "second")
			d.Rollback()
			requireDirectoryEntries(t, parent, 2)
			requireDeletionContents(t, first, false, "first")
			requireDeletionContents(t, second, false, "second")
		})
	}
}

func TestDeleterFailedCompletionRetainsContents(t *testing.T) {
	for _, action := range []string{"commit", "rollback"} {
		t.Run(action, func(t *testing.T) {
			parent := t.TempDir()
			source := filepath.Join(parent, "file.mp4")
			writeDeletionFixture(t, source, false, "recoverable")
			d := NewDeleter()
			require.NoError(t, d.Files([]string{source}))
			stagingDir := filepath.Join(parent, requireDirectoryEntries(t, parent, 1)[0].Name())

			rr := newRenamerRemoverImpl()
			if action == "commit" {
				rr.RemoveFn = func(string) error { return os.ErrPermission }
				d.RenamerRemover = rr
				d.Commit()
			} else {
				rr.RenameFn = func(string, string) error { return os.ErrPermission }
				d.RenamerRemover = rr
				d.Rollback()
			}
			requireDeletionContents(t, filepath.Join(stagingDir, "file.mp4"), false, "recoverable")
		})
	}
}

func TestDeleterRollbackNestedPaths(t *testing.T) {
	parent := t.TempDir()
	folder := filepath.Join(parent, "folder")
	writeDeletionFixture(t, folder, true, "contents")
	d := NewDeleter()
	require.NoError(t, d.Files([]string{filepath.Join(folder, "child.txt")}))
	require.NoError(t, d.Dirs([]string{folder}))
	d.Rollback()
	requireDeletionContents(t, folder, true, "contents")
	requireDirectoryEntries(t, parent, 1)
	requireDirectoryEntries(t, folder, 1)
}

func TestDeleterMissingPathsAndExistingDeleteSuffix(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "file.mp4")
	legacy := source + ".delete"
	writeDeletionFixture(t, source, false, "original")
	writeDeletionFixture(t, legacy, false, "unrelated")
	d := NewDeleter()
	require.NoError(t, d.Files([]string{source, source, filepath.Join(parent, "missing.mp4")}))
	require.NoError(t, d.Dirs([]string{filepath.Join(parent, "missing")}))
	d.Commit()
	require.Equal(t, filepath.Base(legacy), requireDirectoryEntries(t, parent, 1)[0].Name())
	requireDeletionContents(t, legacy, false, "unrelated")
}

func TestScannerRejectsStagedDeletions(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "file.mp4")
	writeDeletionFixture(t, source, false, "contents")
	d := NewDeleter()
	require.NoError(t, d.Files([]string{source}))
	stagingDir := filepath.Join(parent, requireDirectoryEntries(t, parent, 1)[0].Name())
	scanner := &Scanner{}
	for _, path := range []string{stagingDir, filepath.Join(stagingDir, "file.mp4")} {
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.False(t, scanner.AcceptEntry(context.Background(), path, info, ""))
		// Names inside an archive cannot be deletion staging directories.
		require.True(t, scanner.AcceptEntry(context.Background(), path, info, "archive.zip"))
	}
	d.Rollback()
	info, err := os.Stat(source)
	require.NoError(t, err)
	require.True(t, scanner.AcceptEntry(context.Background(), source, info, ""))
}
