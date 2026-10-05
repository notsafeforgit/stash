package sqlite_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func snapshotFileHash(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func snapshotTreeHashes(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			result[path] = snapshotFileHash(t, path)
		}
		return nil
	})
	require.NoError(t, err)
	return result
}

func TestNativeSnapshotVerificationRetainsOrphanDeletionMarkers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "native ? # snapshot.sqlite")
	initial := filepath.Join(filepath.Dir(path), "initial.sqlite")
	writeEmptyNativeFixture(t, initial)
	raw := openRawDB(t, initial)
	_, err := raw.Exec("INSERT INTO file_deletions(id) VALUES('retained-operation')")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.NoError(t, os.Rename(initial, path))
	before := snapshotFileHash(t, path)
	require.NoError(t, os.Chmod(path, 0400))
	report, err := sqlite.VerifyNativeSnapshot(t.Context(), path)
	require.NoError(t, err)
	require.True(t, report.DatabaseVerified)
	require.False(t, report.FilesystemRecoveryVerified)
	require.EqualValues(t, 1, report.PendingFileDeletions)
	require.Equal(t, sqlite.GetRequiredSchemaVersion(), report.SchemaVersion)
	require.Equal(t, sqlite.NativeSchemaLineage, report.Lineage)
	require.Equal(t, before, report.SHA256)
	require.Equal(t, before, snapshotFileHash(t, path))
	repeated, err := sqlite.VerifyNativeSnapshot(t.Context(), path)
	require.NoError(t, err)
	require.Equal(t, report, repeated)
}

func TestNativeSnapshotVerificationRejectsIncompleteAndForeignInputs(t *testing.T) {
	for _, tc := range []struct{ name, mutation string }{
		{"foreign", "UPDATE native_schema SET lineage='foreign-archive'"},
		{"dirty", "UPDATE schema_migrations SET dirty=1"},
		{"older", fmt.Sprintf("UPDATE schema_migrations SET version=%d", sqlite.GetRequiredSchemaVersion()-1)},
		{"newer", fmt.Sprintf("UPDATE schema_migrations SET version=%d", sqlite.GetRequiredSchemaVersion()+1)},
		{"missing guard", "DROP TRIGGER archive_scene_deleted"},
		{"broken reference", "PRAGMA foreign_keys=OFF; INSERT INTO galleries_images(gallery_id,image_id) VALUES(123,456)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "native.sqlite")
			writeEmptyNativeFixture(t, path)
			raw := openRawDB(t, path)
			_, err := raw.Exec(tc.mutation)
			require.NoError(t, err)
			require.NoError(t, raw.Close())
			before := snapshotFileHash(t, path)
			report, err := sqlite.VerifyNativeSnapshot(t.Context(), path)
			require.Error(t, err)
			require.Nil(t, report)
			require.Equal(t, before, snapshotFileHash(t, path))
		})
	}
	missing := filepath.Join(t.TempDir(), "missing.sqlite")
	_, err := sqlite.VerifyNativeSnapshot(t.Context(), missing)
	require.Error(t, err)
	require.NoFileExists(t, missing)
}

func TestNativeSnapshotVerificationRejectsUncheckpointedWALAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "native.sqlite")
	writeEmptyNativeFixture(t, path)
	raw := openRawDB(t, path)
	_, err := raw.Exec("PRAGMA journal_mode=WAL; INSERT INTO file_deletions(id) VALUES('wal-only')")
	require.NoError(t, err)
	before, wal := snapshotFileHash(t, path), snapshotFileHash(t, path+"-wal")
	_, err = sqlite.VerifyNativeSnapshot(t.Context(), path)
	require.ErrorContains(t, err, "standalone SQLite snapshot")
	require.Equal(t, before, snapshotFileHash(t, path))
	require.Equal(t, wal, snapshotFileHash(t, path+"-wal"))
	require.NoError(t, raw.Close())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = sqlite.VerifyNativeSnapshot(ctx, path)
	require.ErrorIs(t, err, context.Canceled)
	alias := filepath.Join(t.TempDir(), "alias.sqlite")
	require.NoError(t, os.Symlink(path, alias))
	_, err = sqlite.VerifyNativeSnapshot(t.Context(), alias)
	require.ErrorContains(t, err, "regular SQLite database")
}

func TestNativeSnapshotVerificationDoesNotRecoverInterruptedDeletion(t *testing.T) {
	for _, phase := range []string{"before_commit", "after_commit"} {
		t.Run(phase, func(t *testing.T) {
			db, source, _ := newDeletionDatabase(t)
			path, journal := db.DatabasePath(), db.FileDeletionJournalPath()
			require.NoError(t, db.Close())
			command := exec.Command(os.Args[0], "-test.run=^TestDeletionCrashHelper$")
			command.Env = append(os.Environ(), "STASH_TEST_DELETION_CRASH="+phase,
				"STASH_TEST_DELETION_DATABASE="+path, "STASH_TEST_DELETION_SOURCE="+source, "STASH_TEST_DELETION_TRASH=")
			output, err := command.CombinedOutput()
			var exited *exec.ExitError
			require.ErrorAs(t, err, &exited, "%s", output)
			require.Equal(t, deletionCrashExit, exited.ExitCode(), "%s", output)
			// Checkpoint SQLite only. Never open Stash's database/recovery layer.
			raw := openRawDB(t, path)
			_, err = raw.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
			require.NoError(t, err)
			require.NoError(t, raw.Close())
			filesBefore, journalBefore := snapshotTreeHashes(t, filepath.Dir(source)), snapshotTreeHashes(t, journal)
			require.NotEmpty(t, filesBefore)
			require.NotEmpty(t, journalBefore)
			before := snapshotFileHash(t, path)
			report, err := sqlite.VerifyNativeSnapshot(t.Context(), path)
			require.NoError(t, err)
			require.Equal(t, before, report.SHA256)
			require.Equal(t, before, snapshotFileHash(t, path))
			require.Equal(t, filesBefore, snapshotTreeHashes(t, filepath.Dir(source)))
			require.Equal(t, journalBefore, snapshotTreeHashes(t, journal))
			require.NoFileExists(t, source)
			expected := int64(0)
			if phase == "after_commit" {
				expected = 1
			}
			require.Equal(t, expected, report.PendingFileDeletions)
			require.False(t, report.FilesystemRecoveryVerified)
		})
	}
}

func TestNativeSnapshotVerificationChecksReleasedEnrichmentProof(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.complete)
	require.NoError(t, err)
	_, err = f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
	require.NoError(t, err)
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	_, err = sqlite.VerifyNativeSnapshot(t.Context(), path)
	require.NoError(t, err)
	raw := openRawDB(t, path)
	var trigger string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='enrichment_checkpoint_release_immutable'").Scan(&trigger))
	_, err = raw.Exec("DROP TRIGGER enrichment_checkpoint_release_immutable; UPDATE enrichment_checkpoint_releases SET proof_sha256=printf('%064d',0)")
	require.NoError(t, err)
	_, err = raw.Exec(trigger)
	require.NoError(t, err)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, raw.Close())
	before := snapshotFileHash(t, path)
	report, err := sqlite.VerifyNativeSnapshot(t.Context(), path)
	require.Error(t, err)
	require.Nil(t, report)
	require.Equal(t, before, snapshotFileHash(t, path))
}
