package archive

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCapturedRedditExternalMediaContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/reddit-external-media-v1.json")
	require.NoError(t, err)
	var fixture struct {
		Cases []struct {
			Name          string
			Source        json.RawMessage
			Manifest      *models.SourcePostIdentifier
			Complete      bool
			Kind          string
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
				if tc.Manifest == nil {
					require.Nil(t, result)
					continue
				}
				require.NotNil(t, result)
				require.Equal(t, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "post"}, result.Post)
				require.Equal(t, []models.SourceAttachmentEntry{{Position: 0, Reference: *tc.Manifest, MediaKind: tc.Kind}}, result.Manifest.Entries)
				require.Equal(t, tc.Complete, result.Manifest.Complete)
				require.False(t, result.Manifest.DeclaredAlbum)
				if tc.Complete {
					require.Equal(t, 1, *result.Manifest.ExpectedCount)
				} else {
					require.Nil(t, result.Manifest.ExpectedCount, "one linked clip cannot establish the external gallery count")
				}
			}
		})
	}
}
