package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadableTextPreservesPlainInputs(t *testing.T) {
	for _, item := range []struct{ input, expected string }{
		{"  Plain &amp; text  ", "  Plain &amp; text  "},
		{"<p>One &amp; two</p><div>Three<br>four</div>", "One & two\n\nThree\nfour"},
		{"<ul><li>First</li><li>Second</li></ul>", "First\n\nSecond"},
		{"<p>Read <a href='https://example.invalid'>this</a> <strong>text</strong></p>", "Read this text"},
		{"<p/>A<p/>B", "A\n\nB"},
		{"<p><!--ignored-->Visible<script>literal &amp;</script></p>", "Visibleliteral &amp;"},
	} {
		require.Equal(t, item.expected, ReadableText(item.input))
	}
}
