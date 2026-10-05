package sqlite_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestMetadataPolicyDraftNeverPublishesAndPinsReviewedRevisions(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	performer := archiveFind(t, f.repo, models.ArchivePerformer, 71)
	definition := models.MetadataPolicyDefinition{Enabled: true, ApplyToScans: true,
		Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{models.ArchiveScene: {OnCreate: true, FilenameTitleFallback: true, Mappings: map[string]models.MetadataMapping{
			"performers": {Value: json.RawMessage(`["` + performer.UUID + `"]`)},
		}}}}
	input := metadata.Input{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, EntityUUID: f.entity.UUID, RelativePath: "Purchased.mp4", Created: true}
	preview := func() (*metadata.DraftPreview, error) {
		var result *metadata.DraftPreview
		err := f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			result, err = (metadata.Service{Repo: f.repo}).PreviewDraft(ctx, input, definition)
			return err
		})
		return result, err
	}
	one, err := preview()
	require.NoError(t, err)
	require.Equal(t, "ready", one.State)
	require.Len(t, one.Changes, 2)
	encoded, err := json.Marshal(one)
	require.NoError(t, err)
	var document map[string]interface{}
	require.NoError(t, json.Unmarshal(encoded, &document))
	require.NotContains(t, document, "digest")
	require.NotContains(t, one.Data, "settings")
	require.NotContains(t, one.Data, "definition")
	require.Empty(t, metadataHistory(t, f.repo, f.entity.UUID, "title"))
	require.Empty(t, metadataHistory(t, f.repo, f.entity.UUID, "performers"))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		policy, err := f.repo.MetadataPolicy.Find(ctx, f.collection.UUID)
		require.NoError(t, err)
		require.Nil(t, policy)
		intakes, err := f.repo.SourceCollection.MediaIntake(ctx, f.collection.UUID, "", 25)
		require.NoError(t, err)
		require.Empty(t, intakes)
		return nil
	}))
	input.Created = false
	existing, err := preview()
	require.NoError(t, err)
	require.Equal(t, "not_enabled_for_event", existing.State)
	require.Len(t, existing.Changes, 2, "inspect candidates while explaining why this event would not apply them")
	input.Created = true
	policy := f.put(t, 0, models.MetadataPolicyRule{OnCreate: true, Mappings: map[string]models.MetadataMapping{"title": {Value: json.RawMessage(`"Saved rule"`)}}})
	_, err = preview()
	require.ErrorIs(t, err, models.ErrMetadataPolicyConflict)
	input.PolicyRevision = policy.Revision
	_, err = preview()
	require.NoError(t, err)
	require.JSONEq(t, `"Saved rule"`, string(policyChange(t, policyPreview(t, f.repo, input), "title").Value), "draft must not replace the enabled definition")
	definition.Rules[models.ArchiveImage] = models.MetadataPolicyRule{Mappings: map[string]models.MetadataMapping{"rating100": {Value: json.RawMessage(`101`)}}}
	_, err = preview()
	require.ErrorIs(t, err, models.ErrMetadataPolicyInvalid, "validate constants even for the other sample kind")
	delete(definition.Rules, models.ArchiveImage)
	rule := definition.Rules[models.ArchiveScene]
	rule.Mappings["performers"] = models.MetadataMapping{Value: json.RawMessage(`["Shared name"]`), PerformerNames: true}
	definition.Rules[models.ArchiveScene] = rule
	conflict, err := preview()
	require.NoError(t, err)
	require.Equal(t, "ambiguous", conflict.Changes[0].Names[0].Status)
	require.Len(t, conflict.Changes[0].Names[0].Candidates, 2)
	input.CollectionRevision++
	stale, err := preview()
	require.NoError(t, err)
	require.Equal(t, "collection_changed", stale.State)
	require.Empty(t, stale.Changes)
}
