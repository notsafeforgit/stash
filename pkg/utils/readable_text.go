package utils

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var readableHTML = regexp.MustCompile(`(?i)</?(?:p|div|br|span|a|strong|em|ul|li)(?:\s|/?>)`)

// ReadableText projects common HTML caption markup to plain text with paragraph
// and line breaks. Plain inputs, including their whitespace and literal entities,
// stay unchanged. It is a text projection, not an HTML sanitizer.
func ReadableText(original string) string {
	if !readableHTML.MatchString(original) {
		return original
	}
	tokens := html.NewTokenizer(strings.NewReader(original))
	var result strings.Builder
	for {
		kind := tokens.Next()
		switch kind {
		case html.ErrorToken:
			return strings.TrimSpace(result.String())
		case html.TextToken:
			result.WriteString(tokens.Token().Data)
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			tag := tokens.Token().Data
			if tag == "p" || tag == "div" || tag == "li" || (tag == "br" && kind != html.EndTagToken) {
				result.WriteByte('\n')
			}
			if kind == html.SelfClosingTagToken && (tag == "p" || tag == "div" || tag == "li") {
				result.WriteByte('\n')
			}
		}
	}
}
