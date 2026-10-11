package archive

import (
	"encoding/json"
	"strings"

	"github.com/stashapp/stash/pkg/jsonutil"
)

type sourceObject = map[string]interface{}

// DecodeJSONObject uses the same lossless decoder as source adapters.
func DecodeJSONObject(raw []byte, limit int) (map[string]interface{}, error) {
	return jsonutil.DecodeJSONObject(raw, limit)
}

// EncodeSourceJSON preserves the persisted json-v1 hash representation.
func EncodeSourceJSON(value interface{}) (json.RawMessage, error) {
	return jsonutil.EncodeSourceJSON(value)
}

func sourceKeys(names string) map[string]bool {
	ret := make(map[string]bool)
	for _, name := range strings.Fields(names) {
		ret[name] = true
	}
	return ret
}

func hasSourceKey(value sourceObject, key string) bool {
	_, ok := value[key]
	return ok
}
