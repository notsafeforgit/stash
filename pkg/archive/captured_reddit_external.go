package archive

import (
	"html"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

var capturedRedgifsWatch = regexp.MustCompile(`^/(?:watch|ifr)/([A-Za-z]{1,256})/?$`)

// A linked clip has an explicit identity even when its external gallery's
// membership is unavailable. Only direct file links establish a single item.
func redditExternalReference(raw interface{}) (*models.SourcePostIdentifier, string, bool) {
	value, ok := raw.(string)
	if !ok {
		return nil, "", false
	}
	value = html.UnescapeString(value)
	id, err := capturedWebReference(value)
	if err != nil {
		return nil, "", false
	}
	u, err := url.Parse(value)
	if err != nil {
		return nil, "", false
	}
	switch strings.ToLower(u.Hostname()) {
	case "redgifs.com", "www.redgifs.com", "m.redgifs.com":
		if match := capturedRedgifsWatch.FindStringSubmatch(u.EscapedPath()); match != nil {
			return &models.SourcePostIdentifier{Namespace: "native:redgifs", Value: strings.ToLower(match[1])}, "unknown", false
		}
		return nil, "", false
	case "i.redd.it", "preview.redd.it", "v.redd.it":
		return nil, "", false
	}
	kind := "unknown"
	switch strings.ToLower(path.Ext(u.EscapedPath())) {
	case ".jpg", ".jpeg", ".png", ".webp", ".avif", ".jxl", ".bmp":
		kind = "image"
	case ".mp4", ".webm", ".mov", ".m4v", ".mkv":
		kind = "video"
	case ".gif":
	default:
		return nil, "", false
	}
	return &models.SourcePostIdentifier{Namespace: "native:reddit", Value: id}, kind, true
}
