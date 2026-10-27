package sourceadapter

import (
	"encoding/json"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCapturedRedgifsItemsAndRedditParents(t *testing.T) {
	base := sourceObject{"category": "redgifs", "id": "ExampleClip", "userName": "ActualPublisher", "createDate": 1768092373,
		"description": "Original description", "urls": sourceObject{"hd": "https://media.redgifs.com/ExampleClip.mp4"},
		"date": "2020-01-01T00:00:00Z", "num": 8, "count": 10}
	ref := models.SourcePostIdentifier{Namespace: "native:redgifs", Value: "exampleclip"}
	for _, grouped := range []bool{false, true} {
		if grouped {
			base["gallery"] = "gallery-id"
		}
		raw, err := json.Marshal(base)
		require.NoError(t, err)
		retained, err := RetainSourcePayload(raw)
		require.NoError(t, err)
		for _, payload := range [][]byte{raw, retained} {
			post, err := ExtractCapturedPost(payload)
			require.NoError(t, err)
			require.Equal(t, ref, *post)
			album, err := ExtractCapturedAlbum(payload)
			require.NoError(t, err)
			require.Equal(t, ref, album.Post)
			require.Equal(t, []models.SourceAttachmentEntry{{Position: 0, Reference: ref, MediaKind: "unknown"}}, album.Manifest.Entries)
			require.False(t, album.Manifest.DeclaredAlbum)
			require.Equal(t, !grouped, album.Manifest.Complete)
			if grouped {
				require.Nil(t, album.Manifest.ExpectedCount)
			} else {
				require.Equal(t, 1, *album.Manifest.ExpectedCount)
			}
			metadata, err := CapturedMetadata(payload)
			require.NoError(t, err)
			require.Equal(t, "2026-01-11T00:46:13.000Z", *metadata.PublishedAt)
			require.Equal(t, "Original description", *metadata.OriginalText)
			link, err := ExtractCapturedPostURL(payload)
			require.NoError(t, err)
			require.Equal(t, "https://www.redgifs.com/watch/exampleclip", link.URL)
			account, err := ExtractCapturedAccount(payload)
			require.NoError(t, err)
			require.Nil(t, account, "a handle cannot become an immutable publisher ID")
			claims, err := CapturedAccountReferences(payload)
			require.NoError(t, err)
			require.Equal(t, models.AccountReference{Namespace: "native:redgifs", Kind: "handle", Value: "actualpublisher"}, claims.Identifiers[0].Reference)
		}
	}
	base["_reddit"] = sourceObject{"id": "redditpost", "author_fullname": "t2_author", "author": "RedditPublisher",
		"url": "https://www.redgifs.com/watch/exampleclip", "date": "2026-02-01T00:00:00Z", "title": "Reddit caption"}
	raw, err := json.Marshal(base)
	require.NoError(t, err)
	post, err := ExtractCapturedPost(raw)
	require.NoError(t, err)
	require.Equal(t, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "redditpost"}, *post)
	album, err := ExtractCapturedAlbum(raw)
	require.NoError(t, err)
	require.Equal(t, ref, album.Manifest.Entries[0].Reference, "standalone and embedded media keep the same attachment identity")
	metadata, err := CapturedMetadata(raw)
	require.NoError(t, err)
	require.Equal(t, "Reddit caption", *metadata.OriginalText)
	require.Equal(t, "2026-02-01T00:00:00Z", *metadata.PublishedAt)
	account, err := ExtractCapturedAccount(raw)
	require.NoError(t, err)
	require.Equal(t, "native:reddit", account.Namespace)
}

func TestRedgifsInvalidIdentityAndPublication(t *testing.T) {
	for _, id := range []any{nil, 123, "", "../clip", "clip-id"} {
		raw, err := json.Marshal(sourceObject{"category": "redgifs", "id": id})
		require.NoError(t, err)
		_, err = ExtractCapturedPost(raw)
		require.Error(t, err)
	}
	for _, stamp := range []any{true, "1768092373", -1, 1.5, 253402300800} {
		raw, err := json.Marshal(sourceObject{"category": "redgifs", "id": "clip", "createDate": stamp})
		require.NoError(t, err)
		_, err = CapturedMetadata(raw)
		require.Error(t, err)
	}
	album, err := ExtractCapturedAlbum([]byte(`{"category":"redgifs","id":"clip","urls":{"poster":"https://media.redgifs.com/clip.jpg"}}`))
	require.NoError(t, err)
	require.Nil(t, album, "a poster does not prove an available media rendition")
	metadata, err := CapturedMetadata([]byte(`{"category":"redgifs","id":"clip","date":"2020-01-01T00:00:00Z"}`))
	require.NoError(t, err)
	require.Nil(t, metadata.PublishedAt, "a different gallery member's date must not become this item's date")
}

func TestRedgifsProfileReferences(t *testing.T) {
	require.Equal(t, &models.AccountReference{Namespace: "native:redgifs", Kind: "handle", Value: "example_user"},
		ProfileReference("https://www.redgifs.com/users/Example_User/"))
	for _, value := range []string{"https://www.redgifs.com/watch/clip", "https://www.redgifs.com/users/example/collections", "https://www.redgifs.com/users/example/collections/one"} {
		require.Nil(t, ProfileReference(value))
	}
}
