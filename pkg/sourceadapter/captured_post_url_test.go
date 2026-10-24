package sourceadapter_test

import (
	"testing"

	"github.com/stashapp/stash/pkg/sourceadapter"
	"github.com/stretchr/testify/require"
)

func TestCapturedPostURLUsesPostIdentityAndNotMediaOrDiscoveryURLs(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{"reddit permalink", `{"category":"reddit","id":"1x4tyzx","permalink":"/r/MommyHeaven/comments/1x4tyzx/mommy_needs_a_massage/","url":"https://www.reddit.com/gallery/1x4tyzx"}`, "https://www.reddit.com/r/MommyHeaven/comments/1x4tyzx/mommy_needs_a_massage/"},
		{"absolute permalink", `{"category":"reddit","id":"abc123","permalink":"https://old.reddit.com/r/test/comments/abc123/title/?context=3#comment"}`, "https://www.reddit.com/r/test/comments/abc123/title/"},
		{"external media is not a post URL", `{"category":"reddit","id":"abc123","url":"https://redgifs.com/watch/someclip","source_extractor_url":"https://reddit.com/user/example/submitted"}`, "https://www.reddit.com/comments/abc123"},
		{"wrong post permalink", `{"category":"reddit","id":"abc123","permalink":"/comments/def456/title/"}`, "https://www.reddit.com/comments/abc123"},
		{"comment permalink", `{"category":"reddit","id":"abc123","permalink":"/comments/abc123/title/comment123/"}`, "https://www.reddit.com/comments/abc123"},
		{"foreign host", `{"category":"reddit","id":"abc123","permalink":"https://unrelated.test/comments/abc123/title/"}`, "https://www.reddit.com/comments/abc123"},
		{"credentials", `{"category":"reddit","id":"abc123","permalink":"https://secret@reddit.com/comments/abc123"}`, "https://www.reddit.com/comments/abc123"},
		{"scheme-relative", `{"category":"reddit","id":"abc123","permalink":"//reddit.com/comments/abc123"}`, "https://www.reddit.com/comments/abc123"},
		{"crosspost keeps enclosing post", `{"category":"reddit","id":"abc123","crosspost_parent_list":[{"id":"def456","permalink":"/comments/def456/title/"}]}`, "https://www.reddit.com/comments/abc123"},
		{"reddit external child", `{"category":"redgifs","id":"hostedclip","_reddit":{"id":"abc123","permalink":"/r/test/comments/abc123/title/"}}`, "https://www.reddit.com/r/test/comments/abc123/title/"},
		{"external parent", `{"category":"imgur","id":"hostedclip","_parent":{"category":"reddit","id":"abc123","permalink":"/comments/abc123/title/"}}`, "https://www.reddit.com/comments/abc123/title/"},
		{"twitter ID", `{"category":"twitter","tweet_id":1234567890123456789,"url":"https://video.twimg.com/media.mp4","quoted_tweet":{"tweet_id":"99"}}`, "https://x.com/i/web/status/1234567890123456789"},
		{"twitter GraphQL", `{"category":"twitter","rest_id":"123456789","legacy":{"id_str":"123456789"}}`, "https://x.com/i/web/status/123456789"},
		{"erome album", `{"category":"erome","album_id":"Example1","user":"uploader","num":2,"url":"https://cdn.example/file.mp4"}`, "https://www.erome.com/a/Example1"},
		{"invalid reddit ID", `{"category":"reddit","id":"abc/other"}`, ""},
		{"invalid twitter ID", `{"category":"twitter","tweet_id":"not-a-post"}`, ""},
		{"unsupported source", `{"category":"unknown","id":"abc123","url":"https://example.test/post"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			link, err := sourceadapter.ExtractCapturedPostURL([]byte(tc.raw))
			require.NoError(t, err)
			if tc.want == "" {
				require.Nil(t, link)
			} else {
				require.NotNil(t, link)
				require.Equal(t, tc.want, link.URL)
			}
		})
	}
}
