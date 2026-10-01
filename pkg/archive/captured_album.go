package archive

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

const CapturedAlbumPolicy = "captured-album-v1"

// CapturedAlbum is source-list evidence, independent of local file availability.
// The caller records it against the matching post/capture before selecting it.
// CaptureUUID remains unset until that transaction assigns the actual capture.
type CapturedAlbum struct {
	Policy       string
	Post         models.SourcePostIdentifier
	EvidencePath string
	Manifest     models.SourceAttachmentManifestInput
}

func capturedPostReference(namespace string, fields ...capturedField) (*models.SourcePostIdentifier, error) {
	var id string
	for _, field := range fields {
		value, err := capturedIdentifierValue(field)
		if err != nil {
			return nil, err
		}
		if value == "" {
			continue
		}
		if _, err := NormalizeAccountReference(models.AccountReference{Namespace: namespace, Kind: "id", Value: value}); err != nil {
			return nil, err
		}
		if id != "" && id != value {
			return nil, errors.New("captured source identifiers disagree")
		}
		id = value
	}
	if id == "" {
		return nil, nil
	}
	return &models.SourcePostIdentifier{Namespace: namespace, Value: id}, nil
}

func capturedAlbumEntries(result *CapturedAlbum, raw interface{}, metadata sourceObject, reddit bool) error {
	items, ok := raw.([]interface{})
	if !ok || len(items) > MaxManifestEntries {
		return errors.New("captured attachment list must be an array of at most 4096 entries")
	}
	count := len(items)
	result.Manifest.ExpectedCount, result.Manifest.Complete = &count, true
	for position, raw := range items {
		if raw == nil {
			result.Manifest.Complete = false
			continue
		}
		item, ok := raw.(sourceObject)
		if !ok {
			return fmt.Errorf("invalid captured attachment at position %d", position)
		}
		path := result.EvidencePath + "/" + strconv.Itoa(position)
		var fields []capturedField
		if reddit {
			fields = []capturedField{capturedFieldAt(item, path, "media_id")}
		} else {
			fields = []capturedField{capturedFieldAt(item, path, "id_str"), capturedFieldAt(item, path, "id")}
		}
		ref, err := capturedPostReference(result.Post.Namespace, fields...)
		if err != nil {
			return err
		}
		if ref == nil {
			// An unidentified slot retains its position through the known count;
			// later entries are never shifted to hide the missing evidence.
			result.Manifest.Complete = false
			continue
		}
		kind := "unknown"
		if reddit {
			media, _ := metadata[ref.Value].(sourceObject)
			switch media["e"] {
			case "Image":
				kind = "image"
			case "RedditVideo":
				kind = "video"
			}
			// AnimatedImage can yield GIF or MP4; file-kind selection stays
			// separate from this source-list hint.
		} else {
			switch item["type"] {
			case "photo":
				kind = "image"
			case "video", "animated_gif":
				kind = "video"
			}
		}
		result.Manifest.Entries = append(result.Manifest.Entries, models.SourceAttachmentEntry{Position: position, Reference: *ref, MediaKind: kind})
	}
	return nil
}

// ExtractCapturedAlbum uses explicit source attachment lists. gallery-dl's num
// and count describe output files, which may include preview renditions or omit
// unavailable media; they cannot prove the full source list or its order.
// Unsupported or insufficient metadata returns nil, never a guessed gallery.
func ExtractCapturedAlbum(raw []byte) (*CapturedAlbum, error) {
	data, err := DecodeJSONObject(raw, MaxSourcePayloadBytes)
	if err != nil {
		return nil, err
	}
	path := ""
	category, _ := data["category"].(string)
	category = strings.ToLower(category)
	if parent, ok := data["_reddit"].(sourceObject); ok && sourceTruthy(parent["id"]) {
		data, path, category = parent, "/_reddit", "reddit"
	}
	result := &CapturedAlbum{Policy: CapturedAlbumPolicy}
	switch category {
	case "reddit":
		post, err := capturedPostReference("native:reddit", capturedFieldAt(data, path, "id"))
		if err != nil || post == nil {
			return nil, err
		}
		result.Post = *post
		// Reddit crossposts retain their own post identity while exposing the
		// source parent's ordered gallery. Keep the exact evidence location.
		if parents, ok := data["crosspost_parent_list"].([]interface{}); ok && len(parents) > 0 {
			parent, ok := parents[len(parents)-1].(sourceObject)
			if !ok {
				return nil, errors.New("invalid Reddit crosspost parent")
			}
			parentPath := pointerMember(path, "crosspost_parent_list") + "/" + strconv.Itoa(len(parents)-1)
			parentID, err := capturedPostReference("native:reddit", capturedFieldAt(parent, parentPath, "id"))
			if err != nil || parentID == nil {
				return nil, errors.New("captured Reddit crosspost parent has no valid identity")
			}
			if expected, ok := data["crosspost_parent"].(string); ok && expected != "" && expected != "t3_"+parentID.Value {
				return nil, errors.New("captured Reddit crosspost parent identity disagrees with its reference")
			}
			data, path = parent, parentPath
		}
		result.EvidencePath = pointerMember(path, "gallery_data")
		gallery := data["gallery_data"]
		if gallery == nil {
			declared, _ := data["is_gallery"].(bool)
			if !declared {
				return nil, nil
			}
			result.Manifest.DeclaredAlbum = true
			break
		}
		object, ok := gallery.(sourceObject)
		if !ok {
			return nil, errors.New("invalid Reddit gallery data")
		}
		result.Manifest.DeclaredAlbum = true
		result.EvidencePath = pointerMember(result.EvidencePath, "items")
		metadata, _ := data["media_metadata"].(sourceObject)
		if err := capturedAlbumEntries(result, object["items"], metadata, true); err != nil {
			return nil, err
		}
	case "twitter":
		root := data
		if legacy, ok := data["legacy"].(sourceObject); ok {
			data, path = legacy, "/legacy"
		}
		entities, ok := data["extended_entities"].(sourceObject)
		if !ok {
			return nil, nil
		}
		post, err := capturedPostReference("native:twitter", capturedFieldAt(root, "", "tweet_id"),
			capturedFieldAt(root, "", "rest_id"), capturedFieldAt(data, path, "id_str"))
		if err != nil || post == nil {
			return nil, err
		}
		result.Post, result.EvidencePath = *post, pointerMember(path, "extended_entities")+"/media"
		if err := capturedAlbumEntries(result, entities["media"], nil, false); err != nil {
			return nil, err
		}
	default:
		return nil, nil
	}
	result.Manifest, err = NormalizeAttachmentManifest(result.Manifest)
	if err != nil {
		return nil, err
	}
	return result, nil
}
