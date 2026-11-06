package sourceadapter

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTwitterPublicationDate(t *testing.T) {
	require.Equal(t, "2022-08-06T10:03:45.838Z", *TwitterPublicationDate("1555857150810865666"))
	for _, identity := range []string{"", "0", "12345", "+1555857150810865666", "01555857150810865666", "1e18", "18446744073709551616", "18446744073709551615"} {
		require.Nil(t, TwitterPublicationDate(identity), identity)
	}
}

func TestTwitterDateUsesOnlyPostPermalinks(t *testing.T) {
	const identity = "1555857150810865666"
	for _, address := range []string{
		"https://x.com/i/web/status/" + identity,
		"https://twitter.com/example/status/" + identity + "/photo/2?s=20",
		"http://mobile.x.com/example/status/" + identity,
	} {
		require.Equal(t, identity, TwitterPostID(address))
	}
	for _, address := range []string{
		"https://example.com/example/status/" + identity,
		"https://x.com.evil.test/example/status/" + identity,
		"https://user@x.com/example/status/" + identity,
		"https://x.com:443/example/status/" + identity,
		"https://x.com/i/user/" + identity,
		"https://x.com/example/status/%31" + identity,
		"https://x.com/example/status/" + identity + "/unrecognized",
	} {
		require.Empty(t, TwitterPostID(address), address)
	}
}

func TestCapturedTwitterDateFallbackPreservesActualSourceDates(t *testing.T) {
	metadata, err := CapturedMetadata([]byte(`{"category":"twitter","tweet_id":1555857150810865666,"content":"Saved caption"}`))
	require.NoError(t, err)
	require.Equal(t, "2022-08-06T10:03:45.838Z", *metadata.PublishedAt)
	require.Equal(t, TwitterDateBasis, *metadata.DateBasis)
	metadata, err = CapturedMetadata([]byte(`{"category":"twitter","tweet_id":1555857150810865666,"date":"2022-08-06T10:03:45Z"}`))
	require.NoError(t, err)
	require.Equal(t, "2022-08-06T10:03:45Z", *metadata.PublishedAt)
	require.Equal(t, "source", *metadata.DateBasis)
	metadata, err = CapturedMetadata([]byte(`{"category":"reddit","id":"test","url":"https://x.com/i/web/status/1555857150810865666"}`))
	require.NoError(t, err)
	require.Nil(t, metadata.PublishedAt)
}
