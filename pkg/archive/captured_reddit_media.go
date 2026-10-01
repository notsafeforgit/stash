package archive

import (
	"errors"
	"path"
	"regexp"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

var redditMediaID = regexp.MustCompile(`^[A-Za-z0-9]+$`)

// redditMediaReference recognizes service-owned attachment URLs, independently
// of rendition, query parameters and the local output filename. External links
// and preview thumbnails do not establish the attachment list of another site.
func redditMediaReference(value interface{}) (*models.SourcePostIdentifier, string) {
	u := sourceURL(value)
	if u == nil || u.User != nil || u.Port() != "" {
		return nil, ""
	}
	parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	var id, kind string
	switch u.Hostname() {
	case "i.redd.it", "preview.redd.it":
		if len(parts) != 1 {
			return nil, ""
		}
		ext := strings.ToLower(path.Ext(parts[0]))
		id = strings.TrimSuffix(parts[0], path.Ext(parts[0]))
		switch ext {
		case ".jpg", ".jpeg", ".png", ".webp", ".avif", ".jxl":
			kind = "image"
		case ".mp4", ".webm":
			kind = "video"
		case ".gif":
			kind = "unknown" // The downloader may produce GIF or a video encoding.
		default:
			return nil, ""
		}
	case "v.redd.it":
		if len(parts) < 1 || len(parts) > 2 {
			return nil, ""
		}
		id, kind = parts[0], "video"
	default:
		return nil, ""
	}
	if !redditMediaID.MatchString(id) {
		return nil, ""
	}
	return &models.SourcePostIdentifier{Namespace: "native:reddit", Value: id}, kind
}

// capturedRedditSingle uses the post's direct media link or explicit video
// fields. A downloader's per-file _url, num and filename cannot prove that a
// post has only one attachment, especially for comments and linked galleries.
func capturedRedditSingle(result *CapturedAlbum, data sourceObject, evidencePath string) (bool, error) {
	var selected *models.SourcePostIdentifier
	kind := ""
	add := func(value interface{}, fieldPath string) error {
		ref, hint := redditMediaReference(value)
		if ref == nil {
			return nil
		}
		if selected != nil && *selected != *ref {
			return errors.New("captured Reddit post media identifiers disagree")
		}
		if selected == nil {
			selected, kind, result.EvidencePath = ref, hint, fieldPath
		} else if hint != kind {
			// Distinct image/video encodings may describe the same animated
			// attachment; probing decides the final local library entity kind.
			kind = "unknown"
		}
		return nil
	}
	for _, key := range []string{"url", "url_overridden_by_dest"} {
		if err := add(data[key], pointerMember(evidencePath, key)); err != nil {
			return false, err
		}
	}
	for _, key := range []string{"media", "secure_media"} {
		media, _ := data[key].(sourceObject)
		video, _ := media["reddit_video"].(sourceObject)
		for _, field := range []string{"fallback_url", "dash_url", "hls_url"} {
			if err := add(video[field], pointerMember(evidencePath, key)+"/reddit_video/"+field); err != nil {
				return false, err
			}
		}
	}
	if selected == nil {
		return false, nil
	}
	count := 1
	result.Manifest = models.SourceAttachmentManifestInput{
		Complete: true, ExpectedCount: &count,
		Entries: []models.SourceAttachmentEntry{{Position: 0, Reference: *selected, MediaKind: kind}},
	}
	return true, nil
}
