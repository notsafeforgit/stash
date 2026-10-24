package sourceadapter

import (
	"encoding/json"
	"strings"

	"github.com/stashapp/stash/pkg/jsonutil"
)

var captureFileFields = sourceKeys("num filename extension _url media_id file_id")
var captureTwitterFields = sourceKeys("width height type description duration bitrate source_id source_user sensitive_flags")
var captureInstagramFields = sourceKeys("date shortcode display_url video_url width height width_original height_original expires subscription audience tagged_users owner author audio_title audio_duration audio_user audio_artist audio_timestamps")
var captureMirrorFields = sourceKeys("path name url hash type native_file_exclusion")
var captureBlueskyFields = sourceKeys("description width height count")
var captureTiktokFields = sourceKeys("image type title duration width height")
var captureProvenanceFields = sourceKeys("source_extractor_url subcategory nfo_path")

// RetainSourcePayload applies the versioned gallery-dl policy to a decoded
// copy. It never modifies an extractor's working metadata or fetches URLs.
func RetainSourcePayload(raw []byte) (json.RawMessage, error) {
	value, err := jsonutil.DecodeJSONObject(raw, MaxSourcePayloadBytes)
	if err != nil {
		return nil, err
	}
	return jsonutil.EncodeSourceJSON(retainProfiles(retainMedia(sanitizeSource(value), false), ""))
}

// SplitCapturePayload separates post data from per-file/source context using
// the original format rules. It does not reduce historical evidence. Core must
// check the reconstructed bytes before persisting the partition.
func SplitCapturePayload(origin string, payload sourceObject) (sourceObject, sourceObject) {
	keys := make(map[string]bool)
	switch origin {
	case "legacy-nfo":
		if !hasSourceKey(payload, "nfo_fields") {
			return payload, sourceObject{}
		}
		keys["nfo_path"] = true
	case "gallery-dl-enrichment":
		return payload, sourceObject{}
	case "gallery-dl":
		for key := range captureFileFields {
			keys[key] = true
		}
		for key := range captureProvenanceFields {
			keys[key] = true
		}
		if parent, ok := payload["_reddit"].(sourceObject); ok && sourceTruthy(parent["id"]) {
			shared, parentPatch := splitSourceFields(parent, keys)
			patch := make(sourceObject)
			for key, child := range payload {
				if key != "_reddit" {
					patch[key] = child
				}
			}
			if len(parentPatch) > 0 {
				patch["_reddit"] = parentPatch
			}
			return sourceObject{"_reddit": shared}, patch
		}
		category, _ := payload["category"].(string)
		switch category {
		case "twitter":
			for key := range captureTwitterFields {
				keys[key] = true
			}
		case "instagram":
			// Only the explicit new producer evidence opts into this partition.
			// Historical captures must retain their original signatures on replay
			// and when enrichment proofs are rederived at startup.
			if evidence, err := capturedInstagramAlbum(payload, ""); err == nil && evidence != nil {
				for key := range captureInstagramFields {
					keys[key] = true
				}
			}
		case "coomer", "kemono":
			if evidence, err := capturedMirrorAlbum(payload, "", category); err == nil && evidence != nil {
				for key := range captureMirrorFields {
					keys[key] = true
				}
			}
		case "bluesky", "tiktok":
			if evidence, err := capturedSocialAlbum(payload, "", category); err == nil && evidence != nil {
				fields := captureBlueskyFields
				if category == "tiktok" {
					fields = captureTiktokFields
				}
				for key := range fields {
					keys[key] = true
				}
			}
		case "tumblr", "jpgfish", "imglike", "putmega", "leakgallery", "erome":
			if evidence, err := capturedWebAlbum(payload, "", category); err == nil && evidence != nil {
				if category == "erome" {
					keys["web_media_id"] = true
				}
				for key := range sourceKeys("web_media_url photo source url width height native_file_exclusion") {
					keys[key] = true
				}
			} else if category != "tumblr" {
				return payload, sourceObject{}
			}
		case "reddit":
		default:
			return payload, sourceObject{}
		}
	default:
		return payload, sourceObject{}
	}
	return splitSourceFields(payload, keys)
}

func sourceTruthy(value interface{}) bool {
	switch value := value.(type) {
	case nil:
		return false
	case bool:
		return value
	case string:
		return value != ""
	case json.Number:
		mantissa := strings.Split(strings.ToLower(value.String()), "e")[0]
		return strings.Trim(mantissa, "-.0") != ""
	case sourceObject:
		return len(value) > 0
	case []interface{}:
		return len(value) > 0
	default:
		return true
	}
}

func splitSourceFields(payload sourceObject, keys map[string]bool) (sourceObject, sourceObject) {
	shared, patch := make(sourceObject), make(sourceObject)
	for key, child := range payload {
		if keys[key] {
			patch[key] = child
		} else {
			shared[key] = child
		}
	}
	return shared, patch
}

func pointerMember(path, key string) string {
	return path + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

// ProfilePartition identifies separately stored profile bodies in the source
// format. Core still owns hashing, reference validation and reconstruction.
func ProfilePartition(parent map[string]interface{}, inherited, key string) (platform, namespace string) {
	platform = sourceCategory(parent, inherited)
	if key == "_reddit" {
		platform = "reddit"
	}
	if (platform == "reddit" || platform == "twitter") && profileRoles[key] {
		namespace = "native:" + platform
	}
	return platform, namespace
}

// MergeCapturePayload applies the source format's historical partition rules.
// Callers must verify profile references before merging these decoded copies.
func MergeCapturePayload(shared, patch map[string]interface{}) {
	for key, value := range patch {
		if key == "_reddit" {
			parent, hasParent := shared[key].(sourceObject)
			parentPatch, hasPatch := value.(sourceObject)
			if hasParent && hasPatch {
				for name, child := range parentPatch {
					parent[name] = child
				}
				continue
			}
		}
		shared[key] = value
	}
}
