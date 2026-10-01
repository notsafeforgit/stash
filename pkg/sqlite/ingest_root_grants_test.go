package sqlite_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestIngestRootGrantDispatchReplayAndHistoryStayWithinRoots(t *testing.T) {
	f := newSourceRunFixture(t)
	namedToken := f.token
	credential, rootToken, err := f.service.IssueCredential(t.Context(), f.producer.UUID, nil, nil, f.root.UUID)
	require.NoError(t, err)
	require.Empty(t, credential.Scopes)
	oldInput := f.request()
	old, err := f.coordinator.Submit(t.Context(), rootToken, oldInput)
	require.NoError(t, err)
	definition := f.collection.SourceCollectionDefinition
	definition.TargetURL += "?later=1"
	definition.PathPrefix = "Later"
	later := putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: definition})
	laterInput := f.request()
	laterInput.CollectionUUID = later.UUID
	second, err := f.coordinator.Submit(t.Context(), rootToken, laterInput)
	require.NoError(t, err)
	_, err = f.coordinator.Submit(t.Context(), namedToken, laterInput)
	require.ErrorIs(t, err, ingest.ErrForbidden)
	ready, err := f.coordinator.Ready(t.Context(), rootToken, f.root.UUID, oldInput.PolicySHA256, 0)
	require.NoError(t, err)
	require.ElementsMatch(t, []models.SourceRunCandidate{{Sequence: old.Sequence, UUID: old.UUID}, {Sequence: second.Sequence, UUID: second.UUID}}, ready)
	ready, err = f.coordinator.Ready(t.Context(), namedToken, f.root.UUID, oldInput.PolicySHA256, 0)
	require.NoError(t, err)
	require.Equal(t, []models.SourceRunCandidate{{Sequence: old.Sequence, UUID: old.UUID}}, ready)

	binding, err := archive.ProbeMediaRoot(t.TempDir())
	require.NoError(t, err)
	otherRoot := putMediaRoot(t, f.repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Other root", State: "active", Binding: binding}})
	definition = f.collection.SourceCollectionDefinition
	definition.RootUUID = &otherRoot.UUID
	f.collection = putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
	replayed, err := f.coordinator.Submit(t.Context(), rootToken, oldInput)
	require.NoError(t, err, "a lost response can replay its original root after the collection moves")
	require.Equal(t, old.UUID, replayed.UUID)
	_, err = f.coordinator.Submit(t.Context(), rootToken, f.request())
	require.ErrorIs(t, err, ingest.ErrForbidden)
	_, otherToken, err := f.service.IssueCredential(t.Context(), f.producer.UUID, nil, nil, otherRoot.UUID)
	require.NoError(t, err)
	_, err = f.coordinator.Submit(t.Context(), otherToken, oldInput)
	require.ErrorIs(t, err, ingest.ErrForbidden)
	newRun, err := f.coordinator.Submit(t.Context(), otherToken, f.request())
	require.NoError(t, err)
	for token, expected := range map[string][]models.SourceRun{rootToken: {*old}, otherToken: {*newRun}} {
		rows, err := f.coordinator.List(t.Context(), token, f.collection.UUID, 0)
		require.NoError(t, err)
		require.Equal(t, expected, rows)
	}
	_, bothToken, err := f.service.IssueCredential(t.Context(), f.producer.UUID, nil, nil, f.root.UUID, otherRoot.UUID)
	require.NoError(t, err)
	rows, err := f.coordinator.List(t.Context(), bothToken, f.collection.UUID, 0)
	require.NoError(t, err)
	require.Equal(t, []models.SourceRun{*old, *newRun}, rows)

	unbound := putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Unbound", Kind: "feed", State: "active", Namespace: "native:reddit", TargetURL: "https://example.invalid/unbound"}})
	input := f.request()
	input.CollectionUUID, input.CollectionRevision, input.Operation = unbound.UUID, 1, "enrich"
	_, err = f.coordinator.Submit(t.Context(), bothToken, input)
	require.ErrorIs(t, err, ingest.ErrForbidden, "root grants never include unbound metadata")
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Equal(t, uint(3), queryUint(t, raw, "SELECT count(*) FROM source_runs"), "rejected admissions roll back all native work")
	_, _, err = f.service.IssueCredential(t.Context(), f.producer.UUID, nil, nil, f.root.UUID, f.root.UUID)
	require.ErrorIs(t, err, ingest.ErrInvalid)
	_, _, err = f.service.IssueCredential(t.Context(), f.producer.UUID, nil, nil, uuid.NewString())
	require.ErrorIs(t, err, models.ErrSourceDefinitionConflict)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.repo.Ingest.RevokeCredential(ctx, credential.UUID) }))
	_, err = f.coordinator.Ready(t.Context(), rootToken, f.root.UUID, oldInput.PolicySHA256, 0)
	require.ErrorIs(t, err, ingest.ErrUnauthorized)
}
