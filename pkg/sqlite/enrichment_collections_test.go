package sqlite_test

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestEnrichmentCollectionDiscoveryFollowsCurrentGrantsAndNewCollections(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	root := putMediaRoot(t, f.repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Root", State: "active"}})
	otherRoot := putMediaRoot(t, f.repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Other", State: "active"}})
	_, token, err := f.service.IssueCredential(t.Context(), f.producers[0].UUID, nil, nil, root.UUID)
	require.NoError(t, err)
	add := func(rootID string) *models.SourceCollection {
		collection := putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Feed", Kind: "feed", Namespace: "native:reddit", State: "active", RootUUID: &rootID, PathPrefix: "Feed"}})
		input := f.target.EnrichmentTargetInput
		input.CollectionUUID, input.CollectionRevision = collection.UUID, collection.Revision
		retainEnrichment(t, f.repo, input, models.EnrichmentSchedule{State: "pending"}, f.now)
		return collection
	}
	one, two := add(root.UUID), add(root.UUID)
	add(otherRoot.UUID)
	page := func(token, after string, limit int) []models.EnrichmentCollectionCandidate {
		result, err := f.worker.ReadyCollections(t.Context(), token, strings.Repeat("a", 64), "1.32.15-dev", after, limit)
		require.NoError(t, err)
		return result
	}
	wanted := []string{one.UUID, two.UUID}
	sort.Strings(wanted)
	first := page(token, "", 1)
	require.Equal(t, []models.EnrichmentCollectionCandidate{{UUID: wanted[0]}}, first)
	require.Equal(t, []models.EnrichmentCollectionCandidate{{UUID: wanted[1]}}, page(token, first[0].UUID, 1))
	require.Empty(t, page(token, wanted[1], 1))
	require.Equal(t, []models.EnrichmentCollectionCandidate{{UUID: f.collection.UUID}}, page(f.tokens[0], "", 20), "an unbound grant remains collection-specific")
	definition := one.SourceCollectionDefinition
	definition.RootUUID = &otherRoot.UUID
	putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: one.UUID, ExpectedRevision: one.Revision, Origin: "review", SourceCollectionDefinition: definition})
	require.Equal(t, []models.EnrichmentCollectionCandidate{{UUID: two.UUID}}, page(token, "", 20), "moving a collection cannot reuse its old root grant")
	definition = two.SourceCollectionDefinition
	definition.State = "disabled"
	putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: two.UUID, ExpectedRevision: two.Revision, Origin: "review", SourceCollectionDefinition: definition})
	require.Empty(t, page(token, "", 20))
}

func TestEnrichmentCollectionDiscoveryRetainsRuntimeBackoffAndTerminalBoundaries(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	page := func(policy string) []models.EnrichmentCollectionCandidate {
		result, err := f.worker.ReadyCollections(t.Context(), f.tokens[0], policy, "1.32.15-dev", "", 20)
		require.NoError(t, err)
		return result
	}
	policy := strings.Repeat("a", 64)
	require.Len(t, page(policy), 1, "an unadmitted target discovers its collection")
	job := f.admit(t)
	require.Len(t, page(policy), 1, "an admitted job stays discoverable")
	require.Empty(t, page(strings.Repeat("b", 64)), "a different runtime policy cannot adopt an admitted job")
	running := f.claim(t, job.UUID, 0)
	require.Empty(t, page(policy))
	_, err := f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "source_busy")
	require.NoError(t, err)
	require.Empty(t, page(policy))
	f.now = f.now.Add(5 * time.Minute)
	require.Len(t, page(policy), 1)
	running = f.claim(t, job.UUID, 0)
	_, err = f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "not_found")
	require.NoError(t, err)
	require.Empty(t, page(policy), "terminal work requires review, not automatic readmission")
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		attempts, err := f.repo.ArchiveJob.Attempts(ctx, job.UUID, 0, archive.MaxEnrichmentJobs)
		require.NoError(t, err)
		require.Len(t, attempts, 2, "discovery never claims or recovers work")
		return nil
	}))
}
