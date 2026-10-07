package sqlite_test

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func assertActivityReferences(t *testing.T, repo models.Repository, job *models.ArchiveJob, expected []models.ArchiveActivityReference) {
	t.Helper()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		refs, err := repo.ArchiveActivity.JobReferences(ctx, job.UUID)
		require.NoError(t, err)
		require.ElementsMatch(t, expected, refs)
		current, err := repo.ArchiveJob.Find(ctx, job.UUID)
		require.Equal(t, job, current, "reading activity must not change admission or work state")
		return err
	}))
}

func TestArchiveActivityNormalizedSubjectsRetainOriginalBindings(t *testing.T) {
	t.Run("enrichment", func(t *testing.T) {
		f := newEnrichmentExecutionFixture(t)
		job := f.admit(t)
		original := f.collection.Revision
		definition := f.collection.SourceCollectionDefinition
		definition.Label = "Renamed after admission"
		putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: original, Origin: "review", SourceCollectionDefinition: definition})
		assertActivityReferences(t, f.repo, job, []models.ArchiveActivityReference{
			{Kind: "post", UUID: f.target.PostUUID},
			{Kind: "collection", UUID: f.collection.UUID, Revision: original},
		})
	})
	t.Run("listing", func(t *testing.T) {
		f := newListingFixture(t)
		job := f.admit(t)
		assertActivityReferences(t, f.repo, job, []models.ArchiveActivityReference{
			{Kind: "collection", UUID: f.listing.CollectionUUID, Revision: f.listing.CollectionRevision},
		})
	})
	t.Run("candidate", func(t *testing.T) {
		f := newDetailFixture(t)
		job := f.admit(t)
		assertActivityReferences(t, f.repo, job, []models.ArchiveActivityReference{
			{Kind: "post", UUID: f.target.PostUUID},
			{Kind: "collection", UUID: f.listing.CollectionUUID, Revision: f.listing.CollectionRevision},
		})
	})
	t.Run("shared translation", func(t *testing.T) {
		_, repo := archiveTestDatabase(t)
		service, _ := translationService(t, repo)
		request := retainTranslationRequest(t, repo, "Shared text")
		first := translationJobTarget(t, repo, request.UUID, "first", models.TranslationTargetSchedule{State: "pending"}, service.Durable.Now())
		second := translationJobTarget(t, repo, request.UUID, "second", models.TranslationTargetSchedule{State: "pending"}, service.Durable.Now())
		job, err := service.Admit(t.Context())
		require.NoError(t, err)
		require.NotNil(t, job)
		assertActivityReferences(t, repo, job, []models.ArchiveActivityReference{
			{Kind: "post", UUID: first.PostUUID}, {Kind: "post", UUID: second.PostUUID},
		})
	})
}
