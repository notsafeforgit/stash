package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func sourceAttachmentEntry(position int, id string) models.SourceAttachmentEntry {
	return models.SourceAttachmentEntry{Position: position, Reference: models.SourcePostIdentifier{Namespace: "native:twitter", Value: id}, MediaKind: "image"}
}

func recordAttachmentManifest(t *testing.T, repo models.Repository, input models.SourceAttachmentManifestInput) *models.SourceAttachmentManifest {
	t.Helper()
	var ret *models.SourceAttachmentManifest
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceAttachment.RecordManifest(ctx, input)
		return err
	}))
	return ret
}

func manifestEntries(t *testing.T, repo models.Repository, id string) []models.SourceAttachmentManifestEntry {
	t.Helper()
	var ret []models.SourceAttachmentManifestEntry
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceAttachment.ManifestEntries(ctx, id, -1, 100)
		return err
	}))
	return ret
}

func findAttachment(t *testing.T, repo models.Repository, id string) *models.SourceAttachment {
	t.Helper()
	var ret *models.SourceAttachment
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceAttachment.Find(ctx, id)
		return err
	}))
	return ret
}

func attachmentFixture(t *testing.T, repo models.Repository) (*models.SourceCapture, *models.SourceAttachment) {
	t.Helper()
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: uuid.NewString()}, "")
	capture := recordSourceTestCapture(t, repo, sourceTestCapture(t, post.UUID, 1, "Biography"))
	manifest := recordAttachmentManifest(t, repo, models.SourceAttachmentManifestInput{CaptureUUID: capture.UUID, Complete: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "98432109876543210987"), sourceAttachmentEntry(1, "second-image")}})
	return capture, &manifestEntries(t, repo, manifest.UUID)[0].Attachment
}

func TestSourceAttachmentSharedManifestsPreserveOrderPartialListsAndIdentity(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "album"}, "")
	one := recordSourceTestCapture(t, repo, sourceTestCapture(t, post.UUID, 1, "bio"))
	two := recordSourceTestCapture(t, repo, sourceTestCapture(t, post.UUID, 2, "changed bio"))
	input := models.SourceAttachmentManifestInput{CaptureUUID: one.UUID, Complete: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(1, "SecondID"), sourceAttachmentEntry(0, "FirstID"), sourceAttachmentEntry(2, "FirstID")}}
	first := recordAttachmentManifest(t, repo, input)
	require.True(t, first.IsAlbum())
	input.CaptureUUID = two.UUID
	require.Equal(t, first, recordAttachmentManifest(t, repo, input), "per-file captures share one source list")
	entries := manifestEntries(t, repo, first.UUID)
	require.Len(t, entries, 3)
	require.Equal(t, "FirstID", entries[0].Attachment.Reference.Value)
	require.Equal(t, entries[0].Attachment.UUID, entries[2].Attachment.UUID, "repeated slots reuse the attachment")
	require.NotEqual(t, entries[0].Attachment.UUID, entries[1].Attachment.UUID)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	require.Equal(t, first, recordAttachmentManifest(t, repo, input))
	input.Entries[0].Reference.Value = "ChangedID"
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceAttachment.RecordManifest(ctx, input)
		return err
	})
	require.ErrorIs(t, err, models.ErrAttachmentManifestReplay)
	third := recordSourceTestCapture(t, repo, sourceTestCapture(t, post.UUID, 3, "bio"))
	count := 4
	partial := recordAttachmentManifest(t, repo, models.SourceAttachmentManifestInput{CaptureUUID: third.UUID, ExpectedCount: &count,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(2, "FirstID")}})
	require.False(t, partial.Complete)
	require.True(t, partial.IsAlbum())
	require.Equal(t, entries[0].Attachment.UUID, manifestEntries(t, repo, partial.UUID)[0].Attachment.UUID)
	require.Equal(t, entries, manifestEntries(t, repo, first.UUID), "a partial capture cannot erase the earlier album")
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		page, err := repo.SourceAttachment.ManifestEntries(ctx, first.UUID, 0, 1)
		require.NoError(t, err)
		require.Equal(t, entries[1:2], page)
		_, err = repo.SourceAttachment.ManifestEntries(ctx, first.UUID, -2, 1)
		require.Error(t, err)
		_, err = repo.SourceAttachment.ManifestEntries(ctx, first.UUID, -1, 101)
		require.Error(t, err)
		return nil
	}))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Equal(t, uint(2), queryUint(t, raw, "SELECT count(*) FROM source_attachments"))
	require.Equal(t, uint(2), queryUint(t, raw, "SELECT count(*) FROM source_attachment_manifests"))
	require.Equal(t, uint(3), queryUint(t, raw, "SELECT count(*) FROM source_capture_attachment_manifests"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM galleries"), "evidence alone must not create library records")
	require.Equal(t, uint(2), queryUint(t, raw, "SELECT count(*) FROM scenes"))
}

func TestSourceAttachmentManifestRollbackCorruptionAndForgottenPost(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "post"}, "")
	capture := recordSourceTestCapture(t, repo, sourceTestCapture(t, post.UUID, 1, "bio"))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	before := queryUint(t, raw, "SELECT revision FROM source_posts")
	_, err := raw.Exec("CREATE TRIGGER reject_manifest BEFORE INSERT ON source_capture_attachment_manifests BEGIN SELECT RAISE(ABORT, 'fixture failure'); END")
	require.NoError(t, err)
	input := models.SourceAttachmentManifestInput{CaptureUUID: capture.UUID, Complete: true, Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "new")}}
	err = repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceAttachment.RecordManifest(ctx, input)
		return err
	})
	require.ErrorContains(t, err, "fixture failure")
	for _, table := range []string{"source_attachments", "source_attachment_manifests", "source_attachment_entries", "source_capture_attachment_manifests"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	require.Equal(t, before, queryUint(t, raw, "SELECT revision FROM source_posts"))
	_, err = raw.Exec("DROP TRIGGER reject_manifest")
	require.NoError(t, err)
	manifest := recordAttachmentManifest(t, repo, input)
	_, err = raw.Exec("UPDATE source_attachment_entries SET position=2")
	require.ErrorContains(t, err, "immutable")
	_, err = raw.Exec("DELETE FROM source_attachment_entries WHERE manifest_uuid=?", manifest.UUID)
	require.NoError(t, err)
	err = repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceAttachment.FindManifest(ctx, manifest.UUID)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourcePayloadCorrupt)
	capture2 := recordSourceTestCapture(t, repo, sourceTestCapture(t, post.UUID, 2, "bio"))
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten'")
	require.NoError(t, err)
	input.CaptureUUID = capture2.UUID
	err = repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceAttachment.RecordManifest(ctx, input)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourcePostForgotten)
}

func TestSourceAttachmentMigrationPreservesGalleryAndSourceEvidence(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "before-attachments.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+7; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(context.Background(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	_, err = raw.Exec(archiveIdentityFixture)
	require.NoError(t, err)
	_, err = raw.Exec(`INSERT INTO galleries(id, title, created_at, updated_at) VALUES (81, 'Manual album', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO galleries_images(gallery_id, image_id, cover) VALUES (81, 41, 1);
INSERT INTO scenes_galleries(gallery_id, scene_id) VALUES (81, 31);`)
	require.NoError(t, err)
	var galleryUUID string
	require.NoError(t, raw.QueryRow("SELECT uuid FROM archive_entities WHERE gallery_id=81").Scan(&galleryUUID))
	postUUID := uuid.NewString()
	_, err = raw.Exec("INSERT INTO source_posts(uuid) VALUES (?)", postUUID)
	require.NoError(t, err)
	_, err = raw.Exec("INSERT INTO source_post_identifiers(post_uuid, namespace, value) VALUES (?, 'native:reddit', 'existing-post')", postUUID)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	require.Equal(t, galleryUUID, archiveFind(t, db.Repository(), models.ArchiveGallery, 81).UUID)
	raw = openRawDB(t, path)
	defer raw.Close()
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM gallery_covers c JOIN archive_entities a ON a.uuid=c.media_uuid WHERE c.gallery_id=81 AND a.image_id=41"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM scenes_galleries WHERE gallery_id=81 AND scene_id=31"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM source_post_identifiers WHERE value='existing-post'"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_attachments"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}
