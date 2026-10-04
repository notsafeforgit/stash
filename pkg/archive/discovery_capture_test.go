package archive

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryCaptureKeepsOriginalObservationsAndParentContexts(t *testing.T) {
	for _, fixture := range readDiscoveryPageFixture(t).Pages {
		if len(fixture.Metadata) == 0 {
			continue
		}
		t.Run(fixture.Name, func(t *testing.T) {
			page, err := ParseDiscoveryPage(fixture.Page)
			require.NoError(t, err)
			reference, err := ExtractCapturedPost(fixture.Metadata[0])
			require.NoError(t, err)
			require.NotNil(t, reference)
			saved := models.DiscoveryPage{DiscoveryPageReceipt: models.DiscoveryPageReceipt{ListingUUID: uuid.NewString(), Ordinal: 1,
				JobUUID: uuid.NewString(), ProducerUUID: uuid.NewString(), Fence: 1, Digest: translationDigest(page.Body()), RecordCount: len(page.Records), Complete: page.Complete}, Body: page.Body()}
			post := uuid.NewString()
			records, err := PrepareDiscoveryCaptures(saved, post, *reference)
			require.NoError(t, err)
			require.Len(t, records, len(page.Records))
			captures := make(map[string]*models.SourceCapture)
			for i, record := range records {
				require.Equal(t, i, record.Ordinal)
				original, err := page.Metadata(i)
				require.NoError(t, err)
				restored, err := RestoreCapture(&record.Input.Payload)
				require.NoError(t, err)
				require.Equal(t, original, restored)
				require.Equal(t, post, record.Input.PostUUID)
				capture := &models.SourceCapture{UUID: record.Input.UUID, PostUUID: post, CapturedAt: record.Input.CapturedAt, Payload: &record.Input.Payload}
				for _, link := range record.Input.Contexts {
					require.NoError(t, ValidateCaptureContext(link, capture, captures[link.ParentUUID]))
				}
				captures[capture.UUID] = capture
			}
			replay, err := PrepareDiscoveryCaptures(saved, post, *reference)
			require.NoError(t, err)
			require.Equal(t, records, replay)
			saved.ProducerUUID = uuid.NewString()
			other, err := PrepareDiscoveryCaptures(saved, post, *reference)
			require.NoError(t, err)
			require.NotEqual(t, records[0].Input.UUID, other[0].Input.UUID, "the original producer is capture provenance")
			saved.Body = append(saved.Body, ' ')
			_, err = PrepareDiscoveryCaptures(saved, post, *reference)
			require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
		})
	}
}

func TestDiscoveryCaptureLeavesUnrelatedPostsStaged(t *testing.T) {
	fixture := readDiscoveryPageFixture(t).Pages[1]
	value, err := DecodeJSONObject(fixture.Page, MaxDiscoveryPageBytes)
	require.NoError(t, err)
	records := value["records"].([]any)
	value["records"] = append(records, map[string]any{"kind": "post", "base": 0, "parent": nil, "observed_at": "2026-10-04T01:02:03.000000004Z",
		"patch": map[string]any{"id": "def456", "title": "unrelated caption"}, "removed": []any{}})
	body, err := EncodeSourceJSON(value)
	require.NoError(t, err)
	page, err := ParseDiscoveryPage(body)
	require.NoError(t, err)
	saved := models.DiscoveryPage{DiscoveryPageReceipt: models.DiscoveryPageReceipt{ListingUUID: uuid.NewString(), Ordinal: 1, JobUUID: uuid.NewString(),
		ProducerUUID: uuid.NewString(), Fence: 1, Digest: translationDigest(page.Body()), RecordCount: len(page.Records), Complete: page.Complete}, Body: page.Body()}
	selected, err := PrepareDiscoveryCaptures(saved, uuid.NewString(), models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"})
	require.NoError(t, err)
	require.Len(t, selected, 3)
	_, err = PrepareDiscoveryCaptures(saved, uuid.NewString(), models.SourcePostIdentifier{Namespace: "native:reddit", Value: "missing"})
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
}
