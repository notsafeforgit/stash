package sourceadapter

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCapturedRedditRedgifsProfile(t *testing.T) {
	fixture := func() sourceObject {
		return sourceObject{"category": "redgifs", "id": "ExampleClip", "userName": "Uploader_O",
			"native_redgifs_directory": "Custom/Reddit user", "source_extractor_url": "https://www.redgifs.com/watch/ExampleClip",
			"_reddit": sourceObject{"category": "reddit", "id": "post1", "author": "RedditUser0", "author_fullname": "t2_123",
				"url": "https://www.redgifs.com/watch/exampleclip"}}
	}
	for _, tc := range []struct {
		name string
		edit func(sourceObject, sourceObject)
		want bool
	}{
		{"API uploader differs from Reddit", func(_, _ sourceObject) {}, true},
		{"embed", func(_, p sourceObject) { p["url"] = "https://www.redgifs.com/ifr/ExampleClip" }, true},
		{"body link", func(_, p sourceObject) {
			p["url"] = "https://reddit.com/comments/post1"
			p["selftext"] = "[clip](https://redgifs.com/watch/exampleclip)"
		}, true},
		{"numeric alias", func(d, p sourceObject) {
			p["url"] = "https://redgifs.com/watch/12345"
			d["source_extractor_url"] = p["url"]
		}, true},
		{"unproven alias", func(_, p sourceObject) { p["url"] = "https://redgifs.com/watch/12345" }, false},
		{"different item", func(d, _ sourceObject) { d["id"] = "AnotherClip" }, false},
		{"conflicting destination", func(_, p sourceObject) { p["url_overridden_by_dest"] = "https://redgifs.com/watch/other" }, false},
		{"invalid uploader", func(d, _ sourceObject) { d["userName"] = "../another" }, false},
		{"missing uploader", func(d, _ sourceObject) { delete(d, "userName") }, false},
		{"crosspost", func(_, p sourceObject) { p["crosspost_parent"] = "t3_other" }, false},
		{"crosspost list", func(_, p sourceObject) { p["crosspost_parent_list"] = []any{sourceObject{"id": "other"}} }, false},
		{"comment", func(_, p sourceObject) { p["comment"] = sourceObject{"id": "comment1"} }, false},
		{"no stable Reddit author", func(_, p sourceObject) { delete(p, "author_fullname") }, false},
		{"standalone item", func(d, _ sourceObject) { delete(d, "_reddit") }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := fixture()
			tc.edit(data, data["_reddit"].(sourceObject))
			raw, err := json.Marshal(data)
			require.NoError(t, err)
			retained, err := RetainSourcePayload(raw)
			require.NoError(t, err)
			for _, body := range [][]byte{raw, retained} {
				link, err := ExtractCapturedRedditRedgifsProfile(body)
				require.NoError(t, err)
				if !tc.want {
					require.Nil(t, link)
					continue
				}
				require.Equal(t, &CapturedLinkedProfile{SourceProfileURL: "https://www.reddit.com/user/reddituser0/",
					URL: "https://www.redgifs.com/users/uploader_o", Directory: "Custom/Reddit user"}, link)
			}
		})
	}
}
