package archive

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCapturedRedditImgurAlbums(t *testing.T) {
	raw, err := os.ReadFile("testdata/reddit-imgur-albums-v1.json")
	require.NoError(t, err)
	var fixtures struct {
		Cases []struct {
			Name          string
			Source        json.RawMessage
			ManifestError bool `json:"manifest_error"`
			Entries       []struct {
				Position  int
				Reference models.SourcePostIdentifier
				MediaKind string `json:"media_kind"`
			}
		}
	}
	require.NoError(t, json.Unmarshal(raw, &fixtures))
	for _, tc := range fixtures.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			retained, err := RetainSourcePayload(tc.Source)
			require.NoError(t, err)
			for _, payload := range [][]byte{tc.Source, retained} {
				album, err := ExtractCapturedAlbum(payload)
				if tc.ManifestError {
					require.Error(t, err)
					continue
				}
				require.NoError(t, err)
				if tc.Entries == nil {
					require.Nil(t, album)
					continue
				}
				require.NotNil(t, album)
				require.Equal(t, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "post"}, album.Post)
				require.True(t, album.Manifest.DeclaredAlbum)
				require.True(t, album.Manifest.Complete)
				require.Equal(t, len(tc.Entries), *album.Manifest.ExpectedCount)
				var expected []models.SourceAttachmentEntry
				for _, entry := range tc.Entries {
					expected = append(expected, models.SourceAttachmentEntry{Position: entry.Position, Reference: entry.Reference, MediaKind: entry.MediaKind})
				}
				require.Equal(t, expected, album.Manifest.Entries)
				require.Equal(t, "/album/imgur_media/items", album.EvidencePath)
			}
		})
	}
}
