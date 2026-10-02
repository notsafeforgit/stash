package sqlite_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestSourcePostMediaEvidenceRetainsUnknownOrderAndCapture(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "old-album"}, "")
	media := archiveFind(t, repo, models.ArchiveImage, 41)
	input := models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: post.UUID, MediaUUID: media.UUID, Basis: "legacy",
		Details: []byte(`{"snapshot":"frozen","source_relpath":"creator/image.jpg","position":null}`)}
	stored := recordMediaEvidence(t, repo, input)
	require.Empty(t, stored.AttachmentUUID)
	require.Empty(t, stored.CaptureUUID)
	require.Nil(t, stored.FileUUID)
	require.JSONEq(t, string(input.Details), string(stored.Details))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Equal(t, uint(post.Revision+1), queryUint(t, raw, "SELECT revision FROM source_posts"))
	require.Equal(t, stored, recordMediaEvidence(t, repo, input))
	require.Equal(t, uint(post.Revision+1), queryUint(t, raw, "SELECT revision FROM source_posts"))
	for _, table := range []string{"source_captures", "source_attachments", "source_attachment_manifests", "post_attachment_selections", "attachment_media_links", "galleries"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table), table)
	}
	// Multiple posts can reference the same media without merging posts or
	// creating another image. Targeted pagination must not cross that boundary.
	other := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "repost"}, "")
	second := input
	second.UUID, second.PostUUID = uuid.NewString(), other.UUID
	recordMediaEvidence(t, repo, second)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		page, err := repo.SourceAttachment.PostMediaEvidence(ctx, post.UUID, "", 1)
		require.NoError(t, err)
		require.Equal(t, []models.SourceMediaEvidence{*stored}, page)
		page, err = repo.SourceAttachment.PostMediaEvidence(ctx, post.UUID, stored.UUID, 1)
		require.NoError(t, err)
		require.Empty(t, page)
		_, err = repo.SourceAttachment.PostMediaEvidence(ctx, post.UUID, "invalid", 1)
		require.Error(t, err)
		_, err = repo.SourceAttachment.PostMediaEvidence(ctx, post.UUID, "", 101)
		require.Error(t, err)
		return nil
	}))
	second.UUID = input.UUID
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceAttachment.RecordMediaEvidence(ctx, second)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourceMediaEvidenceReplay)
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM images"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestSourcePostMediaEvidenceOptionalScopeCannotInventProof(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	capture, attachment := attachmentFixture(t, repo)
	otherCapture, otherAttachment := attachmentFixture(t, repo)
	withoutManifest := recordSourceTestCapture(t, repo, sourceTestCapture(t, capture.PostUUID, 2, "bio"))
	media := archiveFind(t, repo, models.ArchiveScene, 31)
	file := archiveFind(t, repo, models.ArchiveFile, 21)
	base := models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: capture.PostUUID, MediaUUID: media.UUID, Basis: "legacy"}
	apply := func(input models.SourceMediaEvidence) error {
		return repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.SourceAttachment.RecordMediaEvidence(ctx, input)
			return err
		})
	}
	for _, tc := range []struct {
		name, capture, attachment string
	}{
		{"post only", "", ""},
		{"capture only without manifest", withoutManifest.UUID, ""},
		{"attachment only", "", attachment.UUID},
		{"proven attachment and capture", capture.UUID, attachment.UUID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := base
			input.UUID, input.CaptureUUID, input.AttachmentUUID = uuid.NewString(), tc.capture, tc.attachment
			stored := recordMediaEvidence(t, repo, input)
			require.Equal(t, tc.capture, stored.CaptureUUID)
			require.Equal(t, tc.attachment, stored.AttachmentUUID)
			require.Equal(t, stored, recordMediaEvidence(t, repo, input))
			// Adding a previously unknown scope is new evidence, never an edit.
			if input.CaptureUUID == "" {
				input.CaptureUUID = capture.UUID
			} else {
				input.CaptureUUID = ""
			}
			require.ErrorIs(t, apply(input), models.ErrSourceMediaEvidenceReplay)
		})
	}
	for _, tc := range []struct{ capture, attachment, message string }{
		{otherCapture.UUID, "", "another post"},
		{uuid.NewString(), "", "missing"},
		{"", otherAttachment.UUID, "review changed"},
		{withoutManifest.UUID, attachment.UUID, "containing this attachment"},
	} {
		input := base
		input.UUID, input.CaptureUUID, input.AttachmentUUID = uuid.NewString(), tc.capture, tc.attachment
		require.ErrorContains(t, apply(input), tc.message)
	}
	for _, basis := range []string{"observed-file", "verified-bytes"} {
		input := base
		input.Basis, input.FileUUID = basis, &file.UUID
		require.ErrorContains(t, apply(input), "requires an attachment, capture and file")
		input.AttachmentUUID = attachment.UUID
		require.ErrorContains(t, apply(input), "requires an attachment, capture and file")
		input.CaptureUUID, input.FileUUID = capture.UUID, nil
		require.ErrorContains(t, apply(input), "requires an attachment, capture and file")
	}
	attachment = findAttachment(t, repo, attachment.UUID)
	require.ErrorIs(t, applyMediaChoice(repo, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID,
		ExpectedAttachmentRevision: attachment.Revision, State: "linked", MediaUUID: media.UUID,
		ExpectedMediaRevision: media.Revision, Origin: "ingest"}), models.ErrAmbiguousSourceMedia)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Equal(t, uint(4), queryUint(t, raw, "SELECT count(*) FROM source_media_evidence"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM attachment_media_links"))
}

func TestSourcePostMediaEvidenceSQLGuardsRollbackRetirementAndAdoption(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	capture, attachment := attachmentFixture(t, repo)
	otherCapture, otherAttachment := attachmentFixture(t, repo)
	media := archiveFind(t, repo, models.ArchiveScene, 31)
	input := models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: attachment.PostUUID, MediaUUID: media.UUID, Basis: "legacy"}
	stored := recordMediaEvidence(t, repo, input)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	for _, change := range []string{
		"attachment_uuid='" + attachment.UUID + "'",
		"capture_uuid='" + capture.UUID + "'",
		"post_uuid='" + otherAttachment.PostUUID + "'",
		"basis='review'",
	} {
		_, err := raw.Exec("UPDATE source_media_evidence SET "+change+" WHERE uuid=?", stored.UUID)
		require.ErrorContains(t, err, "immutable")
	}
	for _, tc := range []struct {
		attachment, capture interface{}
		basis, message      string
	}{
		{otherAttachment.UUID, nil, "legacy", "FOREIGN KEY"},
		{nil, otherCapture.UUID, "legacy", "FOREIGN KEY"},
		{attachment.UUID, capture.UUID, "legacy", "CHECK"},
		{nil, nil, "verified-bytes", "CHECK"},
	} {
		_, err := raw.Exec(`INSERT INTO source_media_evidence(uuid,post_uuid,attachment_uuid,capture_uuid,media_uuid,basis,details)
 VALUES(?,?,?,?,?,?,'{}')`, uuid.NewString(), input.PostUUID, tc.attachment, tc.capture, input.MediaUUID, tc.basis)
		require.ErrorContains(t, err, tc.message)
	}
	// Failure in a revision trigger must also undo evidence insertion, even
	// when a caller handles the failed statement and commits its transaction.
	before := queryUint(t, raw, "SELECT sum(revision) FROM source_posts")
	_, err := raw.Exec(`CREATE TRIGGER reject_post_media_revision BEFORE UPDATE OF revision ON source_posts
 BEGIN SELECT RAISE(ABORT,'fixture revision failure'); END`)
	require.NoError(t, err)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		failed := input
		failed.UUID = uuid.NewString()
		_, err := repo.SourceAttachment.RecordMediaEvidence(ctx, failed)
		require.ErrorContains(t, err, "fixture revision failure")
		return nil
	}))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM source_media_evidence"))
	require.Equal(t, before, queryUint(t, raw, "SELECT sum(revision) FROM source_posts"))
	_, err = raw.Exec("DROP TRIGGER reject_post_media_revision")
	require.NoError(t, err)
	adopted := uuid.NewString()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, media.UUID, adopted, media.Revision)
		return err
	}))
	require.Equal(t, adopted, recordMediaEvidence(t, repo, input).MediaUUID)
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=?", input.PostUUID)
	require.NoError(t, err)
	require.Equal(t, adopted, recordMediaEvidence(t, repo, input).MediaUUID, "historical replay survives retirement")
	input.UUID = uuid.NewString()
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceAttachment.RecordMediaEvidence(ctx, input)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourcePostForgotten)
	_, err = raw.Exec(`INSERT INTO source_media_evidence(uuid,post_uuid,media_uuid,basis,details) VALUES(?,?,?,'legacy','{}')`, input.UUID, input.PostUUID, adopted)
	require.ErrorContains(t, err, "forgotten")
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestSourcePostMediaMigrationPreservesPopulatedAttachmentEvidence(t *testing.T) {
	fixture, repo := archiveTestDatabase(t)
	capture, attachment := attachmentFixture(t, repo)
	path := filepath.Join(t.TempDir(), "schema35.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+35; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(t.Context(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	_, err = raw.Exec(`INSERT INTO scenes(id,title,created_at,updated_at) VALUES(31,'Kept media',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO folders(id,path,basename,mod_time,created_at,updated_at) VALUES(1,'/media','media',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO files(id,parent_folder_id,basename,size,mod_time,created_at,updated_at) VALUES(21,1,'kept.mp4',1000,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);`)
	require.NoError(t, err)
	_, err = raw.Exec("ATTACH DATABASE ? AS source_fixture", fixture.DatabasePath())
	require.NoError(t, err)
	for _, table := range []string{"source_posts", "source_post_identifiers", "source_payloads", "source_profile_bodies", "source_post_revisions", "source_captures", "source_capture_profiles", "source_attachments", "source_attachment_manifests", "source_attachment_entries", "source_capture_attachment_manifests"} {
		_, err := raw.Exec("INSERT INTO " + table + " SELECT * FROM source_fixture." + table)
		require.NoError(t, err, table)
	}
	_, err = raw.Exec("DETACH DATABASE source_fixture")
	require.NoError(t, err)
	var media, file string
	require.NoError(t, raw.QueryRow("SELECT uuid FROM archive_entities WHERE scene_id=31").Scan(&media))
	require.NoError(t, raw.QueryRow("SELECT uuid FROM archive_entities WHERE file_id=21").Scan(&file))
	expected := models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: attachment.PostUUID, AttachmentUUID: attachment.UUID,
		CaptureUUID: capture.UUID, MediaUUID: media, FileUUID: &file, Basis: "legacy", Details: []byte(`{"source_id":98765432109876543210}`),
		CreatedAt: time.Date(2025, 6, 7, 8, 9, 10, 123456789, time.UTC)}
	_, err = raw.Exec(`INSERT INTO source_media_evidence(uuid,attachment_uuid,capture_uuid,manifest_uuid,position,media_uuid,file_uuid,basis,details,created_at)
 SELECT ?,e.attachment_uuid,?,e.manifest_uuid,e.position,?,?,'legacy',?,?
 FROM source_attachment_entries e WHERE e.attachment_uuid=?`, expected.UUID, capture.UUID, media, file, string(expected.Details), expected.CreatedAt, attachment.UUID)
	require.NoError(t, err)
	before := queryUint(t, raw, "SELECT revision FROM source_posts")
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	require.Equal(t, &expected, recordMediaEvidence(t, db.Repository(), expected))
	require.Equal(t, before, queryUint(t, raw, "SELECT revision FROM source_posts"), "migration and replay cannot alter review revisions")
	var manifest string
	var position int
	require.NoError(t, raw.QueryRow("SELECT manifest_uuid,position FROM source_media_evidence").Scan(&manifest, &position))
	require.Zero(t, position)
	require.NotEmpty(t, manifest)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(path))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	var integrity string
	require.NoError(t, raw.QueryRow("PRAGMA quick_check").Scan(&integrity))
	require.Equal(t, "ok", integrity)
}

func TestSourcePostMediaStartupRejectsCorruptScopeBeforeWriting(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	_, attachment := attachmentFixture(t, repo)
	_, other := attachmentFixture(t, repo)
	media := archiveFind(t, repo, models.ArchiveImage, 41)
	stored := recordMediaEvidence(t, repo, models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: attachment.PostUUID, MediaUUID: media.UUID, Basis: "legacy"})
	require.NoError(t, db.Close())
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	// Restore the real guard after simulating corruption; startup must validate
	// retained rows rather than checking only that a trigger name exists.
	var guard string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='source_media_evidence_immutable'").Scan(&guard))
	_, err := raw.Exec("DROP TRIGGER source_media_evidence_immutable; PRAGMA foreign_keys=OFF")
	require.NoError(t, err)
	_, err = raw.Exec("UPDATE source_media_evidence SET attachment_uuid=? WHERE uuid=?", other.UUID, stored.UUID)
	require.NoError(t, err)
	_, err = raw.Exec(guard)
	require.NoError(t, err)
	before, err := os.ReadFile(db.DatabasePath())
	require.NoError(t, err)
	require.ErrorContains(t, db.Open(db.DatabasePath()), "invalid source media evidence scope")
	after, err := os.ReadFile(db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}
