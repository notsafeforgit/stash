package api

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertMapJSONNumbersNestedArrays(t *testing.T) {
	row := map[string]interface{}{
		"savedFilterId": json.Number("123"),
		"extension": []interface{}{
			[]interface{}{json.Number("2.5"), json.Number("1e-7")},
			map[string]interface{}{"count": json.Number("1e3")},
		},
	}
	input := map[string]interface{}{"rows": []interface{}{row, "00123", true, nil}}
	want := map[string]interface{}{
		"rows": []interface{}{
			map[string]interface{}{
				"savedFilterId": int64(123),
				"extension": []interface{}{
					[]interface{}{2.5, 1e-7},
					map[string]interface{}{"count": float64(1000)},
				},
			},
			"00123", true, nil,
		},
	}

	result := convertMapJSONNumbers(input)
	require.Equal(t, want, result)
	assert.Equal(t, json.Number("123"), row["savedFilterId"])
	result["rows"].([]interface{})[0].(map[string]interface{})["savedFilterId"] = int64(456)
	assert.Equal(t, json.Number("123"), row["savedFilterId"], "conversion must not share mutable input containers")
}

func TestConvertMapJSONNumbersNumericForms(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input json.Number
		want  interface{}
	}{
		{"integer", "123", int64(123)},
		{"negative", "-123", int64(-123)},
		{"maximum_int64", "9223372036854775807", int64(math.MaxInt64)},
		{"fraction", "2.5", 2.5},
		{"small_exponent", "1e-7", 1e-7},
		{"integer_exponent", "1e3", float64(1000)},
		{"uppercase_exponent", "2E+3", float64(2000)},
		{"outside_int64", "100000000000000000000", float64(1e20)},
		{"outside_native_range", "1e999", json.Number("1e999")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := convertMapJSONNumbers(map[string]interface{}{"value": tt.input})
			assert.Equal(t, tt.want, result["value"])
		})
	}
}

func TestConvertMapJSONNumbersPreservesEmptyContainers(t *testing.T) {
	input := map[string]interface{}{
		"null":        nil,
		"nil_array":   []interface{}(nil),
		"empty_array": []interface{}{},
		"nil_map":     map[string]interface{}(nil),
		"empty_map":   map[string]interface{}{},
	}
	assert.Equal(t, input, convertMapJSONNumbers(input))
	assert.Nil(t, convertMapJSONNumbers(nil))
}
