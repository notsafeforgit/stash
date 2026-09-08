package api

import "encoding/json"

// convertJSONNumbers prepares arbitrary GraphQL JSON values for persistence.
// json.Number marshals to a YAML string unless converted to a native number.
// Real strings, including legacy saved-filter IDs, keep their original type.
func convertJSONNumbers(value interface{}) interface{} {
	switch v := value.(type) {
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n
		}
		if n, err := v.Float64(); err == nil {
			return n
		}
		// Preserve an unrepresentable token rather than introducing a zero,
		// a saturated integer, or infinity when conversion fails.
		return v
	case map[string]interface{}:
		return convertMapJSONNumbers(v)
	case []interface{}:
		if v == nil {
			return v
		}
		ret := make([]interface{}, len(v))
		for i, item := range v {
			ret[i] = convertJSONNumbers(item)
		}
		return ret
	default:
		return v
	}
}
