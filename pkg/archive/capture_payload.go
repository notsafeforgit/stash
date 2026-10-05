package archive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

const CaptureStructureVersion = "post-capture-v1"

var captureFileFields = sourceKeys("num filename extension _url media_id file_id")
var captureTwitterFields = sourceKeys("width height type description duration bitrate source_id source_user sensitive_flags")
var captureInstagramFields = sourceKeys("date shortcode display_url video_url width height width_original height_original expires subscription audience tagged_users owner author audio_title audio_duration audio_user audio_artist audio_timestamps")
var captureProvenanceFields = sourceKeys("source_extractor_url subcategory nfo_path")

// RetainSourcePayload applies the versioned gallery-dl policy to a decoded
// copy. It never modifies an extractor's working metadata or fetches URLs.
func RetainSourcePayload(raw []byte) (json.RawMessage, error) {
	value, err := DecodeJSONObject(raw, MaxSourcePayloadBytes)
	if err != nil {
		return nil, err
	}
	return EncodeSourceJSON(retainProfiles(retainMedia(sanitizeSource(value), false), ""))
}

func splitCapturePayload(origin string, payload sourceObject) (sourceObject, sourceObject) {
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
		case "reddit", "bluesky", "tiktok", "coomer", "kemono", "tumblr":
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

func sourceProfileHash(namespace string, body []byte) string {
	sum := sha256.Sum256(append([]byte("stash-source-profile-json-v1\x00"+namespace+"\x00"), body...))
	return hex.EncodeToString(sum[:])
}

// PrepareRetainedProfile preserves an already-retained profile independently of
// its use in a capture. Catalogs can contain valid, currently unreferenced bodies.
func PrepareRetainedProfile(namespace string, raw []byte) (*models.SourceProfileBody, error) {
	value, err := DecodeJSONObject(raw, MaxSourcePayloadBytes)
	if err != nil || len(value) == 0 || !ValidAccountNamespace(namespace) {
		return nil, errors.New("invalid retained source profile")
	}
	body, err := EncodeSourceJSON(value)
	if err != nil || len(body) > MaxSourcePayloadBytes {
		return nil, errors.New("retained source profile exceeds payload limit")
	}
	return &models.SourceProfileBody{Hash: sourceProfileHash(namespace, body), Namespace: namespace, Body: body}, nil
}

func pointerMember(path, key string) string {
	return path + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

func extractProfiles(value interface{}, platform, part, path string, result *models.SourceCapturePayload, profiles map[string]models.SourceProfileBody) (interface{}, error) {
	switch value := value.(type) {
	case []interface{}:
		ret := make([]interface{}, len(value))
		for i, child := range value {
			var err error
			ret[i], err = extractProfiles(child, platform, part, path+"/"+strconv.Itoa(i), result, profiles)
			if err != nil {
				return nil, err
			}
		}
		return ret, nil
	case sourceObject:
		platform = sourceCategory(value, platform)
		ret := make(sourceObject)
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			child := value[key]
			site := platform
			if key == "_reddit" {
				site = "reddit"
			}
			location := pointerMember(path, key)
			if profile, ok := child.(sourceObject); ok && len(profile) > 0 && (site == "reddit" || site == "twitter") && profileRoles[key] {
				body, err := EncodeSourceJSON(profile)
				if err != nil {
					return nil, err
				}
				namespace := "native:" + site
				hash := sourceProfileHash(namespace, body)
				profiles[hash] = models.SourceProfileBody{Hash: hash, Namespace: namespace, Body: body}
				result.Refs = append(result.Refs, models.SourceProfileReference{Part: part, Path: location, Hash: hash})
				ret[key] = nil
			} else {
				var err error
				ret[key], err = extractProfiles(child, site, part, location, result, profiles)
				if err != nil {
					return nil, err
				}
			}
		}
		return ret, nil
	default:
		return value, nil
	}
}

// PrepareRetainedCapture partitions already-retained evidence without dropping
// anything. The catalog importer uses this boundary to preserve historical
// payload semantics. New gallery-dl inputs first pass RetainSourcePayload.
func PrepareRetainedCapture(origin, platform string, raw []byte) (*models.SourceCapturePayload, error) {
	value, err := DecodeJSONObject(raw, MaxSourcePayloadBytes)
	if err != nil {
		return nil, err
	}
	shared, patch := splitCapturePayload(origin, value)
	result := &models.SourceCapturePayload{Profiles: []models.SourceProfileBody{}, Refs: []models.SourceProfileReference{}}
	profiles := make(map[string]models.SourceProfileBody)
	for _, item := range []struct {
		part string
		body sourceObject
		out  *json.RawMessage
	}{{"shared", shared, &result.Shared}, {"patch", patch, &result.Patch}} {
		body, err := extractProfiles(item.body, platform, item.part, "", result, profiles)
		if err != nil {
			return nil, err
		}
		*item.out, err = EncodeSourceJSON(body)
		if err != nil {
			return nil, err
		}
	}
	hashes := make([]string, 0, len(profiles))
	for hash := range profiles {
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)
	for _, hash := range hashes {
		result.Profiles = append(result.Profiles, profiles[hash])
	}
	if len(result.Refs) > 1024 {
		return nil, errors.New("source capture exceeds profile reference limit")
	}
	restored, err := RestoreCapture(result)
	if err != nil {
		return nil, err
	}
	canonical, err := EncodeSourceJSON(value)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(canonical, restored) {
		return nil, errors.New("source capture partition did not preserve retained evidence")
	}
	return result, nil
}

func profileReferenceMember(root sourceObject, pointer string) (sourceObject, string, error) {
	if !strings.HasPrefix(pointer, "/") || len(pointer) > 8192 {
		return nil, "", errors.New("invalid profile reference path")
	}
	parts := strings.Split(pointer[1:], "/")
	var parent interface{} = root
	for i, part := range parts {
		for j := 0; j < len(part); j++ {
			if part[j] == '~' {
				j++
				if j >= len(part) || (part[j] != '0' && part[j] != '1') {
					return nil, "", errors.New("invalid profile reference escape")
				}
			}
		}
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		if i == len(parts)-1 {
			object, ok := parent.(sourceObject)
			if !ok || !hasSourceKey(object, part) || object[part] != nil {
				return nil, "", errors.New("profile reference would overwrite source data")
			}
			return object, part, nil
		}
		switch value := parent.(type) {
		case sourceObject:
			parent = value[part]
		case []interface{}:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(value) || strconv.Itoa(index) != part {
				return nil, "", errors.New("invalid profile reference array index")
			}
			parent = value[index]
		default:
			return nil, "", errors.New("invalid profile reference parent")
		}
	}
	return nil, "", errors.New("invalid profile reference")
}

// RestoreCapture reconstructs retained evidence and verifies profile checksums.
// It validates every reference against the unhydrated data before substituting
// any profile, preventing overlapping references from creating new target paths.
func RestoreCapture(payload *models.SourceCapturePayload) (json.RawMessage, error) {
	if payload == nil || len(payload.Profiles) > 1024 || len(payload.Refs) > 1024 {
		return nil, errors.New("invalid source capture payload")
	}
	shared, err := DecodeJSONObject(payload.Shared, MaxSourcePayloadBytes)
	if err != nil {
		return nil, err
	}
	patch, err := DecodeJSONObject(payload.Patch, MaxSourcePayloadBytes)
	if err != nil {
		return nil, err
	}
	profiles := make(map[string]models.SourceProfileBody)
	storedBytes := len(payload.Shared) + len(payload.Patch)
	if storedBytes > MaxSourcePayloadBytes {
		return nil, errors.New("source capture exceeds payload limit")
	}
	for _, profile := range payload.Profiles {
		body, err := DecodeJSONObject(profile.Body, MaxSourcePayloadBytes)
		if err != nil {
			return nil, err
		}
		canonical, err := EncodeSourceJSON(body)
		if err != nil {
			return nil, err
		}
		if !ValidAccountNamespace(profile.Namespace) || sourceProfileHash(profile.Namespace, canonical) != profile.Hash {
			return nil, errors.New("source profile checksum mismatch")
		}
		if _, duplicate := profiles[profile.Hash]; duplicate {
			return nil, errors.New("duplicate source profile body")
		}
		profile.Body = canonical
		profiles[profile.Hash] = profile
		storedBytes += len(canonical)
		if storedBytes > MaxSourcePayloadBytes {
			return nil, errors.New("source profile bodies exceed payload limit")
		}
	}
	type replacement struct {
		parent sourceObject
		key    string
		body   json.RawMessage
	}
	replacements := make([]replacement, 0, len(payload.Refs))
	seen := make(map[string]bool)
	referencedProfiles := make(map[string]bool)
	expandedBytes := len(payload.Shared) + len(payload.Patch)
	for _, ref := range payload.Refs {
		root := shared
		if ref.Part == "patch" {
			root = patch
		} else if ref.Part != "shared" {
			return nil, errors.New("invalid source profile reference part")
		}
		profile, ok := profiles[ref.Hash]
		if !ok || seen[ref.Part+":"+ref.Path] {
			return nil, errors.New("missing or duplicate source profile reference")
		}
		seen[ref.Part+":"+ref.Path] = true
		referencedProfiles[ref.Hash] = true
		expandedBytes += len(profile.Body) - len("null")
		if expandedBytes > MaxSourcePayloadBytes {
			return nil, errors.New("reconstructed source capture exceeds payload limit")
		}
		parent, key, err := profileReferenceMember(root, ref.Path)
		if err != nil {
			return nil, err
		}
		replacements = append(replacements, replacement{parent: parent, key: key, body: profile.Body})
	}
	if len(referencedProfiles) != len(profiles) {
		return nil, errors.New("unreferenced source profile body")
	}
	for _, replacement := range replacements {
		body, err := DecodeJSONObject(replacement.body, MaxSourcePayloadBytes)
		if err != nil {
			return nil, err
		}
		replacement.parent[replacement.key] = body
	}
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
	return EncodeSourceJSON(shared)
}
