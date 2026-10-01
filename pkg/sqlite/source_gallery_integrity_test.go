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

func singleSourceAlbum(t *testing.T, repo models.Repository) (string, *models.SourceGallerySyncResult) {
	t.Helper()
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: uuid.NewString()}, "")
	selection := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{DeclaredAlbum: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "one")}})
	chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveScene, 31)
	return post.UUID, syncSourceGallery(t, repo, post.UUID)
}

func decideSourceGallery(repo models.Repository, input models.SourceGalleryChoiceInput) error {
	return repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceGallery.DecideAssociation(ctx, input)
		return err
	})
}

func TestSourceGalleryAssociationRequiresExplicitCurrentReview(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post, original := singleSourceAlbum(t, repo)
	manual := createArchiveGallery(t, repo, "Existing manual collection")
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error { return repo.Gallery.AddImages(ctx, manual.ID, 41) }))
	identity := archiveFind(t, repo, models.ArchiveGallery, manual.ID)
	input := models.SourceGalleryChoiceInput{PostUUID: post, ExpectedPostRevision: selectionPost(t, repo, post).Revision, State: "linked",
		GalleryUUID: identity.UUID, ExpectedGalleryRevision: identity.Revision, Origin: "ingest"}
	require.ErrorContains(t, decideSourceGallery(repo, input), "review or migration")
	input.Origin = "review"
	input.ExpectedGalleryRevision--
	require.ErrorIs(t, decideSourceGallery(repo, input), models.ErrSourceGalleryConflict)
	input.ExpectedGalleryRevision++
	require.NoError(t, decideSourceGallery(repo, input))
	adopted := syncSourceGallery(t, repo, post)
	require.False(t, adopted.Created)
	require.Equal(t, identity.UUID, adopted.GalleryUUID)
	sourceGalleryMemberships(t, repo, manual.ID, []int{41}, []int{31})
	sourceGalleryMemberships(t, repo, *original.GalleryID, nil, []int{31})
	other := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: uuid.NewString()}, "")
	input.PostUUID, input.ExpectedPostRevision = other.UUID, other.Revision
	input.ExpectedGalleryRevision = archiveFind(t, repo, models.ArchiveGallery, manual.ID).Revision
	require.ErrorIs(t, decideSourceGallery(repo, input), models.ErrSourceGalleryConflict, "two posts cannot claim the same gallery")
	input.PostUUID, input.ExpectedPostRevision, input.State = post, selectionPost(t, repo, post).Revision, "disabled"
	input.GalleryUUID, input.ExpectedGalleryRevision = "", 0
	require.NoError(t, decideSourceGallery(repo, input))
	require.Equal(t, "disabled", syncSourceGallery(t, repo, post).Action)
	// Edits to a previously adopted manual gallery retain intent even while its
	// source association is disabled; reenabling must not undo the removal.
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.Scene.UpdatePartial(ctx, 31, models.ScenePartial{GalleryIDs: &models.UpdateIDs{Mode: models.RelationshipUpdateModeRemove, IDs: []int{manual.ID}}})
		return err
	}))
	identity = archiveFind(t, repo, models.ArchiveGallery, manual.ID)
	input.ExpectedPostRevision, input.State, input.GalleryUUID, input.ExpectedGalleryRevision = selectionPost(t, repo, post).Revision, "linked", identity.UUID, identity.Revision
	require.NoError(t, decideSourceGallery(repo, input))
	require.Empty(t, syncSourceGallery(t, repo, post).Added)
	sourceGalleryMemberships(t, repo, manual.ID, []int{41}, nil)
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		history, err := repo.SourceGallery.AssociationHistory(ctx, post, 0, 100)
		require.NoError(t, err)
		require.Len(t, history, 4)
		page, err := repo.SourceGallery.AssociationHistory(ctx, post, history[1].Revision, 1)
		require.NoError(t, err)
		require.Equal(t, history[2:3], page)
		_, err = repo.SourceGallery.AssociationHistory(ctx, post, -1, 1)
		require.Error(t, err)
		_, err = repo.SourceGallery.MembershipHistory(ctx, identity.UUID, 0, 101)
		require.Error(t, err)
		gallery, err := repo.Gallery.Find(ctx, manual.ID)
		require.NoError(t, err)
		require.Equal(t, models.GalleryOriginManual, gallery.Origin)
		return nil
	}))
	// Neither folder nor ZIP galleries can be adopted by this source service.
	folder := models.NewGallery()
	folder.Title, folder.FolderID = "Folder", new(models.FolderID(1))
	zip := models.NewGallery()
	zip.Title = "ZIP"
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		if err := repo.Gallery.Create(ctx, &models.CreateGalleryInput{Gallery: &folder}); err != nil {
			return err
		}
		return repo.Gallery.Create(ctx, &models.CreateGalleryInput{Gallery: &zip, FileIDs: []models.FileID{21}})
	}))
	for _, gallery := range []models.Gallery{folder, zip} {
		require.Equal(t, models.GalleryOriginFilesystem, gallery.Origin)
		identity = archiveFind(t, repo, models.ArchiveGallery, gallery.ID)
		input.GalleryUUID, input.ExpectedGalleryRevision, input.ExpectedPostRevision = identity.UUID, identity.Revision, selectionPost(t, repo, post).Revision
		require.ErrorContains(t, decideSourceGallery(repo, input), "folder or ZIP")
	}
	// File associations still count when no primary file has been selected.
	// A formerly manual gallery can acquire such a link after adoption.
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error { return repo.Gallery.AddFileID(ctx, manual.ID, 21) }))
	identity = archiveFind(t, repo, models.ArchiveGallery, manual.ID)
	input.GalleryUUID, input.ExpectedGalleryRevision = identity.UUID, identity.Revision
	require.ErrorContains(t, decideSourceGallery(repo, input), "folder or ZIP")
	require.Equal(t, "review", sourceGalleryPreview(t, repo, post).Action)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec("UPDATE galleries SET origin='manual' WHERE id=?", *original.GalleryID)
	require.ErrorContains(t, err, "immutable")
	_, err = raw.Exec("UPDATE galleries SET folder_id=1 WHERE id=?", *original.GalleryID)
	require.ErrorContains(t, err, "filesystem")
	_, err = raw.Exec(`INSERT INTO galleries_files(gallery_id, file_id, "primary") VALUES (?, 21, 1)`, *original.GalleryID)
	require.ErrorContains(t, err, "filesystem")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestSourceGalleryUUIDAdoptionMergeAndDeletion(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post, album := singleSourceAlbum(t, repo)
	originalHistory := sourceGalleryHistory(t, repo, album.GalleryUUID)
	gallery := archiveFind(t, repo, models.ArchiveGallery, *album.GalleryID)
	media := archiveFind(t, repo, models.ArchiveScene, 31)
	adoptedGallery, adoptedMedia := uuid.NewString(), uuid.NewString()
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		if _, err := repo.ArchiveEntity.AdoptUUID(ctx, gallery.UUID, adoptedGallery, gallery.Revision); err != nil {
			return err
		}
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, media.UUID, adoptedMedia, media.Revision)
		return err
	}))
	history := sourceGalleryHistory(t, repo, adoptedGallery)
	require.Len(t, history, 1)
	require.Equal(t, originalHistory[0].UUID, history[0].UUID)
	require.Equal(t, adoptedMedia, history[0].MediaUUID)
	preview := sourceGalleryPreview(t, repo, post)
	require.Equal(t, adoptedGallery, preview.Gallery.UUID)
	require.Equal(t, adoptedMedia, *preview.Entries[0].MediaUUID)
	require.Empty(t, syncSourceGallery(t, repo, post).Added)
	// Merge redirects retain the chosen attachment and resolve it to the survivor.
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		if err := repo.Scene.RedirectMergedIdentities(ctx, []int{31}, 32); err != nil {
			return err
		}
		return repo.Scene.Destroy(ctx, 31)
	}))
	require.Equal(t, history, sourceGalleryHistory(t, repo, adoptedGallery), "merge deletion is not a manual exclusion")
	survivor := archiveFind(t, repo, models.ArchiveScene, 32)
	preview = sourceGalleryPreview(t, repo, post)
	require.Equal(t, survivor.UUID, *preview.Entries[0].MediaUUID)
	require.Equal(t, []string{survivor.UUID}, syncSourceGallery(t, repo, post).Added)
	sourceGalleryMemberships(t, repo, *album.GalleryID, nil, []int{32})
	// Gallery redirects require an explicit association review instead of
	// silently combining two source posts through the destination gallery.
	other := createArchiveGallery(t, repo, "Merge target")
	gallery = archiveFind(t, repo, models.ArchiveGallery, *album.GalleryID)
	target := archiveFind(t, repo, models.ArchiveGallery, other.ID)
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		if err := repo.ArchiveEntity.Redirect(ctx, gallery.UUID, target.UUID, gallery.Revision); err != nil {
			return err
		}
		return repo.Gallery.Destroy(ctx, *album.GalleryID)
	}))
	preview = sourceGalleryPreview(t, repo, post)
	require.Equal(t, "review", preview.Action)
	require.ErrorIs(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceGallery.Sync(ctx, post, preview.Signature)
		return err
	}), models.ErrSourceGalleryConflict)
	require.NoError(t, decideSourceGallery(repo, models.SourceGalleryChoiceInput{PostUUID: post, ExpectedPostRevision: selectionPost(t, repo, post).Revision,
		State: "linked", GalleryUUID: target.UUID, ExpectedGalleryRevision: target.Revision, Origin: "review"}))
	syncSourceGallery(t, repo, post)
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error { return repo.Gallery.Destroy(ctx, other.ID) }))
	attachmentSQL(t, db, "INSERT INTO galleries(id, title, created_at, updated_at) VALUES (?, 'Reused ID', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)", other.ID)
	require.Equal(t, "disabled", syncSourceGallery(t, repo, post).Action)
	sourceGalleryMemberships(t, repo, other.ID, nil, nil)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestSourceGalleryAtomicWritesAndAuditGuards(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "atomic"}, "")
	selection := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{DeclaredAlbum: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "one")}})
	media := chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveScene, 31)
	preview := sourceGalleryPreview(t, repo, post.UUID)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	apply := func() error {
		return repo.WithTxn(context.Background(), func(ctx context.Context) error {
			_, err := repo.SourceGallery.Sync(ctx, post.UUID, preview.Signature)
			return err
		})
	}
	for _, failure := range []struct{ table, action string }{{"post_gallery_links", "INSERT"}, {"gallery_membership_events", "INSERT"}, {"source_gallery_write_context", "DELETE"}} {
		_, err := raw.Exec("CREATE TRIGGER reject_album_write BEFORE " + failure.action + " ON " + failure.table + " BEGIN SELECT RAISE(ABORT, 'injected album failure'); END")
		require.NoError(t, err)
		require.ErrorContains(t, apply(), "injected album failure")
		require.Equal(t, preview, sourceGalleryPreview(t, repo, post.UUID))
		for _, table := range []string{"galleries", "post_gallery_decisions", "post_gallery_links", "gallery_membership_events", "gallery_membership_heads", "source_gallery_write_context"} {
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
		}
		if failure.table == "source_gallery_write_context" {
			require.ErrorContains(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
				_, syncErr := repo.SourceGallery.Sync(ctx, post.UUID, preview.Signature)
				require.ErrorContains(t, syncErr, "injected album failure")
				return nil // Even an incorrectly ignored cleanup error cannot commit the marker.
			}), "write context was not closed")
		}
		_, err = raw.Exec("DROP TRIGGER reject_album_write")
		require.NoError(t, err)
	}
	result := syncSourceGallery(t, repo, post.UUID)
	history := sourceGalleryHistory(t, repo, result.GalleryUUID)
	_, err := raw.Exec("UPDATE gallery_membership_events SET state='excluded' WHERE uuid=?", history[0].UUID)
	require.ErrorContains(t, err, "immutable")
	_, err = raw.Exec("UPDATE post_gallery_decisions SET reason='Changed' WHERE post_uuid=?", post.UUID)
	require.ErrorContains(t, err, "immutable")
	performer := archiveFind(t, repo, models.ArchivePerformer, 71)
	_, err = raw.Exec(`INSERT INTO gallery_membership_events(uuid, gallery_uuid, media_uuid, state, origin) VALUES (?, ?, ?, 'included', 'library')`, uuid.NewString(), result.GalleryUUID, performer.UUID)
	require.ErrorContains(t, err, "scene or image")
	otherPost, otherAlbum := singleSourceAlbum(t, repo)
	_, err = raw.Exec(`INSERT INTO gallery_membership_events(uuid, gallery_uuid, media_uuid, state, origin, post_uuid, selection_uuid)
VALUES (?, ?, ?, 'included', 'source', ?, ?)`, uuid.NewString(), result.GalleryUUID, media.UUID, otherPost, selection.Decision.UUID)
	require.ErrorContains(t, err, "FOREIGN KEY")
	_, err = raw.Exec(`INSERT INTO source_gallery_write_context(gallery_uuid, post_uuid, selection_uuid) VALUES (?, ?, ?)`, otherAlbum.GalleryUUID, post.UUID, selection.Decision.UUID)
	require.ErrorContains(t, err, "FOREIGN KEY")
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.Scene.UpdatePartial(ctx, 31, models.ScenePartial{GalleryIDs: &models.UpdateIDs{Mode: models.RelationshipUpdateModeRemove, IDs: []int{*result.GalleryID}}})
		return err
	}))
	_, err = raw.Exec("UPDATE gallery_membership_heads SET event_uuid=? WHERE gallery_uuid=?", history[0].UUID, result.GalleryUUID)
	require.ErrorContains(t, err, "backwards")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_gallery_write_context"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestSourceGalleryMigrationPreservesExistingGalleriesAndIdentities(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "before-source-galleries.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+9; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(context.Background(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	_, err = raw.Exec(archiveIdentityFixture)
	require.NoError(t, err)
	_, err = raw.Exec(`INSERT INTO galleries(id, title, folder_id, created_at, updated_at) VALUES
(81, 'Manual', NULL, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(82, 'Folder', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
(83, 'ZIP', NULL, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO galleries_files(gallery_id, file_id, "primary") VALUES (83, 21, 1);
INSERT INTO galleries_images(gallery_id, image_id, cover) VALUES (81, 41, 1), (82, 41, 0);
INSERT INTO scenes_galleries(gallery_id, scene_id) VALUES (81, 31), (83, 32);`)
	require.NoError(t, err)
	identities := make(map[int]string)
	revisions := make(map[int]int)
	for _, id := range []int{81, 82, 83} {
		var value string
		var revision int
		require.NoError(t, raw.QueryRow("SELECT uuid, revision FROM archive_entities WHERE gallery_id=?", id).Scan(&value, &revision))
		identities[id], revisions[id] = value, revision
	}
	require.NoError(t, raw.Close())
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	repo := db.Repository()
	for id, origin := range map[int]models.GalleryOrigin{81: models.GalleryOriginManual, 82: models.GalleryOriginFilesystem, 83: models.GalleryOriginFilesystem} {
		identity := archiveFind(t, repo, models.ArchiveGallery, id)
		require.Equal(t, identities[id], identity.UUID)
		require.Equal(t, revisions[id], identity.Revision, "classifying existing origins is not a user edit")
		require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
			gallery, err := repo.Gallery.Find(ctx, id)
			require.NoError(t, err)
			require.Equal(t, origin, gallery.Origin)
			return nil
		}))
	}
	sourceGalleryMemberships(t, repo, 81, []int{41}, []int{31})
	sourceGalleryMemberships(t, repo, 82, []int{41}, nil)
	sourceGalleryMemberships(t, repo, 83, nil, []int{32})
	raw = openRawDB(t, path)
	defer raw.Close()
	for _, table := range []string{"post_gallery_decisions", "post_gallery_links", "source_gallery_write_context", "gallery_membership_events", "gallery_membership_heads"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table), "migration must not invent source associations or manual intent")
	}
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM galleries_images WHERE gallery_id=81 AND image_id=41 AND cover=1"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestSourceGalleryStartupRejectsUnfinishedWriteContext(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post, _ := singleSourceAlbum(t, repo)
	require.NoError(t, db.Close())
	raw := openRawDB(t, db.DatabasePath())
	_, err := raw.Exec(`INSERT INTO source_gallery_write_context(gallery_uuid, post_uuid, selection_uuid)
SELECT l.gallery_uuid, l.post_uuid, s.decision_uuid FROM post_gallery_links l
JOIN post_attachment_selections s ON s.post_uuid=l.post_uuid WHERE l.post_uuid=?`, post)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.ErrorContains(t, db.Open(db.DatabasePath()), "unfinished source gallery write context")
	// The guard does not try to guess or repair the prior writer's intent.
	raw = openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM source_gallery_write_context"))
	_, err = raw.Exec("DELETE FROM source_gallery_write_context")
	require.NoError(t, err)
	require.NoError(t, db.Open(db.DatabasePath()))
}
