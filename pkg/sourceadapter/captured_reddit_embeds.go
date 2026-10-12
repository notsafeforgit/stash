package sourceadapter

import (
	"errors"
	"regexp"

	"github.com/stashapp/stash/pkg/models"
)

var redditEmbeddedReferences = regexp.MustCompile(`!\[(?:img|video)\]\(([A-Za-z0-9]+)\)|https?://(?:i|preview|v)\.redd\.it/[^\s<>()[\]\\]+`)

// Inline media has a metadata map rather than gallery_data. Only references in
// the post body establish the order of multiple items; map order is not evidence.
func capturedRedditEmbeds(result *CapturedAlbum, data sourceObject, evidencePath string) (bool, error) {
	metadata, ok := data["media_metadata"].(sourceObject)
	if !ok || len(metadata) == 0 || data["comment"] != nil {
		return false, nil
	}
	if len(metadata) > MaxManifestEntries {
		return false, errors.New("too many Reddit embedded attachments")
	}
	for key, raw := range metadata {
		item, ok := raw.(sourceObject)
		if !ok || !redditMediaID.MatchString(key) {
			return false, errors.New("invalid Reddit embedded media identity")
		}
		if id, exists := item["id"]; exists && id != key {
			return false, errors.New("reddit embedded media identifiers disagree")
		}
		rendition, ok := item["s"].(sourceObject)
		if item["s"] != nil && !ok {
			return false, errors.New("invalid Reddit embedded media rendition")
		}
		for _, value := range []interface{}{rendition["u"], rendition["gif"], rendition["mp4"], item["dashUrl"], item["hlsUrl"]} {
			if ref, _ := redditMediaReference(value); ref != nil && ref.Value != key {
				return false, errors.New("reddit embedded media identifiers disagree")
			}
		}
	}
	var ordered []string
	seen := make(map[string]bool)
	body, _ := data["selftext"].(string)
	for _, match := range redditEmbeddedReferences.FindAllStringSubmatch(body, -1) {
		key := match[1]
		if key == "" {
			if ref, _ := redditMediaReference(match[0]); ref != nil {
				key = ref.Value
			}
		}
		if _, exists := metadata[key]; exists && !seen[key] {
			ordered = append(ordered, key)
			seen[key] = true
		}
	}
	if len(metadata) == 1 {
		for key := range metadata {
			ordered = []string{key}
		}
	}
	if len(ordered) != len(metadata) {
		return false, nil
	}
	count := len(ordered)
	result.EvidencePath = pointerMember(evidencePath, "media_metadata")
	result.Manifest = models.SourceAttachmentManifestInput{Complete: true, ExpectedCount: &count, DeclaredAlbum: count > 1}
	for position, key := range ordered {
		item := metadata[key].(sourceObject)
		kind := "unknown"
		switch item["e"] {
		case "Image":
			kind = "image"
		case "RedditVideo":
			kind = "video"
		}
		result.Manifest.Entries = append(result.Manifest.Entries, models.SourceAttachmentEntry{
			Position: position, Reference: models.SourcePostIdentifier{Namespace: "native:reddit", Value: key}, MediaKind: kind,
		})
	}
	return true, nil
}
