package archive

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCapturedWebMediaSharedContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/captured-web-media-v1.json")
	require.NoError(t, err)
	var document struct {
		Cases []struct {
			Name     string
			Source   json.RawMessage
			Post     models.SourcePostIdentifier
			Items    []models.SourcePostIdentifier
			Complete bool
			Album    bool
			Error    bool
		}
	}
	require.NoError(t, json.Unmarshal(raw, &document))
	for _, fixture := range document.Cases {
		t.Run(fixture.Name, func(t *testing.T) {
			retained, err := RetainSourcePayload(fixture.Source)
			require.NoError(t, err)
			for _, raw := range []json.RawMessage{fixture.Source, retained} {
				album, err := ExtractCapturedAlbum(raw)
				if fixture.Error {
					require.Error(t, err)
					continue
				}
				require.NoError(t, err)
				require.NotNil(t, album)
				post, err := ExtractCapturedPost(raw)
				require.NoError(t, err)
				require.Equal(t, fixture.Post, *post)
				require.Equal(t, fixture.Post, album.Post)
				require.Equal(t, fixture.Complete, album.Manifest.Complete)
				require.Equal(t, fixture.Album, album.Manifest.DeclaredAlbum)
				if fixture.Complete {
					require.Equal(t, len(fixture.Items), *album.Manifest.ExpectedCount)
				} else {
					require.Nil(t, album.Manifest.ExpectedCount)
				}
				require.Len(t, album.Manifest.Entries, len(fixture.Items))
				for position, ref := range fixture.Items {
					require.Equal(t, position, album.Manifest.Entries[position].Position)
					require.Equal(t, ref, album.Manifest.Entries[position].Reference)
				}
			}
		})
	}
}

func TestWebMediaSharesPostBodyAndKeepsOriginalCapturePartition(t *testing.T) {
	raw, err := os.ReadFile("testdata/captured-web-media-v1.json")
	require.NoError(t, err)
	var document struct {
		Cases []struct{ Source json.RawMessage }
	}
	require.NoError(t, json.Unmarshal(raw, &document))
	value, err := DecodeJSONObject(document.Cases[0].Source, MaxSourcePayloadBytes)
	require.NoError(t, err)
	value["photo"] = sourceObject{"url": "https://64.media.tumblr.com/first.jpg"}
	firstRaw, err := EncodeSourceJSON(value)
	require.NoError(t, err)
	first, err := PrepareRetainedCapture("gallery-dl", "tumblr", firstRaw)
	require.NoError(t, err)
	value["photo"] = sourceObject{"url": "https://64.media.tumblr.com/second.jpg"}
	value["web_media_url"] = "https://64.media.tumblr.com/second.jpg"
	secondRaw, err := EncodeSourceJSON(value)
	require.NoError(t, err)
	second, err := PrepareRetainedCapture("gallery-dl", "tumblr", secondRaw)
	require.NoError(t, err)
	require.Equal(t, first.Shared, second.Shared)
	require.NotEqual(t, first.Patch, second.Patch)
	restored, err := RestoreCapture(second)
	require.NoError(t, err)
	require.JSONEq(t, string(secondRaw), string(restored))
	delete(value, "web_media")
	legacyRaw, err := EncodeSourceJSON(value)
	require.NoError(t, err)
	legacy, err := PrepareRetainedCapture("gallery-dl", "tumblr", legacyRaw)
	require.NoError(t, err)
	var shared sourceObject
	require.NoError(t, json.Unmarshal(legacy.Shared, &shared))
	require.Contains(t, shared, "photo")
	metadata, err := CapturedMetadata([]byte(`{"category":"jpgfish","date":"0001-01-01T00:00:00Z"}`))
	require.NoError(t, err)
	require.Nil(t, metadata.PublishedAt)
}
