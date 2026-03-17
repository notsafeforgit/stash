package migrations

import (
	"encoding/json"
	"testing"
)

func TestConvertObjectFilter(t *testing.T) {
	migrator := schema85Migrator{}
	input := `{
		"tags": {
			"modifier": "INCLUDES",
			"value": {
				"depth": 0,
				"excluded": [
					{
						"id": "27",
						"label": "JAV Actress"
					}
				],
				"items": [
					{
						"id": "28",
						"label": "xyz"
					}
				]
			}
		}
	}`

	expected := `{"tags":{"depth":0,"excludes":["27"],"modifier":"INCLUDES","value":["28"]}}`

	output, err := migrator.convertObjectFilter([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var outMap, expMap map[string]interface{}
	json.Unmarshal(output, &outMap)
	json.Unmarshal([]byte(expected), &expMap)

	outJSON, _ := json.Marshal(outMap)
	expJSON, _ := json.Marshal(expMap)

	if string(outJSON) != string(expJSON) {
		t.Errorf("expected %s, got %s", string(expJSON), string(outJSON))
	}
}

func TestConvertObjectFilterPrimitive(t *testing.T) {
	migrator := schema85Migrator{}
	input := `{
		"galleries": {
			"modifier": "INCLUDES",
			"value": {
				"excluded": [],
				"items": [
					{
						"id": "1",
						"label": "gallery 1"
					}
				]
			}
		},
		"has_markers": {
			"modifier": "EQUALS",
			"value": "true"
		}
	}`

	expected := `{"galleries":{"excludes":[],"modifier":"INCLUDES","value":["1"]},"has_markers":{"modifier":"EQUALS","value":"true"}}`

	output, err := migrator.convertObjectFilter([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var outMap, expMap map[string]interface{}
	json.Unmarshal(output, &outMap)
	json.Unmarshal([]byte(expected), &expMap)

	outJSON, _ := json.Marshal(outMap)
	expJSON, _ := json.Marshal(expMap)

	if string(outJSON) != string(expJSON) {
		t.Errorf("expected %s, got %s", string(expJSON), string(outJSON))
	}
}
