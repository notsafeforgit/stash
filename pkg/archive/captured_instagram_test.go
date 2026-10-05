package archive

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestInstagramSourceMembershipSharesProducerContract(t *testing.T) {
	body, err := os.ReadFile("testdata/captured-instagram-media-v1.json")
	require.NoError(t, err)
	var fixtures struct {
		Cases []struct {
			Name   string
			Source json.RawMessage
			Post   *models.SourcePostIdentifier
			Error  bool
			Album  *struct {
				Declared bool
				Complete bool
				Count    int
				Entries  [][]interface{}
			}
		}
	}
	require.NoError(t, json.Unmarshal(body, &fixtures))
	for _, fixture := range fixtures.Cases {
		t.Run(fixture.Name, func(t *testing.T) {
			retained, err := RetainSourcePayload(fixture.Source)
			require.NoError(t, err)
			for _, source := range []json.RawMessage{fixture.Source, retained} {
				post, err := ExtractCapturedPost(source)
				if fixture.Error {
					require.Error(t, err)
					_, err = ExtractCapturedAlbum(source)
					require.Error(t, err)
					continue
				}
				require.NoError(t, err)
				require.Equal(t, fixture.Post, post)
				album, err := ExtractCapturedAlbum(source)
				require.NoError(t, err)
				if fixture.Album == nil {
					require.Nil(t, album)
					continue
				}
				require.Equal(t, *fixture.Post, album.Post)
				require.Equal(t, fixture.Album.Declared, album.Manifest.DeclaredAlbum)
				require.Equal(t, fixture.Album.Complete, album.Manifest.Complete)
				require.Equal(t, fixture.Album.Count, *album.Manifest.ExpectedCount)
				require.Len(t, album.Manifest.Entries, len(fixture.Album.Entries))
				for i, entry := range fixture.Album.Entries {
					require.Equal(t, int(entry[0].(float64)), album.Manifest.Entries[i].Position)
					require.Equal(t, entry[1], album.Manifest.Entries[i].Reference.Value)
					require.Equal(t, entry[2], album.Manifest.Entries[i].MediaKind)
				}
			}
		})
	}
}

func TestInstagramCapturesSharePostBodyAcrossDifferentAttachments(t *testing.T) {
	post := `{"category":"instagram","post_id":"123","sidecar_media_id":"123","post_date":"2026-10-01T12:00:00Z","description":"caption","media_id":"701","date":"2020-01-01T00:00:00Z","shortcode":"first","display_url":"https://media.example/first.jpg","width":10,"height":20,"instagram_media":{"version":1,"post_id":"123","album":true,"items":[{"id":"701","kind":"image"},{"id":"702","kind":"image"}]}}`
	first, err := PrepareRetainedCapture("gallery-dl", "instagram", []byte(post))
	require.NoError(t, err)
	var secondSource map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(post), &secondSource))
	secondSource["media_id"], secondSource["date"] = "702", "2021-01-01T00:00:00Z"
	secondSource["shortcode"], secondSource["display_url"], secondSource["width"] = "second", "https://media.example/second.jpg", 30
	body, err := json.Marshal(secondSource)
	require.NoError(t, err)
	second, err := PrepareRetainedCapture("gallery-dl", "instagram", body)
	require.NoError(t, err)
	require.Equal(t, first.Shared, second.Shared)
	require.NotEqual(t, first.Patch, second.Patch)
	restored, err := RestoreCapture(second)
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(restored))
}

func TestInstagramLegacyCapturesKeepTheirOriginalPartitionForReplay(t *testing.T) {
	legacy := `{"category":"instagram","post_id":"123","media_id":"701","date":"2020-01-01T00:00:00Z","shortcode":"first","display_url":"https://media.example/first.jpg","width":10,"height":20}`
	payload, err := PrepareRetainedCapture("gallery-dl", "instagram", []byte(legacy))
	require.NoError(t, err)
	require.JSONEq(t, `{"category":"instagram","post_id":"123","date":"2020-01-01T00:00:00Z","shortcode":"first","display_url":"https://media.example/first.jpg","width":10,"height":20}`, string(payload.Shared))
	require.JSONEq(t, `{"media_id":"701"}`, string(payload.Patch))
}
