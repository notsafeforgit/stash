package archive

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostContentOmitsEngagementButPreservesPostEvidence(t *testing.T) {
	first := []byte(`{"category":"reddit","id":"post","author":"account","title":"original text","created_utc":123,"score":4,"ups":4,"subreddit_subscribers":100,"search_tags":"author:account","gallery_data":{"items":[{"media_id":"one"},{"media_id":"two"}]},"crosspost_parent":"t3_original","user":{"id":"accountid","name":"account","subreddit":{"public_description":"original bio"}}}`)
	second := []byte(`{"category":"reddit","id":"post","author":"account","title":"original text","created_utc":123,"score":40,"ups":40,"subreddit_subscribers":150,"gallery_data":{"items":[{"media_id":"one"},{"media_id":"two"}]},"crosspost_parent":"t3_original","user":{"id":"accountid","name":"account","subreddit":{"public_description":"original bio"}}}`)
	a, err := RetainPostContent(first)
	require.NoError(t, err)
	b, err := RetainPostContent(second)
	require.NoError(t, err)
	require.Equal(t, a, b)
	require.Contains(t, string(a), "original bio")
	require.Contains(t, string(a), "t3_original")
	require.Contains(t, string(a), `"media_id":"two"`)
	require.NotContains(t, string(a), "score")
	again, err := RetainPostContent(a)
	require.NoError(t, err)
	require.Equal(t, a, again)
}

func TestPostContentSearchAndProfileShareRevision(t *testing.T) {
	search, err := RetainPostContent([]byte(`{"category":"reddit","id":"p","author":"a","title":"unchanged","search_tags":"author:a"}`))
	require.NoError(t, err)
	profile, err := RetainPostContent([]byte(`{"category":"reddit","id":"p","author":"a","title":"unchanged","user":{"id":"a","name":"a"}}`))
	require.NoError(t, err)
	a, err := PrepareRetainedCapture("gallery-dl", "reddit", search)
	require.NoError(t, err)
	b, err := PrepareRetainedCapture("gallery-dl", "reddit", profile)
	require.NoError(t, err)
	require.Equal(t, a.Shared, b.Shared)
	require.Empty(t, a.Refs)
	require.Len(t, b.Refs, 1)
}

func TestPostContentNestedAndOtherServices(t *testing.T) {
	for _, raw := range []string{
		`{"category":"twitter","tweet_id":"1","content":"text","legacy":{"favorite_count":2,"full_text":"unchanged","in_reply_to_status_id_str":"3"}}`,
		`{"category":"redgifs","id":"child","_reddit":{"id":"p","score":2,"title":"text"}}`,
		`{"category":"bluesky","post_id":"p","text":"text","likeCount":2,"reply":{"parent":{"uri":"at://parent"}}}`,
		`{"category":"instagram","post_id":"1","description":"text","like_count":2}`,
		`{"category":"tiktok","id":"1","desc":"text","stats":{"diggCount":2}}`,
	} {
		retained, err := RetainPostContent([]byte(raw))
		require.NoError(t, err)
		require.NotContains(t, string(retained), "Count")
		require.NotContains(t, string(retained), "_count")
		require.NotContains(t, string(retained), "score")
		require.Contains(t, string(retained), "text")
	}
	for _, raw := range []string{`{"category":"unknown","score":2}`, `{"category":"reddit","user":{"score":2}}`} {
		retained, err := RetainPostContent([]byte(raw))
		require.NoError(t, err)
		require.Contains(t, string(retained), `"score":2`)
	}
}
