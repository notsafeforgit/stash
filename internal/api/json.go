package api

// convertMapJSONNumbers converts JSON numbers throughout a map, including
// nested objects and arrays, without mutating the input.
func convertMapJSONNumbers(m map[string]interface{}) (ret map[string]interface{}) {
	if m == nil {
		return nil
	}

	ret = make(map[string]interface{})
	for k, v := range m {
		ret[k] = convertJSONNumbers(v)
	}

	return ret
}
