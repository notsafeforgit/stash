package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

var capturedWebCreator = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,256}$`)
var capturedWebFileID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)
var capturedWebHost = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)

func capturedWebPost(data sourceObject, path, category string) (*models.SourcePostIdentifier, error) {
	namespace := "native:" + category
	switch category {
	case "tumblr":
		return capturedNumericPostReference(namespace, capturedFieldAt(data, path, "id"), capturedFieldAt(data, path, "id_string"))
	case "leakgallery":
		creator, _ := data["creator"].(string)
		if !capturedWebCreator.MatchString(creator) || creator == "unknown" {
			return nil, errors.New("LeakGallery post requires its explicit creator page")
		}
		id, err := capturedNumericPostReference(namespace, capturedFieldAt(data, path, "id"))
		if err != nil || id == nil {
			return nil, err
		}
		return capturedPostReference(namespace, capturedField{value: creator + "/" + id.Value, path: path})
	case "jpgfish", "imglike", "putmega":
		id, err := capturedPostReference(namespace, capturedFieldAt(data, path, "id"))
		if err == nil && id != nil && !capturedWebFileID.MatchString(id.Value) {
			return nil, errors.New("chevereto post requires its source file-page ID")
		}
		return id, err
	}
	return nil, errors.New("unsupported web-media source")
}

// A URL reference identifies the observed source attachment, independently of
// the output filename or a later fallback/converted download. Keep meaningful
// path and query spelling; a hostname, basename or list position is insufficient.
func capturedWebReference(raw interface{}) (string, error) {
	value, ok := raw.(string)
	if !ok || (!strings.HasPrefix(value, "https://") && !strings.HasPrefix(value, "http://")) || len(value) > 8192 || mirrorBadEscape.MatchString(value) ||
		strings.IndexFunc(value, func(c rune) bool { return c <= 32 || c >= 127 || c == '\\' }) >= 0 {
		return "", errors.New("source attachment requires an explicit HTTP URL")
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Port() != "" ||
		!capturedWebHost.MatchString(strings.ToLower(u.Hostname())) || !strings.EqualFold(u.Host, u.Hostname()) {
		return "", errors.New("source attachment requires an explicit HTTP URL")
	}
	_, rest, _ := strings.Cut(value, "://")
	path, _, _ := strings.Cut(strings.TrimPrefix(rest, u.Host), "?")
	path, _, _ = strings.Cut(path, "#")
	if path == "" {
		path = "/"
	}
	canonical := u.Scheme + "://" + strings.ToLower(u.Hostname()) + path
	if u.RawQuery != "" {
		canonical += "?" + u.RawQuery
	}
	digest := sha256.Sum256([]byte(canonical))
	return "url:" + hex.EncodeToString(digest[:]), nil
}

func capturedWebAlbum(data sourceObject, path, category string) (*CapturedAlbum, error) {
	if data["web_media"] == nil {
		return nil, nil
	}
	marker, ok := data["web_media"].(sourceObject)
	if !ok || len(marker) != 5 || marker["version"] != json.Number("1") {
		return nil, errors.New("invalid captured web-media membership")
	}
	post, err := capturedWebPost(data, path, category)
	if err != nil || post == nil || marker["post_id"] != post.Value {
		return nil, errors.New("captured web-media membership belongs to another post")
	}
	complete, completeOK := marker["complete"].(bool)
	album, albumOK := marker["album"].(bool)
	items, itemsOK := marker["items"].([]interface{})
	if !completeOK || !albumOK || !itemsOK || len(items) < 1 || len(items) > MaxManifestEntries ||
		(album && (category != "tumblr" || len(items) < 2)) || (category == "leakgallery" && complete) ||
		((category == "jpgfish" || category == "imglike" || category == "putmega") && (!complete || len(items) != 1)) {
		return nil, errors.New("source file/page evidence cannot establish this membership")
	}
	result := &CapturedAlbum{Policy: CapturedAlbumPolicy, Post: *post, EvidencePath: pointerMember(path, "web_media") + "/items",
		Manifest: models.SourceAttachmentManifestInput{Complete: complete, DeclaredAlbum: album}}
	if complete {
		count := len(items)
		result.Manifest.ExpectedCount = &count
	}
	seen := make(map[string]bool)
	for position, raw := range items {
		item, ok := raw.(sourceObject)
		if !ok || len(item) != 2 {
			return nil, errors.New("invalid captured web-media attachment")
		}
		kind, _ := item["kind"].(string)
		if kind != "image" && kind != "video" && kind != "unknown" {
			return nil, errors.New("invalid captured web-media kind")
		}
		ref, err := capturedWebReference(item["url"])
		if err != nil {
			return nil, err
		}
		if seen[ref] {
			return nil, errors.New("captured source lists the same attachment twice")
		}
		seen[ref] = true
		result.Manifest.Entries = append(result.Manifest.Entries, models.SourceAttachmentEntry{
			Position: position, Reference: models.SourcePostIdentifier{Namespace: post.Namespace, Value: ref}, MediaKind: kind})
	}
	return result, nil
}
