package sourceadapter

import (
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestPostTitleUsesCaptionWithoutInventingSourceMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, platform, title, text, want string
		truncated                         bool
	}{
		{"twitter text", "twitter", "", "A short caption", "A short caption", false},
		{"twitter legacy filename", "twitter", "1575550205214134278_2", "A readable caption\nMore details", "A readable caption", false},
		{"x first sentence", "x", "", "A readable caption. Another sentence follows.", "A readable caption.", false},
		{"blank first lines", "twitter", "", " \n\n A readable caption\r\nMore details", "A readable caption", false},
		{"url and decimal", "twitter", "", "See https://example.test/a for version 1.5 today. More", "See https://example.test/a for version 1.5 today.", false},
		{"unicode sentence", "twitter", "", "短い文。次の文", "短い文。", false},
		{"long unicode", "twitter", "", strings.Repeat("界", 220), strings.Repeat("界", 200), true},
		{"reddit title", "reddit", "The actual title", "A different description", "The actual title", false},
		{"caption only", "fansly", "", "A short caption\nLonger body", "A short caption", false},
		{"media only", "twitter", "1575550205214134278_2.jpg", "", "", false},
		{"no content", "twitter", "", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metadata := models.SourcePostMetadata{Title: &tc.title, OriginalText: &tc.text}
			title, truncated := PostTitle(tc.platform, metadata, 512)
			require.Equal(t, tc.want, title)
			require.Equal(t, tc.truncated, truncated)
			require.Equal(t, tc.title, *metadata.Title)
			require.Equal(t, tc.text, *metadata.OriginalText)
		})
	}
}
