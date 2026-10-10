package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

// Restore the actual pre-108 shape for older migration fixtures. Do not turn
// the production migration into an idempotent downgrade/replay adapter.
func removeGalleryCoverSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeMediaConversionSchema(t, raw)
	if queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='gallery_covers'") == 0 {
		return
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM gallery_covers c JOIN archive_entities a ON a.uuid=c.media_uuid WHERE a.kind!='image'"), "the old schema cannot represent a scene cover")
	rows, err := raw.Query("SELECT name,sql FROM sqlite_schema WHERE type='trigger' AND (name GLOB 'gallery_cover_*' OR tbl_name='galleries_images') ORDER BY name")
	require.NoError(t, err)
	defer rows.Close()
	var names, retained []string
	for rows.Next() {
		var name, definition string
		require.NoError(t, rows.Scan(&name, &definition))
		names = append(names, name)
		if !strings.HasPrefix(name, "gallery_cover_") {
			retained = append(retained, definition)
		}
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	tx, err := raw.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	for _, name := range names {
		_, err = tx.Exec(`DROP TRIGGER "` + strings.ReplaceAll(name, `"`, `""`) + `"`)
		require.NoError(t, err)
	}
	_, err = tx.Exec(`ALTER TABLE galleries_images ADD COLUMN cover BOOLEAN NOT NULL DEFAULT 0;
UPDATE galleries_images SET cover=1 WHERE EXISTS (
 SELECT 1 FROM gallery_covers c JOIN archive_entities a ON a.uuid=c.media_uuid
 WHERE c.gallery_id=galleries_images.gallery_id AND a.image_id=galleries_images.image_id);
DROP TABLE gallery_covers;
CREATE UNIQUE INDEX index_galleries_images_gallery_id_cover ON galleries_images(gallery_id,cover) WHERE cover=1;
DELETE FROM native_migration_history WHERE version=1000108;`)
	require.NoError(t, err)
	for _, definition := range retained {
		_, err = tx.Exec(definition)
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())
}

func setImageCover(ctx context.Context, repo models.Repository, galleryID, imageID int) error {
	identity, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveImage, imageID)
	if err != nil {
		return err
	}
	if identity == nil {
		return models.ErrArchiveIdentityConflict
	}
	return repo.Gallery.SetCover(ctx, galleryID, identity.UUID)
}

func TestGalleryCoverSelectsEitherMemberAndRejectsUnrelatedIdentities(t *testing.T) {
	db, _ := archiveTestDatabase(t)
	repo := db.Repository()
	g := createArchiveGallery(t, repo, "Mixed album")
	img := archiveFind(t, repo, models.ArchiveImage, 41)
	scene := archiveFind(t, repo, models.ArchiveScene, 31)
	other := archiveFind(t, repo, models.ArchiveScene, 32)
	performer := archiveFind(t, repo, models.ArchivePerformer, 71)
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		require.NoError(t, repo.Gallery.AddImages(ctx, g.ID, 41))
		require.NoError(t, repo.Gallery.AddSceneIDs(ctx, g.ID, []int{31}))
		return repo.Gallery.SetCover(ctx, g.ID, img.UUID)
	}))
	for _, media := range []*models.ArchiveEntity{scene, img} {
		require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
			return repo.Gallery.SetCover(ctx, g.ID, media.UUID)
		}))
		require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
			stored, err := repo.Gallery.Find(ctx, g.ID)
			require.NoError(t, err)
			require.Equal(t, media.UUID, *stored.CoverMediaUUID)
			cover, err := gallery.FindCover(ctx, repo, stored, "cover")
			require.NoError(t, err)
			if media.Kind == models.ArchiveScene {
				require.Nil(t, cover.Image)
				require.Equal(t, 31, cover.Scene.ID)
			} else {
				require.Nil(t, cover.Scene)
				require.Equal(t, 41, cover.Image.ID)
			}
			return nil
		}))
	}
	for _, candidate := range []string{other.UUID, performer.UUID, uuid.NewString()} {
		require.Error(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
			return repo.Gallery.SetCover(ctx, g.ID, candidate)
		}))
	}
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		stored, err := repo.Gallery.Find(ctx, g.ID)
		require.NoError(t, err)
		require.Equal(t, img.UUID, *stored.CoverMediaUUID, "invalid replacement must not clear the existing choice")
		return repo.Gallery.ResetCover(ctx, g.ID)
	}))
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		stored, err := repo.Gallery.Find(ctx, g.ID)
		require.NoError(t, err)
		require.Nil(t, stored.CoverMediaUUID)
		return nil
	}))
}

func TestGalleryCoverSurvivesReaffirmationAndClearsOnlyWhenMemberLeaves(t *testing.T) {
	db, _ := archiveTestDatabase(t)
	repo := db.Repository()
	g := createArchiveGallery(t, repo, "Video album")
	scene := archiveFind(t, repo, models.ArchiveScene, 31)
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		require.NoError(t, repo.Gallery.AddSceneIDs(ctx, g.ID, []int{31, 32}))
		require.NoError(t, repo.Gallery.SetCover(ctx, g.ID, scene.UUID))
		_, err := repo.Gallery.UpdatePartial(ctx, g.ID, models.GalleryPartial{SceneIDs: &models.UpdateIDs{
			IDs: []int{31}, Mode: models.RelationshipUpdateModeSet,
		}})
		require.NoError(t, err)
		stored, err := repo.Gallery.Find(ctx, g.ID)
		require.NoError(t, err)
		require.Equal(t, scene.UUID, *stored.CoverMediaUUID)
		current, err := repo.ArchiveEntity.Find(ctx, scene.UUID)
		require.NoError(t, err)
		adopted, err := repo.ArchiveEntity.AdoptUUID(ctx, scene.UUID, uuid.NewString(), current.Revision)
		require.NoError(t, err)
		stored, err = repo.Gallery.Find(ctx, g.ID)
		require.NoError(t, err)
		require.Equal(t, adopted.UUID, *stored.CoverMediaUUID)
		_, err = repo.Gallery.UpdatePartial(ctx, g.ID, models.GalleryPartial{SceneIDs: &models.UpdateIDs{
			IDs: []int{32}, Mode: models.RelationshipUpdateModeSet,
		}})
		require.NoError(t, err)
		stored, err = repo.Gallery.Find(ctx, g.ID)
		require.NoError(t, err)
		require.Nil(t, stored.CoverMediaUUID)
		cover, err := gallery.FindCover(ctx, repo, stored, "cover")
		require.NoError(t, err)
		require.Equal(t, 32, cover.Scene.ID, "video-only galleries have a default cover")
		return nil
	}))
}

func TestGalleryCoverMigrationPreservesSelectionWithoutChangingGalleryRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gallery-covers.sqlite")
	// Seed before collection decisions existed, then promote to the exact
	// previous release so its normal migrations finish all metadata choices.
	buildLegacyDatabase(t, path, sqlite.NativeSchemaBaseline+11, false)
	raw := openRawDB(t, path)
	_, err := raw.Exec(archiveIdentityFixture + `
INSERT INTO galleries(id,title,created_at,updated_at) VALUES(81,'Existing cover',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO galleries_images(gallery_id,image_id,cover) VALUES(81,41,1);
INSERT INTO scenes_galleries(gallery_id,scene_id) VALUES(81,31);`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	prior := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(prior.Open(path), &needed))
	migrator, err := sqlite.NewMigrator(prior)
	require.NoError(t, err)
	for current := migrator.CurrentSchemaVersion(); current < sqlite.NativeSchemaBaseline+107; current = migrator.CurrentSchemaVersion() {
		require.NoError(t, migrator.RunMigration(t.Context(), migrator.GetNextMigrationVersion(current)))
	}
	migrator.Close()
	raw = openRawDB(t, path)
	var galleryUUID, imageUUID, updatedAt string
	var revision int
	require.NoError(t, raw.QueryRow(`SELECT a.uuid,a.revision,g.updated_at FROM archive_entities a JOIN galleries g ON g.id=a.gallery_id WHERE g.id=81`).Scan(&galleryUUID, &revision, &updatedAt))
	require.NoError(t, raw.QueryRow("SELECT uuid FROM archive_entities WHERE image_id=41").Scan(&imageUUID))
	require.NoError(t, raw.Close())
	db := sqlite.NewDatabase()
	require.True(t, errors.As(db.Open(path), &needed))
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(path), "the migrated schema must reopen without a repair")
	defer db.Close()
	repo := db.Repository()
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		g, err := repo.Gallery.Find(ctx, 81)
		require.NoError(t, err)
		require.Equal(t, imageUUID, *g.CoverMediaUUID)
		identity, err := repo.ArchiveEntity.Find(ctx, galleryUUID)
		require.NoError(t, err)
		require.Equal(t, revision, identity.Revision)
		return nil
	}))
	raw = openRawDB(t, path)
	defer raw.Close()
	var after string
	require.NoError(t, raw.QueryRow("SELECT updated_at FROM galleries WHERE id=81").Scan(&after))
	require.Equal(t, updatedAt, after)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_table_info('galleries_images') WHERE name='cover'"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM native_migration_history WHERE version=1000108"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestGalleryCoverFollowsMergeOnlyWhileDestinationIsAMember(t *testing.T) {
	for _, member := range []bool{false, true} {
		t.Run(map[bool]string{false: "removed", true: "retained"}[member], func(t *testing.T) {
			db, _ := archiveTestDatabase(t)
			repo := db.Repository()
			g := createArchiveGallery(t, repo, "Merged video")
			source := archiveFind(t, repo, models.ArchiveScene, 31)
			destination := archiveFind(t, repo, models.ArchiveScene, 32)
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				members := []int{31}
				if member {
					members = append(members, 32)
				}
				require.NoError(t, repo.Gallery.AddSceneIDs(ctx, g.ID, members))
				require.NoError(t, repo.Gallery.SetCover(ctx, g.ID, source.UUID))
				current, err := repo.ArchiveEntity.Find(ctx, source.UUID)
				require.NoError(t, err)
				require.NoError(t, repo.ArchiveEntity.Redirect(ctx, source.UUID, destination.UUID, current.Revision))
				return repo.Scene.Destroy(ctx, 31)
			}))
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				stored, err := repo.Gallery.Find(ctx, g.ID)
				require.NoError(t, err)
				if member {
					require.Equal(t, destination.UUID, *stored.CoverMediaUUID)
				} else {
					require.Nil(t, stored.CoverMediaUUID)
				}
				return nil
			}))
		})
	}
}

func TestSourceGalleryProtectsSelectedSceneUntilCoverIsReset(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "scene-cover"}, "")
	manifest := models.SourceAttachmentManifestInput{Complete: true, Entries: []models.SourceAttachmentEntry{
		sourceAttachmentEntry(0, "image"), sourceAttachmentEntry(1, "scene"),
	}}
	selection := selectAlbum(t, repo, post.UUID, manifest)
	chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, 41)
	chooseAlbumMedia(t, repo, selection.Entries[1].Attachment.UUID, models.ArchiveScene, 31)
	result := syncSourceGallery(t, repo, post.UUID)
	scene := archiveFind(t, repo, models.ArchiveScene, 31)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return repo.Gallery.SetCover(ctx, *result.GalleryID, scene.UUID)
	}))
	selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true, DeclaredAlbum: true})
	require.Len(t, syncSourceGallery(t, repo, post.UUID).Removed, 1)
	sourceGalleryMemberships(t, repo, *result.GalleryID, nil, []int{31})
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return repo.Gallery.ResetCover(ctx, *result.GalleryID)
	}))
	require.Len(t, syncSourceGallery(t, repo, post.UUID).Removed, 1)
	sourceGalleryMemberships(t, repo, *result.GalleryID, nil, nil)
}
