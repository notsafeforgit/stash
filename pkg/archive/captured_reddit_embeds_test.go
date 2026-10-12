package archive

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCapturedRedditEmbeddedMediaContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/reddit-embedded-media-v1.json")
	require.NoError(t, err)
	var fixture struct {
		Cases []struct {
			Name          string
			Source        json.RawMessage
			Entries       []models.SourceAttachmentEntry
			ManifestError bool `json:"manifest_error"`
		}
	}
	require.NoError(t, json.Unmarshal(raw, &fixture))
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			retained, err := RetainSourcePayload(tc.Source)
			require.NoError(t, err)
			for _, payload := range [][]byte{tc.Source, retained} {
				result, err := ExtractCapturedAlbum(payload)
				if tc.ManifestError {
					require.Error(t, err)
					continue
				}
				require.NoError(t, err)
				if tc.Entries == nil {
					require.Nil(t, result)
					continue
				}
				require.NotNil(t, result)
				require.Equal(t, tc.Entries, result.Manifest.Entries)
				require.Equal(t, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "post"}, result.Post)
				require.True(t, result.Manifest.Complete)
				require.Equal(t, len(tc.Entries), *result.Manifest.ExpectedCount)
				require.Equal(t, len(tc.Entries) > 1, result.Manifest.DeclaredAlbum)
			}
		})
	}
}
