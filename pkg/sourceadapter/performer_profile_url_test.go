package sourceadapter_test

import (
	"testing"

	"github.com/stashapp/stash/pkg/sourceadapter"
	"github.com/stretchr/testify/require"
)

func TestOnlyFansPerformerProfileURL(t *testing.T) {
	for _, value := range []string{
		"https://onlyfans.com/LucyMochi/c76", "http://www.onlyfans.com/lucymochi/c30?ref=reddit#bio",
		"https://onlyfans.com/lucymochi/trial/token", "https://onlyfans.com/lucymochi/?ref=bio",
		"onlyfans.com/LucyMochi", "www.onlyfans.com/lucymochi/c1", "//onlyfans.com/lucymochi/c2",
		" https://ONLYFANS.COM/LucyMochi ", "https://onlyfans.com/lucymochi",
	} {
		require.Equal(t, "https://onlyfans.com/lucymochi", sourceadapter.PerformerProfileURL(value), value)
	}
	for _, value := range []string{
		"https://onlyfans.com/my/subscriptions", "https://onlyfans.com/12345/username",
		"https://onlyfans.com/", "https://onlyfans.com/login?next=/lucymochi", "https://onlyfans.com/../lucymochi",
		"https://onlyfans.com/lucy%2Fmochi/c76", "https://onlyfans.com/lucy%20mochi/c76",
		"https://onlyfans.com.evil.invalid/lucymochi/c76", "https://user@onlyfans.com/lucymochi/c76",
		"https://onlyfans.com:8443/lucymochi/c76", "ftp://onlyfans.com/lucymochi/c76",
		"https://onlyfans.com/lucymochi\n", "https://onlyfans.com/lucymochi\v",
		"https://fansly.com/lucymochi/c76", "https://site.invalid/CaseSensitive/path?user=2",
	} {
		require.Equal(t, value, sourceadapter.PerformerProfileURL(value), value)
	}
	// Metadata normalization must not change historical keys or infer an owner.
	require.Equal(t, "url:https://onlyfans.com/lucymochi/c76", sourceadapter.ProfileURLKey("https://onlyfans.com/lucymochi/c76"))
	require.Nil(t, sourceadapter.ProfileReference("https://onlyfans.com/lucymochi/c76"))
}

func TestOnlyFansBioSocialAndResolvedLinksShareOneProfile(t *testing.T) {
	user := `{"id":"abc","name":"Example","url":"https://onlyfans.com/LucyMochi/c76","subreddit":{"public_description":"https://onlyfans.com/lucymochi/trial/token https://short.invalid/me"},"social_links":[{"url":"https://www.onlyfans.com/lucymochi?ref=bio"},{"url":"https://onlyfans.com/lucymochi_vip/c1"}],"resolved_links":[{"url":"https://short.invalid/me","resolved_url":"https://onlyfans.com/lucymochi/c30"},{"url":"https://unrelated.invalid","resolved_url":"https://onlyfans.com/wrong"}]}`
	want := []string{"https://onlyfans.com/lucymochi", "https://onlyfans.com/lucymochi_vip"}
	profile, err := sourceadapter.ExtractAccountProfile([]byte(`{"category":"reddit","user":` + user + `}`))
	require.NoError(t, err)
	require.Equal(t, want, profile.URLs)
	links, err := sourceadapter.ExtractCapturedProfileURLs([]byte(`{"category":"reddit","author":"Example","author_fullname":"t2_abc","user":` + user + `}`))
	require.NoError(t, err)
	require.Equal(t, want, links)
}
