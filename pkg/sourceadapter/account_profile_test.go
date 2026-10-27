package sourceadapter

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAccountProfileSocialLinksAndResolvedBio(t *testing.T) {
	raw := []byte(`{"category":"reddit","user":{"id":"abc123","name":"Example","subreddit":{"public_description":"See https://myredlink.com/example"},"social_links":[{"url":"https://instagram.com/example"}],"resolved_links":[{"url":"https://myredlink.com/example","resolved_url":"https://onlyfans.com/example"},{"url":"https://unrelated.invalid","resolved_url":"https://wrong.invalid"}]}}`)
	p, err := ExtractAccountProfile(raw)
	require.NoError(t, err)
	require.Equal(t, "t2_abc123", p.ID.Value)
	require.Equal(t, "example", p.Handle.Value)
	require.ElementsMatch(t, []string{"https://instagram.com/example", "https://onlyfans.com/example"}, p.URLs)
	for _, raw := range []string{
		`{"category":"reddit","user":{"name":"example"}}`,
		`{"category":"reddit","user":{"id":"abc","name":"example"},"id":"post"}`,
		`{"category":"twitter","user":{"id":"abc","name":"example"}}`,
		`{"category":"reddit","user":{"id":"abc","name":"bad/name"}}`,
	} {
		_, err := ExtractAccountProfile([]byte(raw))
		require.Error(t, err)
	}
}
