package manager

import (
	"reflect"
	"testing"
)

func TestReconcileDefaultFilterConfigBackfillsBothFormats(t *testing.T) {
	uiConfig := map[string]interface{}{
		"defaultFilters": map[string]interface{}{
			"scenes": map[string]interface{}{
				"object_filter": map[string]interface{}{},
				"filter_ast": map[string]interface{}{
					"root": map[string]interface{}{
						"condition": map[string]interface{}{
							"field": "rating100",
							"value": map[string]interface{}{"value": 80, "modifier": "GREATER_THAN"},
						},
					},
				},
			},
		},
	}

	changed, err := reconcileDefaultFilterConfig(uiConfig)
	if err != nil {
		t.Fatalf("reconcileDefaultFilterConfig: %v", err)
	}
	if !changed {
		t.Fatal("expected configuration to change")
	}

	defaults := uiConfig["defaultFilters"].(map[string]interface{})
	scenes := defaults["scenes"].(map[string]interface{})
	if objectFilter := scenes["object_filter"].(map[string]interface{}); len(objectFilter) == 0 {
		t.Fatal("v2.5 object_filter projection was not added")
	}
	states := uiConfig[forkDefaultFilterStateKey].(map[string]interface{})
	if _, ok := states["scenes"]; !ok {
		t.Fatal("fork default-filter state was not added")
	}
}

func TestReconcileDefaultFilterConfigPreservesComplexAST(t *testing.T) {
	complexAST := map[string]interface{}{
		"root": map[string]interface{}{
			"group": map[string]interface{}{
				"operator": "OR",
				"children": []interface{}{
					map[string]interface{}{"condition": map[string]interface{}{"field": "rating100", "value": map[string]interface{}{"value": 80}}},
					map[string]interface{}{"condition": map[string]interface{}{"field": "resolution", "value": map[string]interface{}{"value": "FULL_HD"}}},
				},
			},
		},
	}
	oldProjection := map[string]interface{}{}
	upstreamEdit := map[string]interface{}{"favorite": map[string]interface{}{"value": true}}
	uiConfig := map[string]interface{}{
		"defaultFilters": map[string]interface{}{
			"scenes": map[string]interface{}{"object_filter": upstreamEdit},
		},
		forkDefaultFilterStateKey: map[string]interface{}{
			"scenes": map[string]interface{}{
				"filter_ast":           complexAST,
				"legacy_object_filter": oldProjection,
			},
		},
	}

	changed, err := reconcileDefaultFilterConfig(uiConfig)
	if err != nil {
		t.Fatalf("reconcileDefaultFilterConfig: %v", err)
	}
	if !changed {
		t.Fatal("expected configuration to change")
	}

	states := uiConfig[forkDefaultFilterStateKey].(map[string]interface{})
	state := states["scenes"].(map[string]interface{})
	gotAST, err := decodeDefaultFilterAST(state["filter_ast"])
	if err != nil {
		t.Fatalf("decode reconciled AST: %v", err)
	}
	wantAST, err := decodeDefaultFilterAST(complexAST)
	if err != nil {
		t.Fatalf("decode expected AST: %v", err)
	}
	if !reflect.DeepEqual(gotAST, wantAST) {
		t.Fatal("complex v3 AST was replaced")
	}
	if !reflect.DeepEqual(state["pending_legacy_object_filter"], upstreamEdit) {
		t.Fatal("upstream edit was not preserved as pending")
	}
	defaults := uiConfig["defaultFilters"].(map[string]interface{})
	scenes := defaults["scenes"].(map[string]interface{})
	activeAST, err := decodeDefaultFilterAST(scenes["filter_ast"])
	if err != nil {
		t.Fatalf("decode active AST: %v", err)
	}
	if !reflect.DeepEqual(activeAST, wantAST) {
		t.Fatal("canonical v3 AST was not restored to the active default")
	}
}

func TestReconcileDefaultFilterConfigImportsFlatUpstreamEdit(t *testing.T) {
	original := map[string]interface{}{"favorite": map[string]interface{}{"value": true}}
	upstreamEdit := map[string]interface{}{"rating100": map[string]interface{}{"value": 80, "modifier": "GREATER_THAN"}}
	uiConfig := map[string]interface{}{
		"defaultFilters": map[string]interface{}{
			"scenes": map[string]interface{}{"object_filter": upstreamEdit},
		},
		forkDefaultFilterStateKey: map[string]interface{}{
			"scenes": map[string]interface{}{
				"filter_ast": map[string]interface{}{
					"root": map[string]interface{}{
						"condition": map[string]interface{}{"field": "favorite", "value": map[string]interface{}{"value": true}},
					},
				},
				"legacy_object_filter": original,
			},
		},
	}

	changed, err := reconcileDefaultFilterConfig(uiConfig)
	if err != nil {
		t.Fatalf("reconcileDefaultFilterConfig: %v", err)
	}
	if !changed {
		t.Fatal("expected configuration to change")
	}

	states := uiConfig[forkDefaultFilterStateKey].(map[string]interface{})
	state := states["scenes"].(map[string]interface{})
	if !reflect.DeepEqual(state["legacy_object_filter"], upstreamEdit) {
		t.Fatal("upstream edit was not imported")
	}
	if _, exists := state["pending_legacy_object_filter"]; exists {
		t.Fatal("flat upstream edit was incorrectly left pending")
	}
}

func TestReconcileDefaultFilterConfigClearsDefault(t *testing.T) {
	uiConfig := map[string]interface{}{
		"defaultFilters": map[string]interface{}{"scenes": nil},
		forkDefaultFilterStateKey: map[string]interface{}{
			"scenes": map[string]interface{}{"filter_ast": map[string]interface{}{}},
		},
	}

	changed, err := reconcileDefaultFilterConfig(uiConfig)
	if err != nil {
		t.Fatalf("reconcileDefaultFilterConfig: %v", err)
	}
	if !changed {
		t.Fatal("expected configuration to change")
	}
	defaults := uiConfig["defaultFilters"].(map[string]interface{})
	if _, exists := defaults["scenes"]; exists {
		t.Fatal("cleared default was not removed")
	}
	if _, exists := uiConfig[forkDefaultFilterStateKey]; exists {
		t.Fatal("fork state for cleared default was not removed")
	}
}
