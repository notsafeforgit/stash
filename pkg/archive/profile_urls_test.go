package archive_test

import (
	"testing"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stretchr/testify/require"
)

func TestCapturedProfileURLsOnlyUseMatchingAccountMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      []string
	}{
		{"reddit bio", `{"category":"reddit","author":"Example","author_fullname":"t2_abc","title":"https://post.invalid","user":{"id":"abc","name":"Example","icon_img":"https://art.invalid","subreddit":{"public_description":"See [my site](https://site.invalid/me?a=1&amp;b=2).","url":"/user/Example/"}}}`, []string{"https://site.invalid/me?a=1&b=2"}},
		{"reddit saved feed owner", `{"category":"reddit","author":"Else","author_fullname":"t2_abc","user":{"id":"other","name":"Else","subreddit":{"public_description":"https://wrong.invalid"}}}`, nil},
		{"twitter author and feed owner", `{"category":"twitter","author":{"id":123,"name":"Example","description":"https://t.co/bio","url":"https://t.co/web","profile_image":"https://art.invalid","entities":{"url":{"urls":[{"url":"https://t.co/web","expanded_url":"https://site.invalid"}]},"description":{"urls":[{"url":"https://t.co/bio","expanded_url":"https://onlyfans.com/example"}]}}},"user":{"id":456,"description":"https://wrong.invalid"},"content":"https://post.invalid"}`, []string{"https://onlyfans.com/example", "https://site.invalid"}},
		{"twitter original user", `{"category":"twitter","author":{"id":123,"name":"Example"},"user":{"id":"User:123","rest_id":"123","core":{"screen_name":"Example"},"legacy":{"description":"https://site.invalid","entities":{"url":{"urls":[{"url":"https://t.co/a","expanded_url":"https://site.invalid"}]}}}}}`, []string{"https://site.invalid"}},
		{"instagram structured bio", `{"category":"instagram","owner_id":123,"username":"example","description":"https://caption.invalid","user":{"pk":"123","username":"example","biography":"https://site.invalid","external_url":"https://site.invalid","bio_links":[{"title":"shop","url":"https://shop.invalid"}],"profile_pic_url":"https://art.invalid"}}`, []string{"https://shop.invalid", "https://site.invalid"}},
		{"bluesky", `{"category":"bluesky","author":{"did":"did:plc:abc","handle":"example.bsky.social"},"user":{"did":"did:plc:abc","description":"<a href=\"https://site.invalid\">site</a>"}}`, []string{"https://site.invalid"}},
		{"tiktok", `{"category":"tiktok","author":{"id":"123","uniqueId":"example","signature":"https://site.invalid","bioLink":{"link":"https://shop.invalid"}}}`, []string{"https://shop.invalid", "https://site.invalid"}},
		{"fansly", `{"category":"fansly","account":{"id":"123","username":"example","about":"https://site.invalid"}}`, []string{"https://site.invalid"}},
		{"mirror", `{"category":"coomer","service":"onlyfans","user":"123","user_profile":{"id":"123","service":"onlyfans","bio":"https://site.invalid"}}`, []string{"https://site.invalid"}},
		{"mirror different service", `{"category":"kemono","service":"patreon","user":"123","user_profile":{"id":"123","service":"fansly","bio":"https://wrong.invalid"}}`, nil},
		{"generic captured creator", `{"category":"example","creator":{"id":"123","username":"creator","bio":"https://site.invalid"}}`, []string{"https://site.invalid"}},
		{"no profile data", `{"category":"reddit","author_fullname":"t2_abc","author":"Example","url":"https://post.invalid","description":"https://caption.invalid"}`, nil},
		{"no stable identity", `{"category":"twitter","author":{"name":"Example","description":"https://site.invalid"}}`, nil},
		{"quote never supplies bio", `{"category":"twitter","author":{"id":123,"name":"Example"},"quoted_by":{"id":456,"description":"https://wrong.invalid"},"quoted_tweet":{"author":{"id":789,"description":"https://wrong.invalid"}}}`, nil},
		{"invalid profile URLs", `{"category":"twitter","author":{"id":123,"name":"Example","url":"javascript:alert(1)","description":"https://user:pass@site.invalid http://site.invalid:99/"}}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			retained, err := archive.RetainSourcePayload([]byte(tc.raw))
			require.NoError(t, err)
			links, err := archive.ExtractCapturedProfileURLs(retained)
			require.NoError(t, err)
			require.ElementsMatch(t, tc.want, links)
		})
	}
}

func TestProfileURLComparisonPreservesPathsAndRecognizesAccountAliases(t *testing.T) {
	require.Equal(t, archive.ProfileURLKey("https://x.com/Example?ref=profile"), archive.ProfileURLKey("http://www.twitter.com/example/"))
	require.Equal(t, archive.ProfileURLKey("http://www.site.invalid/me/"), archive.ProfileURLKey("https://site.invalid/me"))
	require.NotEqual(t, archive.ProfileURLKey("https://site.invalid/Me"), archive.ProfileURLKey("https://site.invalid/me"))
	require.NotEqual(t, archive.ProfileURLKey("https://site.invalid/?id=1"), archive.ProfileURLKey("https://site.invalid/?id=2"))
}
