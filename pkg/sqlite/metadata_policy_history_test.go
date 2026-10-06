package sqlite_test

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestMetadataPolicyRetainsCaptureHistoryAfterCollectionEdit(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	capture, attachment := attachmentFixture(t, f.repo)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return f.repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, CaptureUUID: capture.UUID})
	}))
	require.NoError(t, applyMediaChoice(f.repo, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision,
		State: "linked", MediaUUID: f.entity.UUID, ExpectedMediaRevision: f.entity.Revision, Origin: "review"}))
	rule := models.MetadataPolicyRule{OnCreate: true, OnExisting: true, Mappings: map[string]models.MetadataMapping{
		"title": {JQ: `.source.metadata.title // empty`},
	}}
	policy := f.put(t, 0, rule)
	input := f.input(policy)
	input.Source = &metadata.Source{CaptureUUID: capture.UUID, AttachmentUUID: attachment.UUID}
	before := policyPreview(t, f.repo, input)
	history := func() []models.CollectionCapture {
		t.Helper()
		var rows []models.CollectionCapture
		require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			rows, err = f.repo.SourceCollection.Captures(ctx, f.collection.UUID, nil, 100)
			return err
		}))
		return rows
	}
	originalHistory := history()
	definition := f.collection.SourceCollectionDefinition
	definition.Label = "Renamed collection"
	f.collection = putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
	require.Equal(t, "collection_changed", policyPreview(t, f.repo, input).State)
	_, err := applyPolicy(f.repo, input, before.Digest)
	require.ErrorIs(t, err, models.ErrMetadataPolicyConflict, "a reviewed old definition cannot authorize a later edit")
	input.CollectionRevision = f.collection.Revision
	require.Equal(t, "collection_changed", policyPreview(t, f.repo, input).State, "the policy must be reviewed for the changed collection")
	policy = f.put(t, policy.Revision, rule)
	input.PolicyRevision, input.Created = policy.Revision, false
	scope := models.MetadataPolicySampleScope{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, EntityUUID: f.entity.UUID}
	lookup := func(scope models.MetadataPolicySampleScope, after *models.MetadataPolicySourceCursor) ([]models.MetadataPolicySampleSource, error) {
		var rows []models.MetadataPolicySampleSource
		err := f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			rows, err = f.repo.MetadataPolicy.SampleSources(ctx, scope, after, 1)
			return err
		})
		return rows, err
	}
	t.Run("samples retain older captures", func(t *testing.T) {
		rows, err := lookup(scope, nil)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, capture.UUID, rows[0].CaptureUUID)
		require.Equal(t, attachment.UUID, rows[0].AttachmentUUID)
		stale := scope
		stale.CollectionRevision--
		_, err = lookup(stale, nil)
		require.ErrorIs(t, err, models.ErrMetadataPolicyConflict)
	})
	t.Run("current policy uses original evidence", func(t *testing.T) {
		preview := policyPreview(t, f.repo, input)
		require.Equal(t, "ready", preview.State)
		require.JSONEq(t, `"Shared title"`, string(policyChange(t, preview, "title").Value))
		applied, err := applyPolicy(f.repo, input, preview.Digest)
		require.NoError(t, err)
		require.Equal(t, []string{"title"}, applied.AppliedFields())
		state := metadataState(t, f.repo, f.entity.UUID, "title")
		require.Equal(t, capture.UUID, *state.Decision.CaptureUUID)
		require.Equal(t, policy.MetadataPolicyRef, *state.Decision.Policy)
	})
	require.Equal(t, originalHistory, history(), "reading and applying a rule must not relabel the original capture")
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		present, err := f.repo.SourceCollection.HasCapture(ctx, models.CollectionCapture{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, CaptureUUID: capture.UUID})
		require.NoError(t, err)
		require.False(t, present, "producer admission still requires an exact recorded collection revision")
		return nil
	}))
	// A later, explicit membership must not duplicate a selectable source.
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return f.repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, CaptureUUID: capture.UUID})
	}))
	rows, err := lookup(scope, nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	next, err := lookup(scope, &rows[0].MetadataPolicySourceCursor)
	require.NoError(t, err)
	require.Empty(t, next)
	require.Equal(t, originalHistory[0], history()[0])
	other := putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: definition})
	otherFixture := f
	otherFixture.collection = other
	otherPolicy := otherFixture.put(t, 0, rule)
	otherInput := otherFixture.input(otherPolicy)
	otherInput.Source = input.Source
	_, err = applyPolicy(f.repo, otherInput, "")
	require.ErrorIs(t, err, models.ErrMetadataPolicyConflict, "sharing a folder does not establish membership in another collection")
	otherScope := scope
	otherScope.CollectionUUID, otherScope.CollectionRevision = other.UUID, other.Revision
	rows, err = lookup(otherScope, nil)
	require.NoError(t, err)
	require.Empty(t, rows)
	current := findAttachment(t, f.repo, attachment.UUID)
	require.NoError(t, applyMediaChoice(f.repo, models.AttachmentMediaDecisionInput{AttachmentUUID: current.UUID, ExpectedAttachmentRevision: current.Revision, State: "unlinked", Origin: "review"}))
	rows, err = lookup(scope, nil)
	require.NoError(t, err)
	require.Empty(t, rows, "capture history must not undo an explicit unlink")
	_, err = applyPolicy(f.repo, input, "")
	require.ErrorIs(t, err, models.ErrMetadataPolicyConflict)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.Len(t, history(), 2)
	require.Equal(t, originalHistory[0], history()[0])
}
