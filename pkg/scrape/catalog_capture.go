package scrape

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// LegacyCatalogJSON is the catalog's legacy-python-json-v1 checksum encoding,
// not the native source payload encoding. The snapshot keeps original bytes;
// reconstruction also has to reproduce Python's number/string normalization.
func LegacyCatalogJSON(value any, limit int) ([]byte, error) {
	var output bytes.Buffer
	var write func(any, int) error
	invalid := errors.New("invalid legacy catalog JSON value")
	write = func(value any, depth int) error {
		if depth > 64 || limit <= 0 || output.Len() > limit {
			return invalid
		}
		switch value := value.(type) {
		case nil:
			output.WriteString("null")
		case bool:
			output.WriteString(strconv.FormatBool(value))
		case string:
			if !utf8.ValidString(value) {
				return invalid
			}
			output.WriteByte('"')
			for _, char := range value {
				switch char {
				case '"', '\\':
					output.WriteByte('\\')
					output.WriteRune(char)
				case '\b':
					output.WriteString(`\b`)
				case '\f':
					output.WriteString(`\f`)
				case '\n':
					output.WriteString(`\n`)
				case '\r':
					output.WriteString(`\r`)
				case '\t':
					output.WriteString(`\t`)
				default:
					if char < 32 {
						const digits = "0123456789abcdef"
						output.WriteString(`\u00`)
						output.WriteByte(digits[char>>4])
						output.WriteByte(digits[char&15])
					} else {
						output.WriteRune(char)
					}
				}
				if output.Len() > limit {
					return invalid
				}
			}
			output.WriteByte('"')
		case int:
			output.WriteString(strconv.Itoa(value))
		case int64:
			output.WriteString(strconv.FormatInt(value, 10))
		case json.Number:
			if !strings.ContainsAny(value.String(), ".eE") {
				if len(value.String()) > 4300 {
					return invalid
				}
				number, ok := new(big.Int).SetString(value.String(), 10)
				if !ok {
					return invalid
				}
				output.WriteString(number.String())
			} else {
				number, err := value.Float64()
				if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
					return invalid
				}
				encoded := strconv.FormatFloat(number, 'e', -1, 64)
				exponent, err := strconv.Atoi(encoded[strings.LastIndexByte(encoded, 'e')+1:])
				if err != nil {
					return invalid
				}
				if exponent >= -4 && exponent < 16 {
					encoded = strconv.FormatFloat(number, 'f', -1, 64)
					if !strings.Contains(encoded, ".") {
						encoded += ".0"
					}
				}
				output.WriteString(encoded)
			}
		case []any:
			output.WriteByte('[')
			for i, child := range value {
				if i != 0 {
					output.WriteByte(',')
				}
				if err := write(child, depth+1); err != nil {
					return err
				}
			}
			output.WriteByte(']')
		case map[string]any:
			keys := make([]string, 0, len(value))
			for key := range value {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			output.WriteByte('{')
			for i, key := range keys {
				if i != 0 {
					output.WriteByte(',')
				}
				if err := write(key, depth+1); err != nil {
					return err
				}
				output.WriteByte(':')
				if err := write(value[key], depth+1); err != nil {
					return err
				}
			}
			output.WriteByte('}')
		default:
			return invalid
		}
		if output.Len() > limit {
			return invalid
		}
		return nil
	}
	if err := write(value, 0); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

type CatalogProfileLoader func(string) (map[string]any, error)

func CatalogProfile(row map[string]any) (map[string]any, error) {
	raw, ok := row["payload_json"].(string)
	if !ok || (row["platform"] != "reddit" && row["platform"] != "twitter") {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	value, err := archive.DecodeJSONObject([]byte(raw), archive.MaxSourcePayloadBytes)
	if err != nil || len(value) == 0 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	body, err := LegacyCatalogJSON([]any{"source-account-snapshot-v1", row["platform"], value}, archive.MaxSourcePayloadBytes)
	if err != nil || CatalogSnapshotSHA(body) != row["snapshot_id"] {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	return value, nil
}

func hydrateCatalogCapture(row map[string]any, column string, load CatalogProfileLoader) (map[string]any, int, error) {
	bad := models.ErrCatalogSnapshotInvalid
	raw, ok := row[column].(string)
	if !ok {
		return nil, 0, bad
	}
	payload, err := archive.DecodeJSONObject([]byte(raw), archive.MaxSourcePayloadBytes)
	if err != nil {
		return nil, 0, bad
	}
	refText := "[]"
	if value, exists := row["account_refs_json"]; exists {
		refText, ok = value.(string)
		if !ok {
			return nil, 0, bad
		}
	}
	refsObject, err := archive.DecodeJSONObject([]byte(`{"refs":`+refText+`}`), archive.MaxSourcePayloadBytes)
	if err != nil {
		return nil, 0, bad
	}
	refs, ok := refsObject["refs"].([]any)
	if !ok || len(refs) > 1024 {
		return nil, 0, bad
	}
	seen := map[string]bool{}
	expanded, err := LegacyCatalogJSON(payload, archive.MaxSourcePayloadBytes)
	if err != nil {
		return nil, 0, bad
	}
	expandedBytes := len(expanded)
	for _, item := range refs {
		ref, ok := item.([]any)
		if !ok || len(ref) != 2 {
			return nil, 0, bad
		}
		path, pathOK := ref[0].([]any)
		key, keyOK := ref[1].(string)
		if !pathOK || len(path) == 0 || !keyOK || !archive.ValidSHA256(key) || load == nil {
			return nil, 0, bad
		}
		encoded, err := LegacyCatalogJSON(path, 8192)
		if err != nil || seen[string(encoded)] {
			return nil, 0, bad
		}
		seen[string(encoded)] = true
		var parent any = payload
		for _, step := range path[:len(path)-1] {
			switch step := step.(type) {
			case string:
				object, ok := parent.(map[string]any)
				if !ok {
					return nil, 0, bad
				}
				parent = object[step]
			case json.Number:
				index, err := step.Int64()
				array, ok := parent.([]any)
				if !ok || err != nil || index < 0 || index >= int64(len(array)) {
					return nil, 0, bad
				}
				parent = array[index]
			default:
				return nil, 0, bad
			}
		}
		object, objectOK := parent.(map[string]any)
		name, nameOK := path[len(path)-1].(string)
		value, exists := object[name]
		if !objectOK || !nameOK || !exists || value != nil {
			return nil, 0, bad
		}
		profile, err := load(key)
		if err != nil {
			return nil, 0, err
		}
		if len(profile) == 0 {
			return nil, 0, bad
		}
		body, err := LegacyCatalogJSON(profile, archive.MaxSourcePayloadBytes)
		if err != nil {
			return nil, 0, bad
		}
		expandedBytes += len(body) - 4
		if expandedBytes > archive.MaxSourcePayloadBytes {
			return nil, 0, bad
		}
		object[name] = profile
	}
	return payload, len(refs), nil
}

type CatalogCapture struct {
	PostKey, ObservationID, CaptureID, Origin, Platform, CapturedAt string
	ExtractorVersion                                                *string
	Metadata                                                        models.SourcePostMetadata
	Payload, Header                                                 json.RawMessage
	PayloadSHA256                                                   string
	ProfileReferences                                               int
	Flat                                                            bool
}

// ReconstructCatalogCapture restores only actual detail captures. Callers pass
// nil detail only when the observation has no children; its summary timestamp
// must not become an extra capture alongside existing details.
func ReconstructCatalogCapture(observation, detail map[string]any, platform string, load CatalogProfileLoader) (*CatalogCapture, error) {
	bad := models.ErrCatalogSnapshotInvalid
	payload, refs, err := hydrateCatalogCapture(observation, "payload_json", load)
	if err != nil {
		return nil, err
	}
	row, idColumn := observation, "observation_id"
	if detail != nil {
		if detail["observation_id"] != observation["observation_id"] {
			return nil, bad
		}
		patch, patchRefs, err := hydrateCatalogCapture(detail, "payload_patch", load)
		if err != nil {
			return nil, err
		}
		refs += patchRefs
		for key, value := range patch {
			sharedParent, sharedOK := payload[key].(map[string]any)
			patchParent, patchOK := value.(map[string]any)
			if key == "_reddit" && sharedOK && patchOK {
				merged := make(map[string]any, len(sharedParent)+len(patchParent))
				for key, value := range sharedParent {
					merged[key] = value
				}
				for key, value := range patchParent {
					merged[key] = value
				}
				payload[key] = merged
			} else {
				payload[key] = value
			}
		}
		row, idColumn = detail, "capture_id"
	}
	result := &CatalogCapture{Platform: platform, ProfileReferences: refs, Flat: detail == nil}
	for _, field := range []struct {
		value  any
		target *string
	}{{observation["post_key"], &result.PostKey}, {observation["observation_id"], &result.ObservationID}, {row[idColumn], &result.CaptureID}, {observation["origin"], &result.Origin}, {row["captured_at"], &result.CapturedAt}} {
		text, ok := field.value.(string)
		if !ok || text == "" {
			return nil, bad
		}
		*field.target = text
	}
	if !journalTime(result.CapturedAt) || result.Platform == "" || len(result.Platform) > 128 || len(result.Origin) > 128 {
		return nil, bad
	}
	metadata := map[string]any{}
	for key, pointer := range map[string]**string{"title": &result.Metadata.Title, "original_text": &result.Metadata.OriginalText, "published_at": &result.Metadata.PublishedAt, "date_basis": &result.Metadata.DateBasis, "language": &result.Metadata.Language} {
		value, exists := observation[key]
		if !exists {
			return nil, bad
		}
		metadata[key] = value
		if value != nil {
			text, ok := value.(string)
			if !ok {
				return nil, bad
			}
			*pointer = &text
		}
	}
	if _, err := LegacyCatalogJSON(metadata, 262144); err != nil {
		return nil, bad
	}
	if row["extractor_version"] != nil {
		version, ok := row["extractor_version"].(string)
		if !ok || len(version) > 128 {
			return nil, bad
		}
		result.ExtractorVersion = &version
	}
	result.Payload, err = LegacyCatalogJSON(payload, archive.MaxSourcePayloadBytes)
	if err != nil {
		return nil, bad
	}
	result.PayloadSHA256 = CatalogSnapshotSHA(result.Payload)
	result.Header, err = LegacyCatalogJSON(map[string]any{
		"post_key": result.PostKey, "observation_id": result.ObservationID, "capture_id": result.CaptureID,
		"origin": result.Origin, "platform": result.Platform, "captured_at": result.CapturedAt,
		"extractor_version": row["extractor_version"], "metadata": metadata,
		"payload_sha256": result.PayloadSHA256, "payload_bytes": len(result.Payload),
		"profile_references": refs, "flat_observation": result.Flat,
	}, 1<<20)
	return result, err
}

func (c *CatalogCapture) NativeInput(post, id string) (models.SourceCaptureInput, error) {
	payload, err := archive.PrepareRetainedCapture(c.Origin, c.Platform, c.Payload)
	if err != nil {
		return models.SourceCaptureInput{}, err
	}
	captured, err := time.Parse(time.RFC3339Nano, c.CapturedAt)
	if err != nil {
		return models.SourceCaptureInput{}, err
	}
	return models.SourceCaptureInput{UUID: id, PostUUID: post, Origin: c.Origin, Platform: c.Platform,
		CapturedAt: captured, ExtractorVersion: c.ExtractorVersion, RetentionPolicy: "legacy-retained-v1",
		Metadata: c.Metadata, Payload: *payload}, nil
}
