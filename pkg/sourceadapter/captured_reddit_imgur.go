package sourceadapter

import (
	"encoding/json"
	"errors"
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

var capturedImgurAlbumLink = regexp.MustCompile(`^/(?:a|gallery)/(?:[^/?#]+-)?([A-Za-z0-9]{5,12})/?$`)
var capturedImgurMediaID = regexp.MustCompile(`^[A-Za-z0-9]{5,12}$`)

// Only this new evidence format opts into shared album storage. Historical
// captures without it retain their original partition and replay signatures.
func hasCapturedImgurAlbum(source sourceObject) bool {
	album, _ := source["album"].(sourceObject)
	marker, _ := album["imgur_media"].(sourceObject)
	version, ok := marker["version"].(json.Number)
	return source["category"] == "imgur" && ok && version.String() == "1"
}

func redditImgurAlbumID(raw interface{}) string {
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	value = html.UnescapeString(value)
	if _, err := capturedWebReference(value); err != nil {
		return ""
	}
	u, err := url.Parse(value)
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Hostname()) {
	case "imgur.com", "www.imgur.com", "m.imgur.com":
		if match := capturedImgurAlbumLink.FindStringSubmatch(u.EscapedPath()); match != nil {
			return match[1]
		}
	}
	return ""
}

// The worker preserves this bounded API list before gallery-dl removes it and
// emits individual files. num/count and output filenames are not membership.
func capturedRedditImgur(result *CapturedAlbum, post, source sourceObject) (bool, error) {
	album, _ := source["album"].(sourceObject)
	raw := album["imgur_media"]
	if raw == nil {
		return false, nil
	}
	invalid := errors.New("Imgur album membership does not match the linked Reddit album")
	var identity string
	for _, key := range []string{"url", "url_overridden_by_dest"} {
		if id := redditImgurAlbumID(post[key]); id != "" {
			if identity != "" && identity != id {
				return false, invalid
			}
			identity = id
		} else {
			if ref, _, _ := redditExternalReference(post[key]); ref != nil {
				return false, invalid
			}
			if ref, _ := redditMediaReference(post[key]); ref != nil {
				return false, invalid
			}
		}
	}
	marker, ok := raw.(sourceObject)
	if !ok || len(marker) != 2 || source["category"] != "imgur" || sourceTruthy(post["comment"]) || identity == "" ||
		album["id"] != identity || album["is_album"] != true {
		return false, invalid
	}
	version, versionOK := marker["version"].(json.Number)
	items, itemsOK := marker["items"].([]interface{})
	count, countOK := album["image_count"].(json.Number)
	expected, countErr := count.Int64()
	if !versionOK || version.String() != "1" || !itemsOK || len(items) < 1 || len(items) > MaxManifestEntries ||
		!countOK || countErr != nil || expected != int64(len(items)) {
		return false, invalid
	}
	result.EvidencePath = "/album/imgur_media/items"
	total := len(items)
	result.Manifest = models.SourceAttachmentManifestInput{DeclaredAlbum: true, Complete: true, ExpectedCount: &total}
	seen := make(map[string]bool, total)
	for position, raw := range items {
		entry, ok := raw.(sourceObject)
		if !ok || len(entry) != 2 {
			return false, errors.New("invalid Imgur album member")
		}
		id, _ := entry["id"].(string)
		kind, _ := entry["kind"].(string)
		if !capturedImgurMediaID.MatchString(id) || seen[id] || (kind != "image" && kind != "video" && kind != "unknown") {
			return false, errors.New("invalid Imgur album member")
		}
		seen[id] = true
		result.Manifest.Entries = append(result.Manifest.Entries, models.SourceAttachmentEntry{
			Position: position, Reference: models.SourcePostIdentifier{Namespace: "native:imgur", Value: id}, MediaKind: kind})
	}
	return true, nil
}
