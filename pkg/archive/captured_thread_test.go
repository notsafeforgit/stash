package archive_test

import (
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCapturedTwitterThread(t *testing.T) {
	for _, raw := range []string{
		`{"category":"twitter","tweet_id":1900000000000000003,"conversation_id":1900000000000000001,"reply_id":1900000000000000002,"reply_user_id":"99","author":{"id":99}}`,
		`{"category":"twitter","rest_id":"1900000000000000003","legacy":{"id_str":"1900000000000000003","conversation_id_str":"1900000000000000001","in_reply_to_status_id_str":"1900000000000000002","in_reply_to_user_id_str":"99","user_id_str":"99"}}`,
	} {
		facts, err := archive.ExtractCapturedThread([]byte(raw))
		require.NoError(t, err)
		require.Equal(t, "1900000000000000003", facts.PostID)
		require.Equal(t, "1900000000000000001", facts.ConversationID)
		require.Equal(t, "1900000000000000002", facts.ReplyID)
		require.Equal(t, facts.AuthorID, facts.ReplyAuthorID)
	}
	root, err := archive.ExtractCapturedThread([]byte(`{"category":"twitter","tweet_id":"100","conversation_id":100,"reply_id":0,"author":{"id":"99"}}`))
	require.NoError(t, err)
	require.Empty(t, root.ReplyID)
	for _, raw := range []string{
		`{"category":"twitter","tweet_id":"100","quoted_id":99,"retweet_id":98,"author":{"id":"99"}}`,
		`{"category":"reddit","id":"100","conversation_id":100}`,
	} {
		facts, err := archive.ExtractCapturedThread([]byte(raw))
		require.NoError(t, err)
		require.Nil(t, facts)
	}
	for _, raw := range []string{
		`{"category":"twitter","tweet_id":"100","conversation_id":100,"reply_id":100,"author":{"id":"99"}}`,
		`{"category":"twitter","tweet_id":"100","conversation_id":200,"author":{"id":"99"}}`,
		`{"category":"twitter","tweet_id":1e3,"conversation_id":100,"author":{"id":"99"}}`,
		`{"category":"twitter","tweet_id":"101","conversation_id":100,"reply_id":99,"legacy":{"in_reply_to_status_id_str":"98"},"author":{"id":"99"}}`,
	} {
		_, err := archive.ExtractCapturedThread([]byte(raw))
		require.Error(t, err)
	}
}
