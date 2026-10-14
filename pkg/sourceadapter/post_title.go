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
