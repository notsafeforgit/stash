package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSourceGalleryPerformerReconciliationRepairsOlderGalleries(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	older := consolidationGallery(t, repo, "Older image post", []int{41}, nil)
	mixed := consolidationGallery(t, repo, "Combined posts", []int{41}, []int{31})
	protected := consolidationGallery(t, repo, "Explicitly cleared", []int{41}, nil)
	manual := createArchiveGallery(t, repo, "Manual")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.Gallery.UpdatePartial(ctx, *protected.LocalID, models.GalleryPartial{PerformerIDs: &models.UpdateIDs{Mode: models.RelationshipUpdateModeSet, IDs: []int{}}})
		if err != nil {
			return err
		}
		return repo.Gallery.AddImages(ctx, manual.ID, 41)
	}))
	manualIdentity := archiveFind(t, repo, models.ArchiveGallery, manual.ID)
	// Reproduce the pre-inheritance state: tagged media whose existing galleries
	// never had an edit to enqueue them. Use the managed transaction for all other
	// metadata bookkeeping, but omit the new gallery work set in this fixture.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `INSERT INTO performers_images(image_id,performer_id) VALUES(41,71),(41,72);
DELETE FROM source_gallery_performer_pending`, nil)
		return err
	}))
	assertSourceGalleryPerformers(t, repo, older)
	assertSourceGalleryPerformers(t, repo, mixed, 71)
	ids := []string{older.UUID, mixed.UUID, protected.UUID, manualIdentity.UUID, uuid.NewString()}
	apply := func() []models.SourceGalleryAttributionResult {
		var result []models.SourceGalleryAttributionResult
		require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			var err error
			result, err = repo.SourceGallery.ReconcilePerformers(ctx, ids)
			return err
		}))
		return result
	}
	result := apply()
	require.Equal(t, "updated", result[0].Status)
	require.Equal(t, "updated", result[1].Status)
	require.Equal(t, "protected", result[2].Status)
	require.Equal(t, "not_source", result[3].Status)
	require.Equal(t, "not_source", result[4].Status)
	assertSourceGalleryPerformers(t, repo, older, 71, 72)
	assertSourceGalleryPerformers(t, repo, mixed, 71, 72)
	assertSourceGalleryPerformers(t, repo, protected)
	assertSourceGalleryPerformers(t, repo, manualIdentity)
	sourceGalleryMemberships(t, repo, *mixed.LocalID, []int{41}, []int{31})
	state := metadataState(t, repo, older.UUID, "performers")
	require.False(t, state.Protected)
	require.Equal(t, "inherit", state.Mode)
	require.Equal(t, "policy", state.Origin)
	history := metadataHistory(t, repo, older.UUID, "performers")
	result = apply()
	require.Equal(t, "unchanged", result[0].Status)
	require.Equal(t, history, metadataHistory(t, repo, older.UUID, "performers"))
	// The repaired relationship keeps following later member edits.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Image.UpdatePerformers(ctx, 41, []int{71}) }))
	assertSourceGalleryPerformers(t, repo, older, 71)
	assertSourceGalleryPerformers(t, repo, mixed, 71)
	assertSourceGalleryPerformers(t, repo, protected)
	for _, invalid := range [][]string{nil, {older.UUID, older.UUID}, {"invalid"}, make([]string, 101)} {
		require.Error(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.SourceGallery.ReconcilePerformers(ctx, invalid)
			return err
		}))
	}
}

func assertSourceGalleryPerformers(t *testing.T, repo models.Repository, gallery *models.ArchiveEntity, expected ...int) {
	t.Helper()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		ids, err := repo.Gallery.GetPerformerIDs(ctx, *gallery.LocalID)
		require.NoError(t, err)
		require.ElementsMatch(t, expected, ids)
		return nil
	}))
}

func TestSourceGalleryPerformersMixedMembersAndEdits(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	attachmentSQL(t, db, archiveMetadataFixture)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return repo.Image.UpdatePerformers(ctx, 41, []int{72})
	}))
	g := consolidationGallery(t, repo, "Mixed", []int{41}, []int{31})
	assertSourceGalleryPerformers(t, repo, g, 71, 72)
	state := metadataState(t, repo, g.UUID, "performers")
	require.False(t, state.Protected)
	require.Equal(t, "inherit", state.Mode)
	before := metadataHistory(t, repo, g.UUID, "performers")

	// A bulk change coalesces intermediate states and deduplicates performers.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := repo.Image.UpdatePerformers(ctx, 41, []int{71, 72}); err != nil {
			return err
		}
		if err := repo.Image.UpdatePerformers(ctx, 41, []int{72}); err != nil {
			return err
		}
		_, err := repo.Scene.UpdatePartial(ctx, 31, models.ScenePartial{PerformerIDs: &models.UpdateIDs{Mode: models.RelationshipUpdateModeSet, IDs: []int{72}}})
		return err
	}))
	assertSourceGalleryPerformers(t, repo, g, 72)
	require.Len(t, metadataHistory(t, repo, g.UUID, "performers"), len(before)+1)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Image.UpdatePerformers(ctx, 41, []int{72}) }))
	require.Len(t, metadataHistory(t, repo, g.UUID, "performers"), len(before)+1, "unchanged union creates no extra decision")

	// The same transaction rolls back both the media edit and its inheritance.
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := repo.Image.UpdatePerformers(ctx, 41, []int{71}); err != nil {
			return err
		}
		return errors.New("rollback")
	})
	require.Error(t, err)
	assertSourceGalleryPerformers(t, repo, g, 72)

	// Removing the last member for a performer removes that derived performer.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := repo.Gallery.RemoveImages(ctx, *g.LocalID, 41); err != nil {
			return err
		}
		_, err := repo.Scene.UpdatePartial(ctx, 31, models.ScenePartial{GalleryIDs: &models.UpdateIDs{Mode: models.RelationshipUpdateModeRemove, IDs: []int{*g.LocalID}}})
		return err
	}))
	assertSourceGalleryPerformers(t, repo, g)
	require.False(t, metadataState(t, repo, g.UUID, "performers").Protected)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_gallery_performer_pending"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestSourceGalleryPerformersPreserveExplicitChoicesAndManualGalleries(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	attachmentSQL(t, db, archiveMetadataFixture)
	g := consolidationGallery(t, repo, "Source", nil, []int{31})
	assertSourceGalleryPerformers(t, repo, g, 71)
	manual := createArchiveGallery(t, repo, "Manual")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := repo.Gallery.AddSceneIDs(ctx, manual.ID, []int{31}); err != nil {
			return err
		}
		_, err := repo.Gallery.UpdatePartial(ctx, *g.LocalID, models.GalleryPartial{PerformerIDs: &models.UpdateIDs{Mode: models.RelationshipUpdateModeSet, IDs: []int{}}})
		return err
	}))
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.Scene.UpdatePartial(ctx, 31, models.ScenePartial{PerformerIDs: &models.UpdateIDs{Mode: models.RelationshipUpdateModeSet, IDs: []int{72}}})
		return err
	}))
	assertSourceGalleryPerformers(t, repo, g)
	require.Equal(t, "clear", metadataState(t, repo, g.UUID, "performers").Mode)
	assertSourceGalleryPerformers(t, repo, archiveFind(t, repo, models.ArchiveGallery, manual.ID))
	// An explicit selection wins even when a membership edit happens with it.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.Gallery.UpdatePartial(ctx, *g.LocalID, models.GalleryPartial{PerformerIDs: &models.UpdateIDs{Mode: models.RelationshipUpdateModeSet, IDs: []int{71}}})
		if err != nil {
			return err
		}
		return repo.Gallery.AddImages(ctx, *g.LocalID, 41)
	}))
	assertSourceGalleryPerformers(t, repo, g, 71)
	input := metadataInput(metadataState(t, repo, g.UUID, "performers"), "inherit", "review", "[]")
	_, err := applyMetadata(repo, input, false)
	require.NoError(t, err)
	assertSourceGalleryPerformers(t, repo, g, 72)
	require.False(t, metadataState(t, repo, g.UUID, "performers").Protected)
}

func TestSourceGalleryPerformersSurviveMergeRemovalAndAlbumConsolidation(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	attachmentSQL(t, db, archiveMetadataFixture)
	a := consolidationGallery(t, repo, "One", nil, []int{31})
	b := consolidationGallery(t, repo, "Two", []int{41}, []int{31})
	consolidationApply(t, repo, b.UUID, consolidationPreview(t, repo, b.UUID))
	assertSourceGalleryPerformers(t, repo, a, 71)
	require.False(t, metadataState(t, repo, a.UUID, "performers").Protected)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Performer.Merge(ctx, []int{71}, 72) }))
	assertSourceGalleryPerformers(t, repo, a, 72)
	require.False(t, metadataState(t, repo, a.UUID, "performers").Protected)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Performer.Destroy(ctx, 72) }))
	assertSourceGalleryPerformers(t, repo, a)
	require.False(t, metadataState(t, repo, a.UUID, "performers").Protected)
	// Inheritance remains active after removal, rather than becoming a clear.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		p := models.NewPerformer()
		p.Name = "Later performer"
		if err := repo.Performer.Create(ctx, &models.CreatePerformerInput{Performer: &p}); err != nil {
			return err
		}
		return repo.Image.UpdatePerformers(ctx, 41, []int{p.ID})
	}))
	require.NotEqual(t, "[]", string(metadataState(t, repo, a.UUID, "performers").Value))
}
