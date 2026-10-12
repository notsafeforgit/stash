package sourceadapter

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreservePostTextKeepsCaptionAndAcceptsGaps(t *testing.T) {
	saved := []byte(`{"category":"reddit","id":"p","title":"Saved title","selftext":"","author":"example"}`)
	current := []byte(`{"category":"reddit","id":"p","title":"Changed title","selftext":"Newly available body","author":"example","user":{"description":"New bio"},"gallery_data":{"items":[{"media_id":"new"}]},"media_metadata":{"new":{"e":"Image"}}}`)
	result, matched, err := PreservePostText(current, saved)
	require.NoError(t, err)
	require.True(t, matched)
	require.Contains(t, string(result), `"title":"Saved title"`)
	require.Contains(t, string(result), `"selftext":"Newly available body"`)
	require.Contains(t, string(result), `"description":"New bio"`)
	require.Contains(t, string(result), `"media_id":"new"`)
	metadata, err := CapturedMetadata(result)
	require.NoError(t, err)
	require.Equal(t, "Newly available body", *metadata.OriginalText)
}

func TestPreservePostTextDoesNotRewriteOtherPostsOrDownloadHosts(t *testing.T) {
	saved := []byte(`{"category":"reddit","id":"p","title":"Saved title"}`)
	other := []byte(`{"category":"reddit","id":"other","title":"Reposted with a different title"}`)
	result, matched, err := PreservePostText(other, saved)
	require.NoError(t, err)
	require.False(t, matched)
	require.JSONEq(t, string(other), string(result))
	external := []byte(`{"category":"redgifs","id":"redgifs-item","title":"Host title","_reddit":{"category":"reddit","id":"p","title":"Changed title"}}`)
	result, matched, err = PreservePostText(external, saved)
	require.NoError(t, err)
	require.True(t, matched)
	require.JSONEq(t, `{"category":"redgifs","id":"redgifs-item","title":"Host title","_reddit":{"category":"reddit","id":"p","title":"Saved title"}}`, string(result))
}

func TestPreservePostTextTwitterLeavesProfilesAndQuotedPostsIndependent(t *testing.T) {
	saved := []byte(`{"category":"twitter","tweet_id":"123","content":"Original caption","author":{"description":"Old bio"},"quote":{"content":"Old quote"}}`)
	current := []byte(`{"category":"twitter","tweet_id":"123","content":"Edited caption","author":{"description":"New bio"},"quote":{"content":"New quote"}}`)
	result, matched, err := PreservePostText(current, saved)
	require.NoError(t, err)
	require.True(t, matched)
	require.JSONEq(t, `{"category":"twitter","tweet_id":"123","content":"Original caption","author":{"description":"New bio"},"quote":{"content":"New quote"}}`, string(result))
}

func TestPreservePostTextDoesNotDiscardInlineMedia(t *testing.T) {
	saved := []byte(`{"category":"kemono","service":"patreon","user":"1","id":"2","content":"Saved caption"}`)
	current := []byte(`{"category":"kemono","service":"patreon","user":"1","id":"2","content":"<img src=\"/inline/new.jpg\">","attachments":[],"mirror_media":{"version":1,"post":{"namespace":"mirror:kemono:patreon","value":"1/2"},"items":[{"id":"/inline/new.jpg","kind":"image"}]}}`)
	result, matched, err := PreservePostText(current, saved)
	require.NoError(t, err)
	require.False(t, matched, "the caption contains new attachment evidence")
	require.JSONEq(t, string(current), string(result))
}
