package sqlite_test

import (
	"context"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryCollectionsFollowCurrentGrantsWithoutCreatingWork(t *testing.T) {
	f := newListingFixture(t)
	w := discoveryCoordinator(f)
	root := putMediaRoot(t, f.repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Root", State: "active"}})
	other := putMediaRoot(t, f.repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Other", State: "active"}})
	_, token, err := f.service.IssueCredential(t.Context(), f.producers[0].UUID, nil, nil, root.UUID)
	require.NoError(t, err)
	add := func(rootID string, defined bool) *models.SourceCollection {
		collection := putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Feed", Kind: "feed", Namespace: "native:reddit", State: "active", RootUUID: &rootID, PathPrefix: "."}})
		if defined {
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				input := f.listing.DiscoveryListingInput
				input.UUID, input.CollectionUUID, input.CollectionRevision, input.RootUUID = uuid.NewString(), collection.UUID, collection.Revision, &rootID
				_, err := f.repo.DiscoveryJob.CreateListing(ctx, input, f.now)
				return err
			}))
		}
		return collection
	}
	one, two := add(root.UUID, true), add(root.UUID, true)
	add(root.UUID, false)
	add(other.UUID, true)
	page := func(token, after string, limit int) []models.DiscoveryCollectionCandidate {
		result, err := w.ReadyCollections(t.Context(), token, after, limit)
		require.NoError(t, err)
		return result
	}
	wanted := []string{one.UUID, two.UUID}
	sort.Strings(wanted)
	require.Equal(t, []models.DiscoveryCollectionCandidate{{UUID: wanted[0]}}, page(token, "", 1))
	require.Equal(t, []models.DiscoveryCollectionCandidate{{UUID: wanted[1]}}, page(token, wanted[0], 1))
	require.Empty(t, page(token, wanted[1], 1))
	require.Equal(t, []models.DiscoveryCollectionCandidate{{UUID: f.collection.UUID}}, page(f.tokens[0], "", 20))
	_, unionToken, err := f.service.IssueCredential(t.Context(), f.producers[0].UUID, []models.IngestScope{
		{CollectionUUID: f.collection.UUID}, {CollectionUUID: one.UUID, RootUUID: &root.UUID}}, nil, root.UUID)
	require.NoError(t, err)
	require.Len(t, page(unionToken, "", 20), 3, "overlapping grants must not repeat a collection")
	definition := one.SourceCollectionDefinition
	definition.RootUUID = &other.UUID
	putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: one.UUID, ExpectedRevision: one.Revision, Origin: "review", SourceCollectionDefinition: definition})
	require.Equal(t, []models.DiscoveryCollectionCandidate{{UUID: two.UUID}}, page(token, "", 20), "a historical root cannot authorize the moved collection")
	definition = two.SourceCollectionDefinition
	definition.State = "disabled"
	putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: two.UUID, ExpectedRevision: two.Revision, Origin: "review", SourceCollectionDefinition: definition})
	require.Empty(t, page(token, "", 20))
	three := add(root.UUID, true)
	require.Equal(t, []models.DiscoveryCollectionCandidate{{UUID: three.UUID}}, page(token, "", 20), "a root grant sees later registered collections")
	rootDefinition := root.MediaRootDefinition
	rootDefinition.State = "disabled"
	putMediaRoot(t, f.repo, models.MediaRootInput{UUID: root.UUID, ExpectedRevision: root.Revision, Origin: "review", MediaRootDefinition: rootDefinition})
	require.Empty(t, page(token, "", 20))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		job, err := f.repo.DiscoveryJob.Job(ctx, f.listing.UUID)
		require.NoError(t, err)
		require.Nil(t, job)
		return nil
	}))
}
