package archive_test

import (
	"testing"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stretchr/testify/require"
)

func TestRecoverGeneratedNFOFields(t *testing.T) {
	input := `<movie><url>https://example.com/?a=1&amp;b=2</url><premiered>2025-01-01</premiered><title>Title & more</title><plot>Text <3 &amp; more</plot></movie>`
	fields, err := archive.RecoverGeneratedNFOFields([]byte(input))
	require.NoError(t, err)
	data, err := archive.CollateLegacyNFOFields(fields)
	require.NoError(t, err)
	require.Equal(t, "Title & more", *data.Metadata.Title)
	require.Equal(t, "Text <3 & more", *data.Metadata.OriginalText)
	require.Equal(t, []string{"https://example.com/?a=1&b=2"}, data.URLs)
	for _, raw := range []string{input + "unexpected", `<!DOCTYPE movie SYSTEM "file:///etc/passwd">` + input, `<movie><title>Missing fields</title></movie>`} {
		_, err := archive.RecoverGeneratedNFOFields([]byte(raw))
		require.Error(t, err)
	}
}
