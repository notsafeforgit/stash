package archive

import (
	"testing"

	"github.com/stashapp/stash/pkg/models"

	"github.com/stretchr/testify/require"
)

func TestProfileReferenceNamespaces(t *testing.T) {
	for _, tc := range []struct{ url, namespace, kind, value string }{
		{"https://www.reddit.com/user/Alice/", "native:reddit", "handle", "alice"},
		{"https://x.com/Alice/media", "native:twitter", "handle", "alice"},
		{"https://twitter.com/i/user/90", "native:twitter", "id", "90"},
		{"https://instagram.com/Alice.Name", "native:instagram", "handle", "alice.name"},
		{"https://bsky.app/profile/alice.test/media", "native:bluesky", "handle", "alice.test"},
		{"https://bsky.app/profile/did:plc:abc", "native:bluesky", "id", "did:plc:abc"},
		{"https://www.tiktok.com/@Alice", "native:tiktok", "handle", "alice"},
		{"https://www.patreon.com/user?u=70", "native:patreon", "id", "70"},
		{"https://patreon.com/c/Alice", "native:patreon", "handle", "alice"},
		{"https://onlyfans.com/Alice", "native:onlyfans", "handle", "alice"},
		{"https://fansly.com/Alice", "native:fansly", "handle", "alice"},
		{"https://coomer.st/onlyfans/user/Alice", "mirror:coomer:onlyfans", "user", "Alice"},
		{"https://coomer.su/fansly/user/50", "mirror:coomer:fansly", "user", "50"},
		{"https://kemono.cr/patreon/user/70", "mirror:kemono:patreon", "user", "70"},
		{"https://beta.kemono.party/fanbox/user/80", "mirror:kemono:fanbox", "user", "80"},
		{"https://coomerfans.com/u/fansly/50/alice", "mirror:coomer:fansly", "user", "50"},
		{"https://alice.tumblr.com/", "native:tumblr", "handle", "alice"},
	} {
		t.Run(tc.url, func(t *testing.T) {
			require.Equal(t, &models.AccountReference{Namespace: tc.namespace, Kind: tc.kind, Value: tc.value}, ProfileReference(tc.url))
		})
	}
}

func TestProfileReferenceDoesNotInferAuthorsFromPostsOrFeeds(t *testing.T) {
	for _, value := range []string{
		"https://reddit.com/r/example", "https://reddit.com/user/alice/comments/post",
		"https://x.com/search?q=alice", "https://x.com/alice/status/123",
		"https://instagram.com/p/123", "https://instagram.com/explore",
		"https://tiktok.com/@alice/video/123", "https://bsky.app/profile/alice.test/post/123",
		"https://coomer.st/onlyfans/user/alice/post/123", "https://patreon.com/posts/example-123",
	} {
		require.Nil(t, ProfileReference(value), value)
	}
}

func TestUnknownProfileURLsPreserveIdentityComponents(t *testing.T) {
	refs := ProfileReferences("http://www.example.test/CaseSensitive/Account?user=Alice#presentation")
	require.Equal(t, []models.AccountReference{{Namespace: "url", Kind: "profile", Value: "https://example.test/CaseSensitive/Account?user=Alice"}}, refs)
	require.NotEqual(t, ProfileReferences("https://example.test/user?id=1"), ProfileReferences("https://example.test/user?id=2"))
	canonical, valid := CanonicalProfileURL("https://example.test/a%2F/")
	require.True(t, valid)
	require.Equal(t, "https://example.test/a%2F", canonical)
	for _, value := range []string{"javascript:alert(1)", "https://user:secret@example.test/alice", "https://example.test:8443/alice", "https://example.test:/alice", "https://example.test/alice\n", "/alice"} {
		require.Empty(t, ProfileReferences(value), value)
	}
}
