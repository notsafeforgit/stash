package file

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stretchr/testify/require"
)

func deletionSnapshotIDs(committed map[string]bool) []string {
	var ids []string
	for id, yes := range committed {
		if yes {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

func roundTripDeletionSnapshot(t *testing.T, journal string, roots []DeletionSnapshotRoot, committed map[string]bool) (*RestoredDeletionSnapshot, string) {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "deletions.zip")
	ids := deletionSnapshotIDs(committed)
	require.NoError(t, CaptureDeletionSnapshot(t.Context(), journal, archive, roots, ids, nil))
	restored, err := RestoreDeletionSnapshot(t.Context(), archive, filepath.Join(t.TempDir(), "restored"), ids, nil)
	require.NoError(t, err)
	return restored, archive
}

func TestDeletionSnapshotNestedRecovery(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[committed], func(t *testing.T) {
			d, journal, markers := journalDeleter(t)
			media := t.TempDir()
			folder := filepath.Join(media, "album")
			writeDeletionFixture(t, folder, true, "child")
			writeDeletionFixture(t, filepath.Join(media, "unrelated.mp4"), false, "outside capture")
			require.NoError(t, d.Files([]string{filepath.Join(folder, "child.txt")}))
			require.NoError(t, d.Dirs([]string{folder}))
			if !committed {
				clear(markers)
			}
			restored, _ := roundTripDeletionSnapshot(t, journal, []DeletionSnapshotRoot{{Name: "media", Path: media}}, markers)
			require.Empty(t, restored.UnresolvedIdentities)
			root := restored.Roots[0].Path
			require.NoFileExists(t, filepath.Join(root, "unrelated.mp4"))
			remaining, err := RecoverDeletionJournal(restored.JournalPath, markers)
			require.NoError(t, err)
			require.Empty(t, remaining)
			require.NoDirExists(t, restored.JournalPath)
			if committed {
				requireDirectoryEntries(t, root, 0)
			} else {
				requireDeletionContents(t, filepath.Join(root, "album"), true, "child")
			}
			// Recovered copies must not touch the original pending work.
			requireDirectoryEntries(t, journal, 2)
			require.NoDirExists(t, folder)
			require.DirExists(t, d.pending[1].StageDir)
		})
	}
}

func TestDeletionSnapshotConflictPreservesBothFiles(t *testing.T) {
	d, journal, _ := journalDeleter(t)
	media := t.TempDir()
	source := filepath.Join(media, "video.mp4")
	writeDeletionFixture(t, source, false, "original")
	require.NoError(t, d.Files([]string{source}))
	writeDeletionFixture(t, source, false, "replacement")
	restored, _ := roundTripDeletionSnapshot(t, journal, []DeletionSnapshotRoot{{Name: "media", Path: media}}, nil)
	root := restored.Roots[0].Path
	copySource := filepath.Join(root, "video.mp4")
	copyStaged := filepath.Join(root, filepath.Base(d.pending[0].StageDir), "video.mp4")
	remaining, err := RecoverDeletionJournal(restored.JournalPath, nil)
	require.Error(t, err)
	require.Len(t, remaining, 1)
	requireDeletionContents(t, copySource, false, "replacement")
	requireDeletionContents(t, copyStaged, false, "original")
	require.NoError(t, os.Remove(copySource))
	remaining, err = RecoverDeletionJournal(restored.JournalPath, nil)
	require.NoError(t, err)
	require.Empty(t, remaining)
	requireDeletionContents(t, copySource, false, "original")
	requireDeletionContents(t, source, false, "replacement")
	requireDeletionContents(t, d.pending[0].Staged, false, "original")
}

func TestDeletionSnapshotByteNamesHardlinksAndSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("arbitrary byte names and unprivileged symlinks require a Unix filesystem")
	}
	d, journal, _ := journalDeleter(t)
	media := t.TempDir()
	name := strings.Repeat("x", 250) + "\xff.mp4"
	folder := filepath.Join(media, "album")
	require.NoError(t, os.Mkdir(folder, 0750))
	require.NoError(t, os.WriteFile(filepath.Join(folder, name), []byte("shared bytes"), 0640))
	require.NoError(t, os.Link(filepath.Join(folder, name), filepath.Join(folder, "hardlink")))
	external := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.WriteFile(external, []byte("must stay outside"), 0600))
	require.NoError(t, os.Symlink(external, filepath.Join(folder, "symlink")))
	require.NoError(t, d.Dirs([]string{folder}))
	restored, _ := roundTripDeletionSnapshot(t, journal, []DeletionSnapshotRoot{{Name: "media", Path: media}}, nil)
	_, err := RecoverDeletionJournal(restored.JournalPath, nil)
	require.NoError(t, err)
	copyFolder := filepath.Join(restored.Roots[0].Path, "album")
	requireDeletionContents(t, filepath.Join(copyFolder, name), false, "shared bytes")
	id, err := fsutil.FileIdentity(filepath.Join(copyFolder, name))
	require.NoError(t, err)
	hardlinkID, err := fsutil.FileIdentity(filepath.Join(copyFolder, "hardlink"))
	require.NoError(t, err)
	require.Equal(t, id, hardlinkID)
	target, err := os.Readlink(filepath.Join(copyFolder, "symlink"))
	require.NoError(t, err)
	require.Equal(t, external, target)
	requireDeletionContents(t, external, false, "must stay outside")
}

func TestDeletionSnapshotTrashCopies(t *testing.T) {
	for _, phase := range []string{"not-reserved", "incomplete", "incomplete-with-extra", "before-publish", "after-publish", "after-rename"} {
		t.Run(phase, func(t *testing.T) {
			d, journal, markers := journalDeleter(t)
			media, trash := t.TempDir(), t.TempDir()
			source := filepath.Join(media, "video.mp4")
			writeDeletionFixture(t, source, false, "original")
			d.TrashPath = trash
			if runtime.GOOS != "windows" {
				alias := filepath.Join(t.TempDir(), "trash-alias")
				require.NoError(t, os.Symlink(trash, alias))
				d.TrashPath = alias
			}
			require.NoError(t, d.Files([]string{source}))
			r := d.pending[0]
			if phase != "not-reserved" {
				dir, err := os.MkdirTemp(d.TrashPath, "stash-trash-")
				require.NoError(t, err)
				r.Destination = filepath.Join(dir, "video.mp4")
				r.TrashRootID, err = fsutil.DirectoryIdentity(d.TrashPath)
				require.NoError(t, err)
				r.TrashDirID, err = fsutil.FileIdentity(dir)
				require.NoError(t, err)
				interrupted := errors.New("interrupted copy")
				if phase == "after-rename" {
					require.NoError(t, writeDeletionRecord(journal, r))
					require.NoError(t, fsutil.RenameNoReplace(r.Staged, r.Destination))
				} else {
					err = copyToTrash(r, r.Staged, func() error {
						if err := writeDeletionRecord(journal, r); err != nil {
							return err
						}
						if phase == "before-publish" && r.CopyID != "" {
							return interrupted
						}
						if strings.HasPrefix(phase, "incomplete") && r.CopyDirID != "" {
							writeDeletionFixture(t, filepath.Join(trashCopyDir(r), "video.mp4"), false, "partial")
							if phase == "incomplete-with-extra" {
								writeDeletionFixture(t, filepath.Join(trashCopyDir(r), "unrelated"), false, "keep")
							}
							return interrupted
						}
						return nil
					})
				}
				if phase == "after-publish" || phase == "after-rename" {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, interrupted)
				}
			}
			restored, _ := roundTripDeletionSnapshot(t, journal, []DeletionSnapshotRoot{{Name: "media", Path: media}, {Name: "trash", Path: trash}}, markers)
			require.Empty(t, restored.UnresolvedIdentities)
			remaining, err := RecoverDeletionJournal(restored.JournalPath, markers)
			if phase == "incomplete-with-extra" {
				require.Error(t, err)
				require.Len(t, remaining, 1)
				other := filepath.Join(restored.Roots[1].Path, filepath.Base(filepath.Dir(r.Destination)), ".stash-copy", "unrelated")
				requireDeletionContents(t, other, false, "keep")
				require.NoError(t, os.Remove(other))
				remaining, err = RecoverDeletionJournal(restored.JournalPath, markers)
			}
			require.NoError(t, err)
			require.Empty(t, remaining)
			requireDirectoryEntries(t, restored.Roots[0].Path, 0)
			matches, err := filepath.Glob(filepath.Join(restored.Roots[1].Path, "stash-trash-*", "video.mp4"))
			require.NoError(t, err)
			require.Len(t, matches, 1)
			requireDeletionContents(t, matches[0], false, "original")
			if phase == "after-rename" {
				requireDeletionContents(t, r.Destination, false, "original")
			} else {
				requireDeletionContents(t, r.Staged, false, "original")
			}
			requireDirectoryEntries(t, journal, 1)
		})
	}
}

func TestDeletionSnapshotMissingIdentityCannotDeleteReplacement(t *testing.T) {
	d, journal, markers := journalDeleter(t)
	media := t.TempDir()
	source := filepath.Join(media, "video.mp4")
	writeDeletionFixture(t, source, false, "original")
	require.NoError(t, d.Files([]string{source}))
	r := d.pending[0]
	require.NoError(t, os.Rename(r.Staged, filepath.Join(media, "moved-elsewhere")))
	writeDeletionFixture(t, r.Staged, false, "replacement")
	restored, _ := roundTripDeletionSnapshot(t, journal, []DeletionSnapshotRoot{{Name: "media", Path: media}}, markers)
	require.Contains(t, restored.UnresolvedIdentities, r.SourceID)
	remaining, err := RecoverDeletionJournal(restored.JournalPath, markers)
	require.ErrorContains(t, err, "replaced")
	require.Len(t, remaining, 1)
	copyStaged := filepath.Join(restored.Roots[0].Path, filepath.Base(r.StageDir), "video.mp4")
	requireDeletionContents(t, copyStaged, false, "replacement")
	requireDeletionContents(t, filepath.Join(media, "moved-elsewhere"), false, "original")
}

func TestDeletionSnapshotRefusesIncompleteAndChangedSources(t *testing.T) {
	d, journal, markers := journalDeleter(t)
	media := t.TempDir()
	source := filepath.Join(media, "video.mp4")
	writeDeletionFixture(t, source, false, strings.Repeat("data", 100000))
	require.NoError(t, d.Files([]string{source}))
	roots := []DeletionSnapshotRoot{{Name: "media", Path: media}}
	ids := deletionSnapshotIDs(markers)
	for _, scenario := range []string{"undeclared", "corrupt", "unknown", "reserve", "cancel", "modified"} {
		t.Run(scenario, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "failed.zip")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			selected := roots
			var check func(int64) error
			switch scenario {
			case "undeclared":
				selected = nil
			case "corrupt", "unknown":
				name := "unexpected"
				if scenario == "corrupt" {
					name = "a19c5a09-cd2e-4402-b453-0f0e359b3894.journal"
				}
				path := filepath.Join(journal, name)
				require.NoError(t, os.WriteFile(path, []byte("unreadable"), 0600))
				defer os.Remove(path)
			case "reserve":
				check = func(n int64) error {
					if n > 0 {
						return errors.New("disk reserve reached")
					}
					return nil
				}
			case "cancel":
				check = func(n int64) error {
					if n > 0 {
						cancel()
					}
					return nil
				}
			case "modified":
				changed := false
				check = func(n int64) error {
					if n > 0 && !changed {
						changed = true
						require.NoError(t, os.WriteFile(d.pending[0].Staged, []byte("changed"), 0600))
					}
					return nil
				}
			}
			require.Error(t, CaptureDeletionSnapshot(ctx, journal, output, selected, ids, check))
			require.NoFileExists(t, output)
		})
	}
}

func rewriteDeletionSnapshot(t *testing.T, source string, mutate func(string, []byte) []byte) string {
	t.Helper()
	archive, err := zip.OpenReader(source)
	require.NoError(t, err)
	defer archive.Close()
	path := filepath.Join(t.TempDir(), "changed.zip")
	output, err := os.Create(path)
	require.NoError(t, err)
	w := zip.NewWriter(output)
	for _, entry := range archive.File {
		r, err := entry.Open()
		require.NoError(t, err)
		data, err := io.ReadAll(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
		body, err := w.CreateHeader(&zip.FileHeader{Name: entry.Name, Method: zip.Store})
		require.NoError(t, err)
		_, err = body.Write(mutate(entry.Name, data))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	require.NoError(t, output.Close())
	return path
}

func TestDeletionSnapshotRestoreValidation(t *testing.T) {
	d, journal, markers := journalDeleter(t)
	media := t.TempDir()
	source := filepath.Join(media, "video.mp4")
	writeDeletionFixture(t, source, false, "original")
	require.NoError(t, d.Files([]string{source}))
	roots := []DeletionSnapshotRoot{{Name: "media", Path: media}}
	_, archive := roundTripDeletionSnapshot(t, journal, roots, markers)
	ids := deletionSnapshotIDs(markers)
	for _, scenario := range []string{"marker", "escape", "digest", "symlink-parent", "unknown-field", "reserve", "cancel", "old-root", "existing"} {
		t.Run(scenario, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "restored")
			selectedIDs, selectedArchive := ids, archive
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var check func(int64) error
			switch scenario {
			case "marker":
				selectedIDs = nil
			case "escape", "digest", "symlink-parent", "unknown-field":
				selectedArchive = rewriteDeletionSnapshot(t, archive, func(name string, data []byte) []byte {
					if scenario == "digest" && strings.HasPrefix(name, "objects/") {
						return []byte("tampered")
					}
					if name != "manifest.json" {
						return data
					}
					if scenario == "unknown-field" {
						return append([]byte(`{"unknown":true,`), data[1:]...)
					}
					var manifest deletionSnapshotManifest
					require.NoError(t, json.Unmarshal(data, &manifest))
					if scenario == "escape" {
						manifest.Nodes[len(manifest.Nodes)-1].Path = []byte("../escape")
					}
					if scenario == "symlink-parent" {
						manifest.Nodes[1].Kind = "symlink"
						manifest.Nodes[1].Target = []byte(media)
					}
					data, err := json.Marshal(manifest)
					require.NoError(t, err)
					return data
				})
			case "reserve":
				check = func(n int64) error {
					if n > 0 {
						return errors.New("reserve reached")
					}
					return nil
				}
			case "cancel":
				check = func(n int64) error {
					if n > 0 {
						cancel()
					}
					return nil
				}
			case "old-root":
				output = filepath.Join(media, "forbidden")
			case "existing":
				require.NoError(t, os.Mkdir(output, 0700))
				writeDeletionFixture(t, filepath.Join(output, "keep"), false, "existing")
			}
			_, err := RestoreDeletionSnapshot(ctx, selectedArchive, output, selectedIDs, check)
			require.Error(t, err)
			if scenario == "existing" {
				requireDeletionContents(t, filepath.Join(output, "keep"), false, "existing")
			} else {
				require.NoDirExists(t, output)
			}
			requireDeletionContents(t, d.pending[0].Staged, false, "original")
		})
	}
}

func TestDeletionSnapshotEmptyAndPreparedJournals(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "missing-journal")
	media := t.TempDir()
	marker := "62500fe5-7b94-4649-bcb6-8151a2c00c46"
	for _, prepare := range []bool{false, true} {
		if prepare {
			require.NoError(t, os.Mkdir(journal, 0700))
			require.NoError(t, os.WriteFile(filepath.Join(journal, ".prepare-interrupted"), []byte("incomplete gob"), 0600))
		}
		markers := map[string]bool{marker: true}
		restored, _ := roundTripDeletionSnapshot(t, journal, []DeletionSnapshotRoot{{Name: "media", Path: media}}, markers)
		remaining, err := RecoverDeletionJournal(restored.JournalPath, markers)
		require.NoError(t, err)
		require.Empty(t, remaining)
	}
}
