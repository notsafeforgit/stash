package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharingPublicURL(t *testing.T) {
	for _, value := range []string{"", "https://nsfw.ak.codes/share", "https://example.test/mounted/share"} {
		require.NoError(t, ValidateSharingPublicURL(value), value)
	}
	for _, value := range []string{"http://example.test/share", "//example.test/share", "https://owner:secret@example.test/share", "https://example.test/share?apikey=secret", "https://example.test/share#secret", "https://example.test/share?", "https://example.test/share#", "https://example.test/library", "https://example.test/../share", "https://example.test/./share", "https://example.test//share", "https://example.test/%2e%2e/share", "https://:443/share"} {
		require.Error(t, ValidateSharingPublicURL(value), value)
	}
}
