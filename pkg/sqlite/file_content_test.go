package sqlite_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

type fileContentFixture struct {
	db       *sqlite.Database
	repo     models.Repository
	root     *models.MediaRoot
	file     models.File
	identity *models.ArchiveEntity
}

func newFileContentFixture(t *testing.T) fileContentFixture {
	t.Helper()
	db, repo := archiveTestDatabase(t)
	dir := t.TempDir()
	binding, err := archive.ProbeMediaRoot(dir)
	require.NoError(t, err)
	root := putMediaRoot(t, repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Content test", State: "active", Binding: binding}})
	f := fileContentFixture{db: db, repo: repo, root: root}
	f.file = f.addFile(t, "first.mp4", []byte("original media"))
	f.identity = archiveFind(t, repo, models.ArchiveFile, int(f.file.Base().ID))
	return f
}
func (f fileContentFixture) addFile(t *testing.T, name string, body []byte) models.File {
	t.Helper()
	path := filepath.Join(f.root.Binding.Path, name)
	require.NoError(t, os.WriteFile(path, body, 0600))
	info, err := os.Stat(path)
	require.NoError(t, err)
	ret := &models.BaseFile{Path: path, Basename: name, Size: info.Size(), DirEntry: models.DirEntry{ModTime: info.ModTime()}, CreatedAt: time.Now(), UpdatedAt: time.Now(), Fingerprints: models.Fingerprints{{Type: models.FingerprintTypeMD5, Fingerprint: "fixture-md5"}}}
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		folder, err := file.GetOrCreateFolderHierarchy(ctx, f.repo.Folder, f.root.Binding.Path, []string{f.root.Binding.Path})
		if err != nil {
			return err
		}
		ret.ParentFolderID = folder.ID
		return f.repo.File.Create(ctx, ret)
	}))
	return ret
}
func (f fileContentFixture) input(t *testing.T, media models.File) models.FileContentInput {
	t.Helper()
	verified, err := archive.VerifyMediaFile(t.Context(), *f.root, media.Base().Basename, nil, "")
	require.NoError(t, err)
	defer verified.Close()
	identity := archiveFind(t, f.repo, models.ArchiveFile, int(media.Base().ID))
	return models.FileContentInput{FileUUID: identity.UUID, ExpectedGeneration: media.Base().Generation, SHA256: verified.SHA256, RootUUID: f.root.UUID, ExpectedRootRevision: f.root.Revision, RelativePath: media.Base().Basename, Snapshot: verified.Snapshot}
}
func (f fileContentFixture) record(t *testing.T, input models.FileContentInput) *models.FileContentVerification {
	t.Helper()
	var ret *models.FileContentVerification
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = f.repo.FileContent.RecordVerification(ctx, input)
		return err
	}))
	return ret
}
func (f fileContentFixture) reload(t *testing.T, id models.FileID) models.File {
	t.Helper()
	var rows []models.File
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error { var err error; rows, err = f.repo.File.Find(ctx, id); return err }))
	require.Len(t, rows, 1)
	return rows[0]
}

func TestFileContentSharesBytesWithoutMergingEntities(t *testing.T) {
	f := newFileContentFixture(t)
	firstInput := f.input(t, f.file)
	first := f.record(t, firstInput)
	require.Equal(t, f.root.Revision, first.RootRevision)
	require.Equal(t, first, f.record(t, firstInput))
	secondFile := f.addFile(t, "second.mp4", []byte("original media"))
	second := f.record(t, f.input(t, secondFile))
	require.Equal(t, first.Content, second.Content)
	require.NotEqual(t, first.FileUUID, second.FileUUID)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		locations, err := f.repo.FileContent.Locations(ctx, first.Content.UUID, "", 1)
		require.NoError(t, err)
		require.Len(t, locations, 1)
		next, err := f.repo.FileContent.Locations(ctx, first.Content.UUID, locations[0].FileUUID, 1)
		require.NoError(t, err)
		require.Len(t, next, 1)
		require.NotEqual(t, locations[0].FileUUID, next[0].FileUUID)
		found, err := f.repo.FileContent.FindBySHA256(ctx, first.Content.SHA256)
		require.NoError(t, err)
		require.Equal(t, &first.Content, found)
		return nil
	}))
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM media_contents"))
	require.Equal(t, uint(2), queryUint(t, raw, "SELECT count(*) FROM file_content_versions"))
	require.Equal(t, uint(2), queryUint(t, raw, "SELECT count(*) FROM scenes"), "byte identity must not create/merge scenes")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	rows, err := raw.Query(`EXPLAIN QUERY PLAN SELECT v.file_uuid FROM file_content_versions v INDEXED BY file_content_versions_content
CROSS JOIN archive_entities e ON e.uuid=v.file_uuid CROSS JOIN files f ON f.id=e.file_id AND f.generation=v.generation
WHERE v.content_uuid=? AND v.file_uuid>? AND e.state='active' ORDER BY v.file_uuid LIMIT 50`, first.Content.UUID, "")
	require.NoError(t, err)
	defer rows.Close()
	var plan string
	for rows.Next() {
		var a, b, c int
		var detail string
		require.NoError(t, rows.Scan(&a, &b, &c, &detail))
		plan += detail
	}
	require.NoError(t, rows.Err())
	require.Contains(t, plan, "SEARCH v USING COVERING INDEX file_content_versions_content")
	require.NotContains(t, plan, "SCAN f")
}

func TestFileContentPinsReviewedRootRevision(t *testing.T) {
	f := newFileContentFixture(t)
	input := f.input(t, f.file)
	first := f.record(t, input)
	definition := f.root.MediaRootDefinition
	definition.Label = "Renamed media root"
	f.root = putMediaRoot(t, f.repo, models.MediaRootInput{
		UUID: f.root.UUID, ExpectedRevision: f.root.Revision, Origin: "review", MediaRootDefinition: definition,
	})
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.FileContent.RecordVerification(ctx, input)
		return err
	})
	require.ErrorIs(t, err, models.ErrFileGenerationConflict, "new verification must use the reviewed current root definition")
	input.ExpectedRootRevision = f.root.Revision
	require.Equal(t, first, f.record(t, input), "a label change does not rewrite historical verification evidence")
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		input.ExpectedGeneration, err = f.repo.FileContent.Advance(ctx, input.FileUUID, input.ExpectedGeneration)
		return err
	}))
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec(`INSERT INTO file_content_versions(file_uuid,generation,content_uuid,root_uuid,root_revision,relative_path,filesystem_identity,mod_time_text,change_token)
SELECT file_uuid,?,content_uuid,root_uuid,root_revision,relative_path,filesystem_identity,mod_time_text,change_token
FROM file_content_versions WHERE file_uuid=? AND generation=?`, input.ExpectedGeneration, first.FileUUID, first.Generation)
	require.ErrorContains(t, err, "verified file root is inactive")
	second := f.record(t, input)
	require.Equal(t, f.root.Revision, second.RootRevision)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		history, err := f.repo.FileContent.History(ctx, input.FileUUID, 0, 10)
		require.NoError(t, err)
		require.Equal(t, []models.FileContentVerification{*first, *second}, history)
		roots, err := f.repo.MediaRoot.History(ctx, f.root.UUID, 0, 10)
		require.NoError(t, err)
		require.Equal(t, "Content test", roots[0].Label)
		require.Equal(t, "Renamed media root", roots[1].Label)
		return nil
	}))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestFileGenerationChangesOnlyForLocationOrByteEvidence(t *testing.T) {
	f := newFileContentFixture(t)
	input := f.input(t, f.file)
	proof := f.record(t, input)
	base := f.file.Base()
	before := base.Generation
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		base.UpdatedAt = base.UpdatedAt.Add(time.Second)
		if err := f.repo.File.Update(ctx, f.file); err != nil {
			return err
		}
		if err := f.repo.File.ModifyFingerprints(ctx, base.ID, []models.Fingerprint{{Type: models.FingerprintTypeMD5, Fingerprint: "fixture-md5"}, {Type: models.FingerprintTypePhash, Fingerprint: int64(100)}}); err != nil {
			return err
		}
		return f.repo.File.DestroyFingerprints(ctx, base.ID, []string{models.FingerprintTypePhash})
	}))
	f.file = f.reload(t, base.ID)
	require.Equal(t, before, f.file.Base().Generation, "unchanged MD5 and perceptual metadata must not invalidate the verified bytes")
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := f.repo.FileContent.Current(ctx, input.FileUUID)
		require.NoError(t, err)
		require.Equal(t, proof, current)
		return nil
	}))
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return f.repo.File.ModifyFingerprints(ctx, base.ID, []models.Fingerprint{{Type: models.FingerprintTypeMD5, Fingerprint: "changed-md5"}})
	}))
	f.file = f.reload(t, base.ID)
	require.Greater(t, f.file.Base().Generation, before)
	stale := f.file.Clone()
	stale.Base().Generation = before
	stale.Base().Size++
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.repo.File.Update(ctx, stale) })
	require.ErrorIs(t, err, models.ErrFileGenerationConflict, "a delayed rescan must not overwrite newer file metadata")
	stale.Base().Generation = 0
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.repo.File.Update(ctx, stale) })
	require.ErrorIs(t, err, models.ErrFileGenerationConflict)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.FileContent.RecordVerification(ctx, input)
		return err
	})
	require.ErrorIs(t, err, models.ErrFileGenerationConflict)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := f.repo.FileContent.Current(ctx, input.FileUUID)
		require.NoError(t, err)
		require.Nil(t, current)
		history, err := f.repo.FileContent.History(ctx, input.FileUUID, 0, 10)
		require.NoError(t, err)
		require.Equal(t, []models.FileContentVerification{*proof}, history)
		return nil
	}))
	before = f.file.Base().Generation
	f.file.Base().Basename = "renamed.mp4"
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.repo.File.Update(ctx, f.file) }))
	require.Greater(t, f.file.Base().Generation, before)
	before = f.file.Base().Generation
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := f.db.ExecSQL(ctx, "UPDATE folders SET path=path||'-moved' WHERE id=?", []interface{}{f.file.Base().ParentFolderID})
		return err
	}))
	require.Greater(t, f.reload(t, base.ID).Base().Generation, before)
}

func TestFileContentRequiresManagedTransactionAndAnonymisesProofs(t *testing.T) {
	f := newFileContentFixture(t)
	input := f.input(t, f.file)
	ctx, err := f.db.Begin(t.Context(), true)
	require.NoError(t, err)
	_, err = f.repo.FileContent.RecordVerification(ctx, input)
	require.ErrorContains(t, err, "managed write transaction")
	require.NoError(t, f.db.Rollback(ctx))
	f.record(t, input)
	out := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anon, err := sqlite.NewAnonymiser(f.db, out)
	require.NoError(t, err)
	require.NoError(t, anon.Anonymise(t.Context()))
	raw := openRawDB(t, out)
	defer raw.Close()
	for _, table := range []string{"media_contents", "file_content_versions"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestFileContentRejectsStaleDeletedAndConflictingVerifications(t *testing.T) {
	f := newFileContentFixture(t)
	input := f.input(t, f.file)
	first := f.record(t, input)
	changed := input
	changed.SHA256 = strings.Repeat("f", 64)
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.FileContent.RecordVerification(ctx, changed)
		return err
	})
	require.ErrorIs(t, err, models.ErrFileContentConflict)
	var next int64
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		next, err = f.repo.FileContent.Advance(ctx, input.FileUUID, input.ExpectedGeneration)
		return err
	}))
	input.ExpectedGeneration = next
	second := f.record(t, input)
	require.Equal(t, first.Content, second.Content)
	require.Greater(t, second.Generation, first.Generation)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.repo.File.Destroy(ctx, f.file.Base().ID) }))
	replacement := f.addFile(t, "first.mp4", []byte("original media"))
	replacementIdentity := archiveFind(t, f.repo, models.ArchiveFile, int(replacement.Base().ID))
	require.NotEqual(t, f.identity.UUID, replacementIdentity.UUID)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.FileContent.RecordVerification(ctx, input)
		return err
	})
	require.ErrorIs(t, err, models.ErrFileGenerationConflict)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		history, err := f.repo.FileContent.History(ctx, input.FileUUID, first.Generation, 1)
		require.NoError(t, err)
		require.Equal(t, []models.FileContentVerification{*second}, history)
		locations, err := f.repo.FileContent.Locations(ctx, first.Content.UUID, "", 50)
		require.NoError(t, err)
		require.Empty(t, locations)
		return nil
	}))
}

func TestFileContentRollsBackIfGenerationChangesBeforeCommit(t *testing.T) {
	f := newFileContentFixture(t)
	input := f.input(t, f.file)
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.FileContent.RecordVerification(ctx, input)
		if err != nil {
			return err
		}
		_, err = f.repo.FileContent.Advance(ctx, input.FileUUID, input.ExpectedGeneration)
		return err
	})
	require.ErrorIs(t, err, models.ErrFileGenerationConflict)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM media_contents"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM file_content_versions"))
	require.Equal(t, input.ExpectedGeneration, f.reload(t, f.file.Base().ID).Base().Generation)
	f.record(t, input)
	for _, query := range []string{"UPDATE media_contents SET size=size+1", "UPDATE file_content_versions SET generation=generation+1", "UPDATE files SET generation=0", "UPDATE files SET generation=generation-1", "UPDATE files SET generation=generation+0.5"} {
		_, err := raw.Exec(query)
		require.Error(t, err, query)
	}
	// UUID adoption preserves the verified graph and creates an old-UUID alias.
	entity := archiveFind(t, f.repo, models.ArchiveFile, int(f.file.Base().ID))
	canonical := uuid.NewString()
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveEntity.AdoptUUID(ctx, entity.UUID, canonical, entity.Revision)
		return err
	}))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		proof, err := f.repo.FileContent.Current(ctx, canonical)
		require.NoError(t, err)
		require.NotNil(t, proof)
		require.Equal(t, canonical, proof.FileUUID)
		return nil
	}))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestFileContentMigrationPreservesExistingFingerprintsAndIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema17.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+17; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(t.Context(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	defer raw.Close()
	_, err = raw.Exec(`INSERT INTO folders(id,path,basename,mod_time,created_at,updated_at)
VALUES(1,'/file-content-fixture','file-content-fixture',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO files(id,parent_folder_id,basename,size,mod_time,created_at,updated_at)
VALUES(21,1,'retained.mp4',1000,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);`)
	require.NoError(t, err)
	_, err = raw.Exec("INSERT INTO files_fingerprints(file_id,type,fingerprint) VALUES(21,'md5','retained-md5'),(21,'oshash','retained-oshash')")
	require.NoError(t, err)
	var identity string
	require.NoError(t, raw.QueryRow("SELECT uuid FROM archive_entities WHERE file_id=21").Scan(&identity))
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	repo := db.Repository()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		files, err := repo.File.Find(ctx, 21)
		require.NoError(t, err)
		require.Len(t, files, 1)
		require.EqualValues(t, 1, files[0].Base().Generation)
		require.Equal(t, "retained-md5", files[0].Base().Fingerprints.Get(models.FingerprintTypeMD5))
		require.Equal(t, "retained-oshash", files[0].Base().Fingerprints.Get(models.FingerprintTypeOshash))
		entity, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveFile, 21)
		require.NoError(t, err)
		require.Equal(t, identity, entity.UUID)
		current, err := repo.FileContent.Current(ctx, identity)
		require.NoError(t, err)
		require.Nil(t, current, "old fingerprints are not proof of SHA-256-verified content")
		return nil
	}))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM media_contents"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM file_content_versions"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestFileContentStartupRequiresGenerationGuards(t *testing.T) {
	for _, name := range []string{"file_generation_forward", "file_content_version_valid", "file_content_versions_content"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "incomplete.sqlite")
			writeEmptyNativeFixture(t, path)
			raw := openRawDB(t, path)
			kind := "TRIGGER"
			if name == "file_content_versions_content" {
				kind = "INDEX"
			}
			_, err := raw.Exec("DROP " + kind + " " + name)
			require.NoError(t, err)
			require.NoError(t, raw.Close())
			db := sqlite.NewDatabase()
			require.ErrorContains(t, db.Open(path), "missing "+name)
		})
	}
}
