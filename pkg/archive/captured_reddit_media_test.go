package archive

import (
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCapturedRedditSingleMediaUsesPostEvidenceWithoutInventingAlbums(t *testing.T) {
	for _, tc := range []struct{ name, source, id, kind, evidence string }{
		{"image", `{"category":"reddit","id":"post","url":"https://i.redd.it/abc123.jpg","_url":"https://i.redd.it/other.jpg","num":99}`, "abc123", "image", "/url"},
		{"preview", `{"category":"reddit","id":"post","url":"https://preview.redd.it/abc123.png?width=1000&format=pjpg"}`, "abc123", "image", "/url"},
		{"gif", `{"category":"reddit","id":"post","url":"https://i.redd.it/abc123.gif"}`, "abc123", "unknown", "/url"},
		{"video", `{"category":"reddit","id":"post","url":"https://v.redd.it/abc123","is_video":true,"secure_media":{"reddit_video":{"dash_url":"https://v.redd.it/abc123/DASHPlaylist.mpd?x=1","fallback_url":"https://v.redd.it/abc123/DASH_1080.mp4"}}}`, "abc123", "video", "/url"},
		{"video fields", `{"category":"reddit","id":"post","secure_media":{"reddit_video":{"hls_url":"https://v.redd.it/abc123/HLSPlaylist.m3u8"}}}`, "abc123", "video", "/secure_media/reddit_video/hls_url"},
		{"crosspost", `{"category":"reddit","id":"crosspost","crosspost_parent":"t3_parent","crosspost_parent_list":[{"id":"parent","url":"https://i.redd.it/abc123.jpg"}]}`, "abc123", "image", "/crosspost_parent_list/0/url"},
		{"queued parent", `{"category":"ytdl","id":"download","_reddit":{"id":"post","url":"https://v.redd.it/abc123"}}`, "abc123", "video", "/_reddit/url"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captured, err := ExtractCapturedAlbum([]byte(tc.source))
			require.NoError(t, err)
			require.NotNil(t, captured)
			require.True(t, captured.Manifest.Complete)
			require.False(t, captured.Manifest.DeclaredAlbum)
			require.Equal(t, 1, *captured.Manifest.ExpectedCount)
			require.Equal(t, tc.evidence, captured.EvidencePath)
			require.Equal(t, []models.SourceAttachmentEntry{{Position: 0,
				Reference: models.SourcePostIdentifier{Namespace: "native:reddit", Value: tc.id}, MediaKind: tc.kind}}, captured.Manifest.Entries)
			manifest := models.SourceAttachmentManifest{Complete: true, ExpectedCount: captured.Manifest.ExpectedCount, EntryCount: 1}
			require.False(t, manifest.IsAlbum())
			retained, err := RetainSourcePayload([]byte(tc.source))
			require.NoError(t, err)
			replayed, err := ExtractCapturedAlbum(retained)
			require.NoError(t, err)
			require.Equal(t, captured, replayed)
		})
	}
}

func TestCapturedRedditSingleMediaDoesNotGuessFromDownloadOrPreview(t *testing.T) {
	for _, source := range []string{
		`{"category":"reddit","id":"post","_url":"https://i.redd.it/download.jpg","filename":"download","num":1}`,
		`{"category":"reddit","id":"post","url":"https://external.test/album/123","preview":{"images":[{"source":{"url":"https://preview.redd.it/thumb.jpg"}}]}}`,
		`{"category":"reddit","id":"post","url":"https://i.redd.it/folder/image.jpg"}`,
		`{"category":"reddit","id":"post","url":"https://i.redd.it/a%2Fb.jpg"}`,
		`{"category":"reddit","id":"post","url":"https://user@i.redd.it/image.jpg"}`,
	} {
		captured, err := ExtractCapturedAlbum([]byte(source))
		require.NoError(t, err, source)
		require.Nil(t, captured, source)
	}
	captured, err := ExtractCapturedAlbum([]byte(`{"category":"reddit","id":"post","is_gallery":true,"url":"https://i.redd.it/preview.jpg"}`))
	require.NoError(t, err)
	require.True(t, captured.Manifest.DeclaredAlbum)
	require.False(t, captured.Manifest.Complete)
	require.Empty(t, captured.Manifest.Entries, "an incomplete gallery cannot become a complete single-media post")
	_, err = ExtractCapturedAlbum([]byte(`{"category":"reddit","id":"post","url":"https://v.redd.it/first","media":{"reddit_video":{"dash_url":"https://v.redd.it/second/DASHPlaylist.mpd"}}}`))
	require.ErrorContains(t, err, "disagree")
}
