package sourceadapter

import (
	"encoding/json"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCapturedRedditLegacyRedgifsAttachment(t *testing.T) {
	for _, link := range []string{
		"https://gfycat.com/ExampleClip",
		"https://www.gfycat.com/gifs/detail/ExampleClip",
		"https://m.gfycat.com/ifr/ExampleClip/",
	} {
		t.Run(link, func(t *testing.T) {
			parent := sourceObject{"category": "reddit", "id": "post", "author": "Publisher", "author_fullname": "t2_author",
				"url": link, "url_overridden_by_dest": "https://www.redgifs.com/watch/exampleclip"}
			data := sourceObject{"category": "redgifs", "id": "exampleclip", "userName": "Uploader",
				"source_extractor_url": link, "_reddit": parent,
				"urls": sourceObject{"hd": "https://media.redgifs.com/ExampleClip.mp4"}}
			raw, err := json.Marshal(data)
			require.NoError(t, err)
			album, err := ExtractCapturedAlbum(raw)
			require.NoError(t, err)
			require.NotNil(t, album)
			require.Equal(t, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "post"}, album.Post)
			require.Equal(t, models.SourcePostIdentifier{Namespace: "native:redgifs", Value: "exampleclip"}, album.Manifest.Entries[0].Reference)
			require.False(t, album.Manifest.Complete, "a legacy link does not establish a complete remote gallery")
			profile, err := ExtractCapturedRedditRedgifsProfile(raw)
			require.NoError(t, err)
			require.NotNil(t, profile)
			require.Equal(t, "https://www.redgifs.com/users/uploader", profile.URL)
			parent["url_overridden_by_dest"] = "https://redgifs.com/watch/differentclip"
			raw, err = json.Marshal(data)
			require.NoError(t, err)
			_, err = ExtractCapturedAlbum(raw)
			require.Error(t, err, "legacy aliases cannot hide contradictory attachment identities")
			profile, err = ExtractCapturedRedditRedgifsProfile(raw)
			require.NoError(t, err)
			require.Nil(t, profile)
		})
	}
	for _, link := range []string{"https://gfycat.com/users/example", "https://gfycat.com/search/example",
		"https://gfycat.com.evil.invalid/ExampleClip", "https://gfycat.com/ExampleClip/other"} {
		ref, _, _ := redditExternalReference(link)
		require.Nil(t, ref, link)
	}
}
