package archive

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"unicode/utf8"
)

// DecodeJSONObject rejects representations that encoding/json would otherwise
// silently change: duplicate object keys, invalid UTF-8 and unpaired UTF-16
// surrogate escapes. Numbers retain their JSON spelling, including large IDs.
func DecodeJSONObject(raw []byte, limit int) (map[string]interface{}, error) {
	if limit <= 0 || len(raw) > limit || !utf8.Valid(raw) || !json.Valid(raw) || !validJSONSurrogates(raw) {
		return nil, errors.New("invalid or oversized source JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decodeSourceJSON(decoder, 0)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]interface{})
	if !ok {
		return nil, errors.New("source JSON must be an object")
	}
	return object, nil
}

// EncodeSourceJSON encodes the native json-v1 trees: maps, arrays, strings,
// json.Number, booleans and null. Map keys sort lexically, strings use UTF-8
// without HTML escaping (U+2028/U+2029 stay escaped), and number tokens stay
// exact. Hash inputs must use trees, not structs with field-order serialization.
// This is not RFC 8785; producers hash durable event bytes, not these bytes.
func EncodeSourceJSON(value interface{}) (json.RawMessage, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte{'\n'}), nil
}

func decodeSourceJSON(decoder *json.Decoder, depth int) (interface{}, error) {
	if depth > 64 {
		return nil, errors.New("source JSON exceeds nesting limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		object := make(map[string]interface{})
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, errors.New("invalid source JSON object key")
			}
			if _, exists := object[name]; exists {
				return nil, errors.New("source JSON contains duplicate object keys")
			}
			object[name], err = decodeSourceJSON(decoder, depth+1)
			if err != nil {
				return nil, err
			}
		}
		_, err := decoder.Token()
		return object, err
	case json.Delim('['):
		array := make([]interface{}, 0)
		for decoder.More() {
			value, err := decodeSourceJSON(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		_, err := decoder.Token()
		return array, err
	default:
		return token, nil
	}
}

func validJSONSurrogates(raw []byte) bool {
	// json.Valid already establishes string and escape boundaries. Skip escaped
	// backslashes so a literal "\\ud800" is not mistaken for a Unicode escape.
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if raw[i] != 'u' {
			continue
		}
		code, _ := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		i += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return false
		}
		if code >= 0xd800 && code <= 0xdbff {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}
