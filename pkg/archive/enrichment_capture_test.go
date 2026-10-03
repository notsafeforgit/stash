package archive

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCapturedMetadataUsesOriginalSourceTextAndParent(t *testing.T) {
	for _, input := range []string{
		`{"category":"reddit","title":"Original title","selftext":"  Original\ntext  ","content":"","date":"2026-10-03T01:02:03+00:00","lang":"","language":"ja"}`,
		`{"category":"redgifs","title":"Child title","content":"Child description","date":"2025-01-01","_reddit":{"category":"reddit","id":"abc","title":"Original title","selftext":"  Original\ntext  ","date":"2026-10-03T01:02:03+00:00","language":"ja"}}`,
	} {
		metadata, err := CapturedMetadata([]byte(input))
		require.NoError(t, err)
		require.Equal(t, "Original title", *metadata.Title)
		require.Equal(t, "  Original\ntext  ", *metadata.OriginalText)
		require.Equal(t, "2026-10-03T01:02:03+00:00", *metadata.PublishedAt)
		require.Equal(t, "source", *metadata.DateBasis)
		require.Equal(t, "ja", *metadata.Language)
	}
	metadata, err := CapturedMetadata([]byte(`{"title":"Fallback","selftext":null,"date":12345,"language":false}`))
	require.NoError(t, err)
	require.Equal(t, "Fallback", *metadata.OriginalText)
	require.Nil(t, metadata.PublishedAt)
	require.Nil(t, metadata.DateBasis)
	require.Nil(t, metadata.Language)
}
