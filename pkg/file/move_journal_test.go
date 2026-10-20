package file

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stretchr/testify/require"
)

func moveRecoveryFixture(t *testing.T) (string, *moveRecord) {
	t.Helper()
	root := t.TempDir()
	original := filepath.Join(root, "original.mp4")
	require.NoError(t, os.WriteFile(original, []byte("original"), 0644))
	sourceID, err := fsutil.FileIdentity(original)
	require.NoError(t, err)
	parentID, err := fsutil.DirectoryIdentity(root)
	require.NoError(t, err)
	stage, err := newMoveDirectory(root)
	require.NoError(t, err)
	r := &moveRecord{Version: 1, ID: uuid.NewString(), FileID: 1, Original: original,
		Destination: filepath.Join(root, "destination.mp4"), SourceID: sourceID,
		SourceParent:      moveDirectory{Path: root, ID: parentID},
		DestinationParent: moveDirectory{Path: root, ID: parentID}, Stage: stage}
	return filepath.Join(t.TempDir(), "moves"), r
}

func TestMoveJournalCopyBoundaries(t *testing.T) {
	for _, phase := range []string{"prepared", "staged", "partial_copy", "completed_copy", "published_copy", "renamed"} {
		for _, committed := range []bool{false, true} {
			if committed && phase != "published_copy" && phase != "renamed" {
				continue // A transaction must not commit before durable publication.
			}
			name := phase + "/rollback"
			if committed {
				name = phase + "/commit"
			}
			t.Run(name, func(t *testing.T) {
				journal, r := moveRecoveryFixture(t)
				require.NoError(t, writeMoveRecord(journal, r))
				if phase != "prepared" {
					require.NoError(t, fsutil.RenameNoReplace(r.Original, r.staged()))
				}
				if strings.Contains(phase, "copy") {
					var err error
					r.Copy, err = newMoveDirectory(r.DestinationParent.Path)
					require.NoError(t, err)
					require.NoError(t, writeMoveRecord(journal, r))
					if phase == "partial_copy" {
						require.NoError(t, os.WriteFile(r.copied(), []byte("orig"), 0600))
					} else {
						require.NoError(t, fsutil.CopyPathDurable(r.staged(), r.copied()))
						r.CopyID, err = fsutil.FileIdentity(r.copied())
						require.NoError(t, err)
						require.NoError(t, writeMoveRecord(journal, r))
						if phase == "published_copy" {
							require.NoError(t, fsutil.RenameNoReplace(r.copied(), r.Destination))
						}
					}
				}
				if phase == "renamed" {
					require.NoError(t, fsutil.RenameNoReplace(r.staged(), r.Destination))
				}
				for range 2 {
					remaining, err := RecoverMoveJournal(journal, map[string]bool{r.ID: committed})
					require.NoError(t, err)
					require.Empty(t, remaining)
				}
				want, absent := r.Original, r.Destination
				if committed {
					want, absent = absent, want
				}
				data, err := os.ReadFile(want)
				require.NoError(t, err)
				require.Equal(t, "original", string(data))
				require.NoFileExists(t, absent)
				require.NoDirExists(t, r.Stage.Path)
				if r.Copy.Path != "" {
					require.NoDirExists(t, r.Copy.Path)
				}
				require.NoDirExists(t, journal)
			})
		}
	}
}

func TestMoveJournalRejectsOversizedAndExcessRecords(t *testing.T) {
	journal, r := moveRecoveryFixture(t)
	j := &MoveJournal{Directory: journal, MarkCommitted: func(string) error { return nil }}
	original := r.SourceID
	r.SourceID = strings.Repeat("x", maxMoveRecordSize+1)
	require.ErrorContains(t, j.prepare(r), "64 KiB")
	require.NoDirExists(t, journal)
	require.FileExists(t, r.Original)
	r.SourceID = original
	for range MaxMoveBatch {
		r.ID = uuid.NewString()
		require.NoError(t, j.prepare(r))
	}
	r.ID = uuid.NewString()
	require.ErrorContains(t, j.prepare(r), "exceeds 1000")
	entries, err := os.ReadDir(journal)
	require.NoError(t, err)
	require.Len(t, entries, MaxMoveBatch)
	// A new transaction cannot grow an unresolved journal with another batch.
	next := &MoveJournal{Directory: journal, MarkCommitted: func(string) error { return nil }}
	require.ErrorContains(t, next.prepare(r), "recovery remains pending")
	require.ErrorContains(t, next.prepare(r), "recovery remains pending")
}

func TestMoveJournalDamageRetainsCommitEvidence(t *testing.T) {
	journal, r := moveRecoveryFixture(t)
	require.NoError(t, writeMoveRecord(journal, r))
	path := filepath.Join(journal, r.ID+".journal")
	require.NoError(t, os.WriteFile(path, []byte("truncated"), 0600))
	markers := map[string]bool{r.ID: true, uuid.NewString(): true}
	remaining, err := RecoverMoveJournal(journal, markers)
	require.Error(t, err)
	require.Equal(t, markers, remaining)
	require.FileExists(t, r.Original)
	require.FileExists(t, path)
}

func TestMoveJournalRestorationNeverOverwritesAReplacement(t *testing.T) {
	journal, r := moveRecoveryFixture(t)
	require.NoError(t, writeMoveRecord(journal, r))
	require.NoError(t, fsutil.RenameNoReplace(r.Original, r.Destination))
	require.NoError(t, os.WriteFile(r.Original, []byte("replacement"), 0600))
	remaining, err := RecoverMoveJournal(journal, nil)
	require.Error(t, err)
	require.True(t, remaining[r.ID])
	data, err := os.ReadFile(r.Original)
	require.NoError(t, err)
	require.Equal(t, "replacement", string(data))
	require.NoError(t, os.Remove(r.Original))
	remaining, err = RecoverMoveJournal(journal, nil)
	require.NoError(t, err)
	require.Empty(t, remaining)
	data, err = os.ReadFile(r.Original)
	require.NoError(t, err)
	require.Equal(t, "original", string(data))
}

func TestMovePathsConfineAliasesAndMissingDirectories(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "escape")))
	_, err := managedMovePath(filepath.Join(root, "escape", "missing"), []string{root})
	require.ErrorContains(t, err, "escapes")
	path, err := managedMovePath(filepath.Join(root, "new", "nested"), []string{root})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "new", "nested"), path)
	_, err = managedMovePath(outside, []string{root})
	require.ErrorContains(t, err, "outside")
	_, err = managedMovePath(filepath.Join(outside, "offline"), []string{filepath.Join(outside, "offline")})
	require.ErrorIs(t, err, os.ErrNotExist)
}
