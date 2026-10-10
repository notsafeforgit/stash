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

func createArchiveGallery(t *testing.T, repo models.Repository, title string) *models.Gallery {
	t.Helper()
	gallery := models.NewGallery()
	gallery.Title = title
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		return repo.Gallery.Create(ctx, &models.CreateGalleryInput{Gallery: &gallery})
	}))
	return &gallery
}

func TestArchiveGalleryMigrationPreservesIdentityGraphAndMemberships(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "before-gallery-identities.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+6; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(context.Background(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	_, err = raw.Exec(archiveIdentityFixture)
	require.NoError(t, err)
	_, err = raw.Exec(`INSERT INTO galleries(id, title, created_at, updated_at) VALUES (81, 'Existing manual gallery', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO galleries_images(gallery_id, image_id, cover) VALUES (81, 41, 1);
INSERT INTO scenes_galleries(gallery_id, scene_id) VALUES (81, 31);
INSERT INTO performers_galleries(gallery_id, performer_id) VALUES (81, 71);`)
	require.NoError(t, err)
	var performerID string
	require.NoError(t, raw.QueryRow("SELECT uuid FROM archive_entities WHERE performer_id=71").Scan(&performerID))
	accountID, decisionID, redirectID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, err = raw.Exec(`INSERT INTO source_accounts(uuid, namespace, label) VALUES (?, 'native:reddit', 'Kept account')`, accountID)
	require.NoError(t, err)
	_, err = raw.Exec(`INSERT INTO account_performer_decisions(uuid, account_uuid, revision, state, performer_uuid, origin, reason)
VALUES (?, ?, 1, 'linked', ?, 'migration', 'Kept ownership')`, decisionID, accountID, performerID)
	require.NoError(t, err)
	_, err = raw.Exec(`INSERT INTO account_performer_links(account_uuid, decision_uuid) VALUES (?, ?)`, accountID, decisionID)
	require.NoError(t, err)
	_, err = raw.Exec(`INSERT INTO archive_entities(uuid, kind, state, original_id, redirect_to, retired_at)
VALUES (?, 'performer', 'redirected', 99, ?, CURRENT_TIMESTAMP)`, redirectID, performerID)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	repo := db.Repository()
	require.Equal(t, performerID, archiveFind(t, repo, models.ArchivePerformer, 71).UUID)
	gallery := archiveFind(t, repo, models.ArchiveGallery, 81)
	require.Equal(t, models.ArchiveEntityActive, gallery.State)
	require.Equal(t, 1, gallery.Revision)
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		resolved, err := repo.ArchiveEntity.Resolve(ctx, redirectID)
		require.NoError(t, err)
		require.Equal(t, performerID, resolved.UUID)
		decision, err := repo.SourceAccount.Ownership(ctx, accountID)
		require.NoError(t, err)
		require.Equal(t, decisionID, decision.UUID)
		require.Equal(t, performerID, *decision.PerformerUUID)
		stored, err := repo.Gallery.Find(ctx, 81)
		require.NoError(t, err)
		require.Equal(t, "Existing manual gallery", stored.Title)
		images, err := repo.Gallery.GetImageIDs(ctx, 81)
		require.NoError(t, err)
		require.Equal(t, []int{41}, images)
		scenes, err := repo.Gallery.GetSceneIDs(ctx, 81)
		require.NoError(t, err)
		require.Equal(t, []int{31}, scenes)
		performers, err := repo.Gallery.GetPerformerIDs(ctx, 81)
		require.NoError(t, err)
		require.Equal(t, []int{71}, performers)
		return nil
	}))
	raw = openRawDB(t, path)
	defer raw.Close()
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM gallery_covers c JOIN archive_entities a ON a.uuid=c.media_uuid WHERE c.gallery_id=81 AND a.image_id=41"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='native_gallery_identity_rows'"))
}

func TestArchiveGalleryLifecycleAndMembershipRevisions(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	gallery := createArchiveGallery(t, repo, "Album")
	other := createArchiveGallery(t, repo, "Album")
	first := archiveFind(t, repo, models.ArchiveGallery, gallery.ID)
	require.NotEqual(t, first.UUID, archiveFind(t, repo, models.ArchiveGallery, other.ID).UUID, "titles are not identity")
	revision := first.Revision
	for _, edit := range []func(context.Context) error{
		func(ctx context.Context) error { return repo.Gallery.AddImages(ctx, gallery.ID, 41) },
		func(ctx context.Context) error { return setImageCover(ctx, repo, gallery.ID, 41) },
		func(ctx context.Context) error { return repo.Gallery.AddSceneIDs(ctx, gallery.ID, []int{31}) },
		func(ctx context.Context) error {
			_, err := repo.Gallery.UpdatePartial(ctx, gallery.ID, models.GalleryPartial{Title: models.NewOptionalString("Renamed album")})
			return err
		},
		func(ctx context.Context) error { return repo.Gallery.RemoveImages(ctx, gallery.ID, 41) },
	} {
		require.NoError(t, repo.WithTxn(context.Background(), edit))
		updated := archiveFind(t, repo, models.ArchiveGallery, gallery.ID)
		require.Equal(t, first.UUID, updated.UUID)
		require.Greater(t, updated.Revision, revision)
		revision = updated.Revision
	}
	newID := uuid.NewString()
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, first.UUID, newID, first.Revision)
		return err
	})
	require.ErrorIs(t, err, models.ErrArchiveIdentityConflict, "membership changes invalidate stale gallery reviews")
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, first.UUID, newID, revision)
		return err
	}))
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error { return repo.Gallery.Destroy(ctx, gallery.ID) }))
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		retired, err := repo.ArchiveEntity.Resolve(ctx, first.UUID)
		require.NoError(t, err)
		require.Equal(t, newID, retired.UUID)
		require.Equal(t, models.ArchiveEntityDeleted, retired.State)
		require.Nil(t, retired.LocalID)
		require.Equal(t, gallery.ID, *retired.OriginalID)
		return nil
	}))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec("INSERT INTO galleries(id, title, created_at, updated_at) VALUES (?, 'Reused local ID', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)", gallery.ID)
	require.NoError(t, err)
	require.NotEqual(t, newID, archiveFind(t, repo, models.ArchiveGallery, gallery.ID).UUID)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestArchiveGalleryRedirectRequiresSourceDeletion(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	one := createArchiveGallery(t, repo, "First")
	two := createArchiveGallery(t, repo, "Second")
	source := archiveFind(t, repo, models.ArchiveGallery, one.ID)
	target := archiveFind(t, repo, models.ArchiveGallery, two.ID)
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		return repo.ArchiveEntity.Redirect(ctx, source.UUID, target.UUID, source.Revision)
	})
	require.ErrorContains(t, err, "source record without a current identity")
	require.Equal(t, source, archiveFind(t, repo, models.ArchiveGallery, one.ID))
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		if err := repo.ArchiveEntity.Redirect(ctx, source.UUID, target.UUID, source.Revision); err != nil {
			return err
		}
		return repo.Gallery.Destroy(ctx, one.ID)
	}))
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		resolved, err := repo.ArchiveEntity.Resolve(ctx, source.UUID)
		require.NoError(t, err)
		require.Equal(t, target.UUID, resolved.UUID)
		return nil
	}))
}
