package archive

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

var mirrorOriginalHost = regexp.MustCompile(`^(kemono|coomer)\.(cr|st|su|party)$`)
var mirrorInline = regexp.MustCompile(`src="(?:https?://(?:kemono\.cr|coomer\.st))?(/inline/[^\"]+|/[0-9a-f]{2}/[0-9a-f]{2}/[0-9a-f]{64}\.[^\"]+)`)
var mirrorImages = sourceKeys("jpg jpeg png webp avif jxl bmp")
var mirrorVideos = sourceKeys("mp4 mkv webm mov avi m4v wmv mpeg mpg ts")
var mirrorBadEscape = regexp.MustCompile(`%($|[^0-9a-fA-F]|[0-9a-fA-F]($|[^0-9a-fA-F]))`)

// Mirror paths qualify source identities; none of these values authorize local
// filesystem access or establish a verified content checksum.
func capturedMirrorPath(raw interface{}, category string) (string, error) {
	value, err := capturedIdentifierValue(capturedField{value: raw})
	if err != nil || value == "" || len(value) > 1024 || mirrorBadEscape.MatchString(value) {
		return "", errors.New("mirror attachment has no stable original source path")
	}
	value = strings.ReplaceAll(value, `\`, "/")
	if strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") {
		parsed, err := url.Parse(value)
		if err != nil || parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
			return "", errors.New("mirror attachment has no stable original source path")
		}
		host := mirrorOriginalHost.FindStringSubmatch(strings.ToLower(parsed.Hostname()))
		if len(host) != 3 || host[1] != category {
			return "", errors.New("mirror attachment belongs to another source host")
		}
		// Preserve the original escaped identifier, matching Python urlsplit.
		value = strings.TrimPrefix(value, parsed.Scheme+"://"+parsed.Host)
		if strings.HasPrefix(value, "/data/") {
			value = value[5:]
		}
	}
	if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || value == "/" || value == "/data" || strings.ContainsAny(value, "?#") {
		return "", errors.New("mirror attachment has no stable original source path")
	}
	for part := range strings.SplitSeq(value, "/") {
		if part == "." || part == ".." {
			return "", errors.New("mirror attachment has no stable original source path")
		}
	}
	if _, err := NormalizeAccountReference(models.AccountReference{Namespace: "mirror:" + category + ":source", Kind: "id", Value: value}); err != nil {
		return "", err
	}
	return value, nil
}

func capturedMirrorEntry(raw interface{}, category string) (sourceObject, error) {
	if raw == nil {
		return nil, nil
	}
	item, ok := raw.(sourceObject)
	if !ok {
		return nil, errors.New("invalid original mirror attachment")
	}
	if !sourceTruthy(item["path"]) {
		return nil, nil
	}
	key, err := capturedMirrorPath(item["path"], category)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(key[strings.LastIndex(key, ".")+1:])
	kind := "unknown"
	if mirrorImages[ext] {
		kind = "image"
	} else if mirrorVideos[ext] {
		kind = "video"
	}
	return sourceObject{"id": key, "kind": kind}, nil
}

func capturedMirrorMembership(data sourceObject, category string) ([]sourceObject, error) {
	attachments, ok := data["attachments"].([]interface{})
	if !ok || len(attachments) > MaxManifestEntries {
		return nil, errors.New("mirror post needs its bounded original attachment list")
	}
	items := make([]sourceObject, 0, len(attachments)+1)
	seen := make(map[string]bool)
	for _, raw := range attachments {
		item, err := capturedMirrorEntry(raw, category)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		if item != nil {
			seen[item["id"].(string)] = true
		}
	}
	if sourceTruthy(data["file"]) {
		item, err := capturedMirrorEntry(data["file"], category)
		if err != nil {
			return nil, err
		}
		if item == nil || !seen[item["id"].(string)] {
			items = append([]sourceObject{item}, items...)
			if item != nil {
				seen[item["id"].(string)] = true
			}
		}
	}
	content, ok := data["content"].(string)
	if !ok && sourceTruthy(data["content"]) {
		return nil, errors.New("mirror inline media requires original post text")
	}
	for {
		match := mirrorInline.FindStringSubmatchIndex(content)
		if match == nil {
			break
		}
		item, err := capturedMirrorEntry(sourceObject{"path": content[match[2]:match[3]]}, category)
		if err != nil {
			return nil, err
		}
		key := item["id"].(string)
		if !seen[key] {
			items = append(items, item)
			seen[key] = true
			if len(items) > MaxManifestEntries {
				return nil, errors.New("mirror source attachment list exceeds 4096 entries")
			}
		}
		content = content[match[1]:]
	}
	if len(items) > MaxManifestEntries {
		return nil, errors.New("mirror source attachment list exceeds 4096 entries")
	}
	return items, nil
}

func capturedMirrorAlbum(data sourceObject, path, category string) (*CapturedAlbum, error) {
	raw := data["mirror_media"]
	if raw == nil {
		return nil, nil
	}
	evidence, ok := raw.(sourceObject)
	if !ok || len(evidence) != 3 {
		return nil, errors.New("invalid mirror source attachment manifest")
	}
	version, versionOK := evidence["version"].(json.Number)
	declared, postOK := evidence["post"].(sourceObject)
	observed, itemsOK := evidence["items"].([]interface{})
	if !versionOK || version.String() != "1" || !postOK || len(declared) != 2 || !itemsOK {
		return nil, errors.New("invalid mirror source attachment manifest")
	}
	post, err := capturedMirrorPost(data, path, category)
	if err != nil || post == nil || declared["namespace"] != post.Namespace || declared["value"] != post.Value {
		return nil, errors.New("mirror attachment manifest belongs to another post")
	}
	items, err := capturedMirrorMembership(data, category)
	if err != nil {
		return nil, err
	}
	if len(observed) != len(items) {
		return nil, errors.New("mirror attachment manifest differs from its original post fields")
	}
	count := len(items)
	result := &CapturedAlbum{Policy: CapturedAlbumPolicy, Post: *post, EvidencePath: pointerMember(path, "mirror_media") + "/items",
		Manifest: models.SourceAttachmentManifestInput{DeclaredAlbum: count > 1, Complete: true, ExpectedCount: &count}}
	for position, item := range items {
		if item == nil {
			if observed[position] != nil {
				return nil, errors.New("mirror attachment manifest hides an unavailable source slot")
			}
			result.Manifest.Complete = false
			continue
		}
		stored, ok := observed[position].(sourceObject)
		if !ok || len(stored) != 2 || stored["id"] != item["id"] || stored["kind"] != item["kind"] {
			return nil, errors.New("mirror attachment manifest differs from its original post fields")
		}
		result.Manifest.Entries = append(result.Manifest.Entries, models.SourceAttachmentEntry{
			Position: position, Reference: models.SourcePostIdentifier{Namespace: post.Namespace, Value: item["id"].(string)}, MediaKind: item["kind"].(string)})
	}
	return result, nil
}
