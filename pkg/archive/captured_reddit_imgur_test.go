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

func TestImgurAlbumCapturesShareMembershipWithoutRepeatingItInFilePatches(t *testing.T) {
	raw, err := os.ReadFile("testdata/reddit-imgur-albums-v1.json")
	require.NoError(t, err)
	var fixture struct {
		Cases []struct{ Source json.RawMessage }
	}
	require.NoError(t, json.Unmarshal(raw, &fixture))
	for _, index := range []int{0, len(fixture.Cases) - 1} {
		first, err := DecodeJSONObject(fixture.Cases[index].Source, MaxSourcePayloadBytes)
		require.NoError(t, err)
		var shared json.RawMessage
		for _, id := range []string{"First12", "Next123"} {
			first["id"], first["url"], first["_url"] = id, "https://i.imgur.com/"+id+".jpg", "https://i.imgur.com/"+id+".jpg"
			encoded, err := EncodeSourceJSON(first)
			require.NoError(t, err)
			payload, err := PrepareRetainedCapture("gallery-dl", "imgur", encoded)
			require.NoError(t, err)
			if shared == nil {
				shared = payload.Shared
			} else {
				require.Equal(t, shared, payload.Shared)
			}
			require.Contains(t, string(payload.Shared), `"imgur_media"`)
			require.NotContains(t, string(payload.Patch), `"imgur_media"`)
			restored, err := RestoreCapture(payload)
			require.NoError(t, err)
			require.Equal(t, []byte(encoded), []byte(restored))
		}
	}
}
