package scrape

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceOriginV1KeepsOnlyCanonicalHTTPOrigin(t *testing.T) {
	for raw, want := range map[string]string{
		"https://WWW.Example.COM.:443/private/path?token=secret#private": "https://www.example.com/",
		"http://example.com:080/file":                                    "http://example.com/",
		"https://example.com:8443/file":                                  "https://example.com:8443/",
		"https://bücher.example/file":                                    "https://xn--bcher-kva.example/",
		"https://cdn.example.net/signed.mp4?signature=secret":            "https://cdn.example.net/",
	} {
		t.Run(raw, func(t *testing.T) {
			origin, err := SourceOriginV1(raw)
			require.NoError(t, err)
			require.Equal(t, want, origin)
			again, err := SourceOriginV1(origin)
			require.NoError(t, err)
			require.Equal(t, origin, again)
			scope, err := SourceScopeV1(raw)
			require.NoError(t, err)
			fromOrigin, err := SourceScopeV1(origin)
			require.NoError(t, err)
			require.Equal(t, scope, fromOrigin)
		})
	}
	for _, raw := range []string{"", "file:///tmp/media", "https://user:secret@example.com/file", "https://example.com:0/", "https://example.com:65536/", "https://example.com:/", "https://example.com:bad/", " https://example.com/", "https://example.com/\n"} {
		_, err := SourceOriginV1(raw)
		require.Error(t, err, raw)
	}
}
