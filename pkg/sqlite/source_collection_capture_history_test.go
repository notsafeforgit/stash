package sqlite_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCollectionCaptureProvenanceBoundsHistoricalMembership(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	capture, _ := attachmentFixture(t, f.repo)
	later := recordSourceTestCapture(t, f.repo, sourceTestCapture(t, capture.PostUUID, 2, "Later profile"))
	definition := f.collection.SourceCollectionDefinition
	definition.Label = "Renamed collection"
	f.collection = putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
	record := func(id string, revision int) {
		t.Helper()
		require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			return f.repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: f.collection.UUID, CollectionRevision: revision, CaptureUUID: id})
		}))
	}
	record(capture.UUID, 1)
	record(later.UUID, 2)
	query := models.CollectionCapture{CollectionUUID: f.collection.UUID, CaptureUUID: capture.UUID, CollectionRevision: 2}
	lookup := func(input models.CollectionCapture) (*models.CollectionCapture, error) {
		var result *models.CollectionCapture
		err := f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			result, err = f.repo.SourceCollection.CaptureProvenance(ctx, input)
			return err
		})
		return result, err
	}
	first, err := lookup(query)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, 1, first.CollectionRevision)
	require.Equal(t, capture.UUID, first.CaptureUUID)
	require.Equal(t, f.collection.UUID, first.CollectionUUID)
	require.False(t, first.CreatedAt.IsZero())
	record(capture.UUID, 2)
	second, err := lookup(query)
	require.NoError(t, err)
	require.NotNil(t, second)
	require.Equal(t, 2, second.CollectionRevision)
	query.CollectionRevision = 1
	original, err := lookup(query)
	require.NoError(t, err)
	require.Equal(t, first, original)
	query.CaptureUUID = later.UUID
	missing, err := lookup(query)
	require.NoError(t, err)
	require.Nil(t, missing, "later membership is not evidence for an earlier definition")
	query.CollectionRevision = 2
	query.CollectionUUID = uuid.NewString()
	missing, err = lookup(query)
	require.NoError(t, err)
	require.Nil(t, missing)
	query.CollectionUUID = strings.ToUpper(f.collection.UUID)
	query.CaptureUUID = strings.ToUpper(capture.UUID)
	canonical, err := lookup(query)
	require.NoError(t, err)
	require.Equal(t, second, canonical)
	for _, mutate := range []func(*models.CollectionCapture){
		func(q *models.CollectionCapture) { q.CollectionUUID = "invalid" },
		func(q *models.CollectionCapture) { q.CaptureUUID = "invalid" },
		func(q *models.CollectionCapture) { q.CollectionRevision = 0 },
		func(q *models.CollectionCapture) { q.CollectionRevision = -1 },
	} {
		invalid := query
		mutate(&invalid)
		_, err := lookup(invalid)
		require.Error(t, err)
	}
}
