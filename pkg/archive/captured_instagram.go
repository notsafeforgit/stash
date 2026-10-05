package archive

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/stashapp/stash/pkg/models"
)

// The producer retains the original source membership before gallery-dl can
// filter unavailable items, add previews, or reverse the download order. Story
// containers identify accounts/highlights; their individual media are posts.
func capturedInstagramAlbum(data sourceObject, path string) (*CapturedAlbum, error) {
	raw, exists := data["instagram_media"]
	if !exists || raw == nil {
		return nil, nil
	}
	evidence, ok := raw.(sourceObject)
	if !ok {
		return nil, errors.New("invalid Instagram source attachment manifest")
	}
	for key := range evidence {
		if key != "version" && key != "post_id" && key != "album" && key != "items" && key != "container" {
			return nil, errors.New("unknown Instagram source attachment field")
		}
	}
	version, versionOK := evidence["version"].(json.Number)
	album, albumOK := evidence["album"].(bool)
	items, itemsOK := evidence["items"].([]interface{})
	if !versionOK || version.String() != "1" || !albumOK || !itemsOK || len(items) < 1 || len(items) > MaxManifestEntries {
		return nil, errors.New("invalid bounded Instagram source attachment manifest")
	}
	evidencePath := pointerMember(path, "instagram_media")
	post, err := capturedNumericPostReference("native:instagram", capturedFieldAt(evidence, evidencePath, "post_id"))
	if err != nil || post == nil {
		return nil, errors.New("captured Instagram source manifest has no numeric post identity")
	}
	var observed *models.SourcePostIdentifier
	if data["type"] == "story" || data["type"] == "highlight" {
		container, valid := evidence["container"].(sourceObject)
		if !valid || len(container) != 2 || container["type"] != data["type"] || album {
			return nil, errors.New("invalid Instagram story container")
		}
		containerID, err := capturedPostReference("native:instagram", capturedFieldAt(container, evidencePath+"/container", "id"), capturedFieldAt(data, path, "post_id"))
		if err != nil || containerID == nil || container["id"] == nil || data["post_id"] == nil {
			return nil, errors.New("captured Instagram story container identifiers disagree")
		}
		observed, err = capturedNumericPostReference("native:instagram", capturedFieldAt(data, path, "media_id"))
		if err != nil {
			return nil, err
		}
	} else {
		if evidence["container"] != nil {
			return nil, errors.New("ordinary Instagram post cannot claim a story container")
		}
		observed, err = capturedNumericPostReference("native:instagram", capturedFieldAt(data, path, "post_id"), capturedFieldAt(data, path, "sidecar_media_id"))
		if err != nil {
			return nil, err
		}
	}
	if observed == nil || *observed != *post {
		return nil, errors.New("captured Instagram attachment list belongs to another post")
	}
	count := len(items)
	result := &CapturedAlbum{Policy: CapturedAlbumPolicy, Post: *post, EvidencePath: evidencePath + "/items",
		Manifest: models.SourceAttachmentManifestInput{DeclaredAlbum: album, Complete: true, ExpectedCount: &count}}
	for position, raw := range items {
		if raw == nil {
			result.Manifest.Complete = false
			continue
		}
		item, valid := raw.(sourceObject)
		if !valid || len(item) != 2 || (item["kind"] != "image" && item["kind"] != "video" && item["kind"] != "unknown") {
			return nil, errors.New("invalid Instagram source attachment")
		}
		ref, err := capturedNumericPostReference("native:instagram", capturedFieldAt(item, result.EvidencePath+"/"+strconv.Itoa(position), "id"))
		if err != nil || ref == nil {
			return nil, errors.New("captured Instagram attachment has no numeric identity")
		}
		result.Manifest.Entries = append(result.Manifest.Entries, models.SourceAttachmentEntry{
			Position: position, Reference: *ref, MediaKind: item["kind"].(string)})
	}
	if !album && (count != 1 || len(result.Manifest.Entries) != 1 || result.Manifest.Entries[0].Reference != *post) {
		return nil, errors.New("a single Instagram post must identify its own media")
	}
	return result, nil
}
