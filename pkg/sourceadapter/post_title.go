package sourceadapter

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/stashapp/stash/pkg/models"
)

var twitterDownloadTitle = regexp.MustCompile(`^[0-9]+(?:_[0-9]+)?(?:\.(?i:jpg|jpeg|png|webp|gif|avif|heic|heif|mp4|m4v|webm|mov|mkv))?$`)

// PostTitle derives a display heading without changing retained source metadata.
// Twitter has post text, not a separate title. Other sites keep their actual
// title when present and can use a caption excerpt when it is absent.
func PostTitle(platform string, metadata models.SourcePostMetadata, limit int) (string, bool) {
	value := func(p *string) string {
		if p == nil {
			return ""
		}
		return strings.TrimSpace(*p)
	}
	title, text := value(metadata.Title), value(metadata.OriginalText)
	if platform == "twitter" || platform == "x" {
		if text != "" {
			return postTextHeading(text, min(limit, 200))
		}
		if twitterDownloadTitle.MatchString(title) {
			title = ""
		}
	}
	if title != "" {
		runes := []rune(title)
		return string(runes[:min(len(runes), limit)]), len(runes) > limit
	}
	return postTextHeading(text, min(limit, 200))
}

func postTextHeading(text string, limit int) (string, bool) {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	runes := []rune(strings.Join(strings.Fields(line), " "))
	// Keep the first sentence of a long line. Periods within URLs, usernames
	// and decimal numbers are not sentence boundaries.
	for i, r := range runes {
		if strings.ContainsRune("。！？", r) || (strings.ContainsRune(".!?", r) && (i+1 == len(runes) || unicode.IsSpace(runes[i+1]))) {
			runes = runes[:i+1]
			break
		}
	}
	if len(runes) <= limit {
		return string(runes), false
	}
	end := limit
	for i := limit - 1; i >= limit/2; i-- {
		if unicode.IsSpace(runes[i]) {
			end = i
			break
		}
	}
	return strings.TrimSpace(string(runes[:end])), true
}

// MatchesPostText identifies copied source text and generated headings without
// treating a different custom title/description as replaceable.
func normalizedSourceText(value string) string {
	return strings.TrimSpace(strings.NewReplacer("\\r\\n", "\n", "\\n", "\n", "\r\n", "\n", "\r", "\n").Replace(value))
}

func MatchesPostText(value, field, platform string, metadata models.SourcePostMetadata) bool {
	value = normalizedSourceText(value)
	if value == "" {
		return false
	}
	if field == "details" {
		return metadata.OriginalText != nil && value == normalizedSourceText(*metadata.OriginalText)
	}
	if metadata.Title != nil && value == normalizedSourceText(*metadata.Title) {
		return true
	}
	if metadata.OriginalText != nil {
		caption := normalizedSourceText(*metadata.OriginalText)
		line, _, _ := strings.Cut(caption, "\n")
		if value == caption || value == strings.Join(strings.Fields(line), " ") {
			return true
		}
		// Earlier generated headings reserved one character for the ellipsis.
		// Match their long source prefix without accepting short custom labels.
		prefix := strings.TrimSuffix(value, "…")
		if prefix != value && len([]rune(prefix)) >= 100 && len([]rune(value)) <= 201 && strings.HasPrefix(strings.Join(strings.Fields(line), " "), prefix) {
			return true
		}
		metadata.OriginalText = &caption
	}
	title, truncated := PostTitle(platform, metadata, 512)
	if truncated {
		title += "…"
	}
	return value == title
}
