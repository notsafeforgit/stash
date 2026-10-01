package archive

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCapturedRedditAlbumUsesSourceOrderAndRetainsUnavailableSlots(t *testing.T) {
	raw := []byte(`{"category":"reddit","id":"post","is_gallery":true,"num":1,
"gallery_data":{"items":[{"id":999,"media_id":"unavailable"},{"media_id":"image"},{"media_id":"video"},{"media_id":"image"}]},
"media_metadata":{"unavailable":{"status":"failed"},"image":{"e":"Image","status":"valid"},"video":{"e":"RedditVideo"}}}`)
	before := bytes.Clone(raw)
	album, err := ExtractCapturedAlbum(raw)
	require.NoError(t, err)
	require.Equal(t, before, raw)
	require.Equal(t, CapturedAlbumPolicy, album.Policy)
	require.Equal(t, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "post"}, album.Post)
	require.Equal(t, "/gallery_data/items", album.EvidencePath)
	require.True(t, album.Manifest.DeclaredAlbum)
	require.True(t, album.Manifest.Complete, "source-list completeness does not imply successful downloads")
	require.Equal(t, 4, *album.Manifest.ExpectedCount)
	require.Equal(t, []models.SourceAttachmentEntry{
		{Position: 0, Reference: models.SourcePostIdentifier{Namespace: "native:reddit", Value: "unavailable"}, MediaKind: "unknown"},
		{Position: 1, Reference: models.SourcePostIdentifier{Namespace: "native:reddit", Value: "image"}, MediaKind: "image"},
		{Position: 2, Reference: models.SourcePostIdentifier{Namespace: "native:reddit", Value: "video"}, MediaKind: "video"},
		{Position: 3, Reference: models.SourcePostIdentifier{Namespace: "native:reddit", Value: "image"}, MediaKind: "image"},
	}, album.Manifest.Entries)
	require.Empty(t, album.Manifest.CaptureUUID)
	replay, err := ExtractCapturedAlbum(raw)
	require.NoError(t, err)
	require.Equal(t, album, replay)
	retained, err := RetainSourcePayload(raw)
	require.NoError(t, err)
	replay, err = ExtractCapturedAlbum(retained)
	require.NoError(t, err)
	require.Equal(t, album, replay)
}

func TestCapturedRedditAlbumsPreserveParentContextAndMissingEvidence(t *testing.T) {
	album, err := ExtractCapturedAlbum([]byte(`{"category":"redgifs","id":"child",
"_reddit":{"id":"repost","crosspost_parent":"t3_original","crosspost_parent_list":[{"id":"original",
"gallery_data":{"items":[{"media_id":"first"},null,{"id":"slot-only"},{"media_id":"last"}]}}]}}`))
	require.NoError(t, err)
	require.Equal(t, "repost", album.Post.Value, "a crosspost keeps its own gallery association")
	require.Equal(t, "/_reddit/crosspost_parent_list/0/gallery_data/items", album.EvidencePath)
	require.False(t, album.Manifest.Complete)
	require.Equal(t, 4, *album.Manifest.ExpectedCount)
	require.Equal(t, []int{0, 3}, []int{album.Manifest.Entries[0].Position, album.Manifest.Entries[1].Position})
	for _, raw := range []string{
		`{"category":"reddit","id":"post","is_gallery":true,"gallery_data":null}`,
		`{"category":"reddit","id":"post","is_gallery":true}`,
	} {
		album, err := ExtractCapturedAlbum([]byte(raw))
		require.NoError(t, err)
		require.True(t, album.Manifest.DeclaredAlbum)
		require.False(t, album.Manifest.Complete)
		require.Nil(t, album.Manifest.ExpectedCount)
		require.Empty(t, album.Manifest.Entries)
	}
	album, err = ExtractCapturedAlbum([]byte(`{"category":"reddit","id":"post","gallery_data":{"items":[{"media_id":"gif"}]},"media_metadata":{"gif":{"e":"AnimatedImage"}}}`))
	require.NoError(t, err)
	require.True(t, album.Manifest.DeclaredAlbum, "a declared album remains eligible with one source item")
	require.Equal(t, "unknown", album.Manifest.Entries[0].MediaKind, "GIF/MP4 file classification is separate")
}

func TestCapturedTwitterAlbumRequiresOriginalMediaList(t *testing.T) {
	raw := []byte(`{"category":"twitter","tweet_id":9007199254740993,"num":2,"count":8,
"extended_entities":{"media":[{"id_str":"101","id":101,"type":"photo"},{"id_str":"102","type":"video"},{"id_str":"103","type":"animated_gif"}]}}`)
	album, err := ExtractCapturedAlbum(raw)
	require.NoError(t, err)
	require.Equal(t, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "9007199254740993"}, album.Post)
	require.True(t, album.Manifest.Complete)
	require.Equal(t, 3, *album.Manifest.ExpectedCount, "output preview files do not increase the post attachment count")
	require.Len(t, album.Manifest.Entries, 3)
	require.Equal(t, "image", album.Manifest.Entries[0].MediaKind)
	require.Equal(t, "video", album.Manifest.Entries[1].MediaKind)
	require.Equal(t, "video", album.Manifest.Entries[2].MediaKind)
	album, err = ExtractCapturedAlbum([]byte(`{"category":"twitter","rest_id":"100","legacy":{"id_str":"100","extended_entities":{"media":[{"id_str":"101","type":"photo"}]}}}`))
	require.NoError(t, err)
	require.Equal(t, "/legacy/extended_entities/media", album.EvidencePath)
	require.Equal(t, 1, *album.Manifest.ExpectedCount)
	require.False(t, album.Manifest.DeclaredAlbum, "an ordinary single-media post is not a declared album")
	for _, raw := range []string{
		`{"category":"twitter","tweet_id":"100","num":1,"count":4,"media_id":"101","filename":"first-photo"}`,
		`{"category":"twitter","tweet_id":"100","entities":{"media":[{"id_str":"101"}]}}`,
		`{"category":"reddit","id":"post","num":3,"count":4,"filename":"third-photo"}`,
		`{"category":"reddit","gallery_data":{"items":[{"media_id":"101"}]}}`,
		`{"category":"other","id":"post","gallery_data":{"items":[{"media_id":"101"}]}}`,
	} {
		album, err := ExtractCapturedAlbum([]byte(raw))
		require.NoError(t, err, raw)
		require.Nil(t, album, raw)
	}
}

func TestCapturedAlbumRejectsContradictoryOrInvalidEvidence(t *testing.T) {
	for _, raw := range []string{
		`{"category":"reddit","id":"post","gallery_data":[]}`,
		`{"category":"reddit","id":"post","gallery_data":{"items":{}}}`,
		`{"category":"reddit","id":"post","gallery_data":{"items":[false]}}`,
		`{"category":"reddit","id":"post","gallery_data":{"items":[{"media_id":{"bad":"id"}}]}}`,
		`{"category":"reddit","id":"post","gallery_data":{"items":[{"media_id":1.5}]}}`,
		`{"category":"reddit","id":"post","crosspost_parent":"t3_other","crosspost_parent_list":[{"id":"original"}]}`,
		`{"category":"reddit","id":"post","crosspost_parent_list":[{}]}`,
		`{"category":"twitter","tweet_id":"100","rest_id":"200","extended_entities":{"media":[]}}`,
		`{"category":"twitter","tweet_id":"100","extended_entities":{"media":[{"id_str":"101","id":102}]}}`,
		`{"category":"twitter","tweet_id":"100","extended_entities":{"media":{}}}`,
		`{"category":"twitter","tweet_id":"100","extended_entities":{"media":[{"id_str":" padded "}]}}`,
	} {
		album, err := ExtractCapturedAlbum([]byte(raw))
		require.Error(t, err, raw)
		require.Nil(t, album, raw)
	}
	raw, err := json.Marshal(map[string]interface{}{"category": "reddit", "id": "post", "gallery_data": map[string]interface{}{"items": make([]interface{}, MaxManifestEntries+1)}})
	require.NoError(t, err)
	_, err = ExtractCapturedAlbum(raw)
	require.ErrorContains(t, err, "4096")
}
