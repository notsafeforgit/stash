package models

import (
	"encoding/json"
	"reflect"
	"testing"
)

func jsonMap(t *testing.T, s string) map[string]interface{} {
	t.Helper()
	var ret map[string]interface{}
	if err := json.Unmarshal([]byte(s), &ret); err != nil {
		t.Fatalf("invalid test JSON: %v", err)
	}
	return ret
}

func TestDecodeCompactFilterAST(t *testing.T) {
	// AND(rating100 > 80, OR(performers includes [{id,label}], o_counter > 5))
	raw := jsonMap(t, `{
		"k": 0, "o": 0, "c": [
			{"k": 1, "f": "rating100", "m": 2, "v": {"value": 80}},
			{"k": 0, "o": 1, "c": [
				{"k": 1, "f": "performers", "m": 6, "v": {"items": [{"id": "3", "label": "P"}]}},
				{"k": 1, "f": "o_counter", "m": 2, "v": {"value": 5}}
			]}
		]
	}`)

	ast, err := DecodeCompactFilterAST(raw)
	if err != nil {
		t.Fatalf("DecodeCompactFilterAST: %v", err)
	}

	root := ast.Root
	if root.Group == nil || root.Group.Operator != FilterGroupOperatorAnd {
		t.Fatalf("expected AND root group, got %+v", root)
	}
	if len(root.Group.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(root.Group.Children))
	}

	cond := root.Group.Children[0].Condition
	if cond == nil || cond.Field != "rating100" {
		t.Fatalf("expected rating100 condition, got %+v", root.Group.Children[0])
	}
	condValue, ok := cond.Value.(map[string]interface{})
	if !ok {
		t.Fatalf("expected condition value map, got %T", cond.Value)
	}
	if condValue["modifier"] != "GREATER_THAN" {
		t.Errorf("expected GREATER_THAN modifier, got %v", condValue["modifier"])
	}

	or := root.Group.Children[1].Group
	if or == nil || or.Operator != FilterGroupOperatorOr {
		t.Fatalf("expected OR subgroup, got %+v", root.Group.Children[1])
	}
	perf := or.Children[0].Condition
	if perf == nil || perf.Field != "performers" {
		t.Fatalf("expected performers condition, got %+v", or.Children[0])
	}
	perfValue := perf.Value.(map[string]interface{})
	if perfValue["modifier"] != "INCLUDES" {
		t.Errorf("expected INCLUDES modifier, got %v", perfValue["modifier"])
	}
}

func TestDecodeCompactFilterASTErrors(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"unknown kind", `{"k": 7}`},
		{"unknown operator", `{"k": 0, "o": 99, "c": [{"k": 1, "f": "x"}]}`},
		{"unknown modifier", `{"k": 1, "f": "x", "m": 99}`},
		{"missing field", `{"k": 1, "m": 0}`},
		{"non-array children", `{"k": 0, "o": 0, "c": {}}`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := DecodeCompactFilterAST(jsonMap(t, c.raw)); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestFilterASTFromLegacySavedFilterFlat(t *testing.T) {
	// pure v2.5 flat criteria map
	objectFilter := jsonMap(t, `{
		"rating100": {"value": 80, "modifier": "GREATER_THAN"},
		"resolution": {"value": "FULL_HD", "modifier": "EQUALS"}
	}`)

	ast, err := FilterASTFromLegacySavedFilter(objectFilter)
	if err != nil {
		t.Fatalf("FilterASTFromLegacySavedFilter: %v", err)
	}

	flat, lossless := ast.FlatObjectFilter()
	if !lossless {
		t.Error("expected lossless flatten")
	}
	if !reflect.DeepEqual(flat, objectFilter) {
		t.Errorf("expected round-trip, got %v", flat)
	}
}

func TestFilterASTFromLegacySavedFilterEmbeddedAST(t *testing.T) {
	// transitional v3 form: compact AST + a sibling legacy entry
	objectFilter := jsonMap(t, `{
		"__filter_ast": {"k": 0, "o": 0, "c": [
			{"k": 1, "f": "rating100", "m": 2, "v": {"value": 80}}
		]},
		"organized": {"value": true, "modifier": "EQUALS"}
	}`)

	ast, err := FilterASTFromLegacySavedFilter(objectFilter)
	if err != nil {
		t.Fatalf("FilterASTFromLegacySavedFilter: %v", err)
	}

	flat, lossless := ast.FlatObjectFilter()
	if !lossless {
		t.Error("expected lossless flatten")
	}
	if len(flat) != 2 {
		t.Fatalf("expected 2 flat criteria, got %v", flat)
	}
	if _, ok := flat["rating100"]; !ok {
		t.Error("expected rating100 from embedded AST")
	}
	if _, ok := flat["organized"]; !ok {
		t.Error("expected organized from legacy sibling entry")
	}
}

func TestFilterASTFromLegacySavedFilterExcludedSplit(t *testing.T) {
	objectFilter := jsonMap(t, `{
		"tags": {
			"modifier": "INCLUDES_ALL",
			"value": {
				"items": [{"id": "1", "label": "keep"}],
				"excluded": [{"id": "2", "label": "drop"}],
				"depth": 0
			}
		}
	}`)

	ast, err := FilterASTFromLegacySavedFilter(objectFilter)
	if err != nil {
		t.Fatalf("FilterASTFromLegacySavedFilter: %v", err)
	}

	root := ast.Root
	if root.Group == nil || root.Group.Operator != FilterGroupOperatorAnd {
		t.Fatalf("expected AND root, got %+v", root)
	}
	if len(root.Group.Children) != 2 {
		t.Fatalf("expected includes+excludes conditions, got %d children", len(root.Group.Children))
	}

	includes := root.Group.Children[0].Condition.Value.(map[string]interface{})
	if includes["modifier"] != "INCLUDES_ALL" {
		t.Errorf("expected INCLUDES_ALL, got %v", includes["modifier"])
	}
	excludes := root.Group.Children[1].Condition.Value.(map[string]interface{})
	if excludes["modifier"] != "EXCLUDES" {
		t.Errorf("expected EXCLUDES, got %v", excludes["modifier"])
	}
	excludesItems := excludes["value"].(map[string]interface{})["items"].([]interface{})
	if len(excludesItems) != 1 {
		t.Fatalf("expected 1 excluded item, got %v", excludesItems)
	}

	// duplicate `tags` fields means this is no longer flat-representable
	if ast.IsFlatRepresentable() {
		t.Error("expected split result to be non-flat")
	}
}

func TestFilterASTFromLegacySavedFilterExcludedOnly(t *testing.T) {
	objectFilter := jsonMap(t, `{
		"performers": {
			"modifier": "INCLUDES",
			"value": {"items": [], "excluded": [{"id": "2", "label": "drop"}]}
		}
	}`)

	ast, err := FilterASTFromLegacySavedFilter(objectFilter)
	if err != nil {
		t.Fatalf("FilterASTFromLegacySavedFilter: %v", err)
	}

	cond := ast.Root.Condition
	if cond == nil {
		t.Fatalf("expected single condition root, got %+v", ast.Root)
	}
	value := cond.Value.(map[string]interface{})
	if value["modifier"] != "EXCLUDES" {
		t.Errorf("expected EXCLUDES rewrite, got %v", value["modifier"])
	}
}

func TestFilterASTFromLegacySavedFilterCustomFields(t *testing.T) {
	// custom_fields is stored bare (no {value, modifier} wrapper) in the
	// v2.5 saved shape
	objectFilter := jsonMap(t, `{
		"custom_fields": [
			{"field": "size", "modifier": "EQUALS", "value": ["L"]}
		]
	}`)

	ast, err := FilterASTFromLegacySavedFilter(objectFilter)
	if err != nil {
		t.Fatalf("FilterASTFromLegacySavedFilter: %v", err)
	}

	cond := ast.Root.Condition
	if cond == nil || cond.Field != "custom_fields" {
		t.Fatalf("expected custom_fields condition root, got %+v", ast.Root)
	}

	// normalized into the wrapper inside the AST
	wrapper, ok := cond.Value.(map[string]interface{})
	if !ok || wrapper["value"] == nil || len(wrapper) != 1 {
		t.Fatalf("expected {value} wrapper, got %#v", cond.Value)
	}

	// flattening unwraps back to the bare v2.5 shape
	flat, lossless := ast.FlatObjectFilter()
	if !lossless {
		t.Error("expected lossless flatten")
	}
	if !reflect.DeepEqual(flat, objectFilter) {
		t.Errorf("expected round-trip, got %#v", flat)
	}
}

func TestFilterASTFromLegacySavedFilterEmpty(t *testing.T) {
	ast, err := FilterASTFromLegacySavedFilter(map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ast != nil {
		t.Errorf("expected nil AST for empty map, got %+v", ast)
	}
}

func TestFlatObjectFilterLossy(t *testing.T) {
	// OR root flattens to nothing
	orAST := &FilterAST{
		Root: &FilterASTNode{
			Group: &FilterASTGroup{
				Operator: FilterGroupOperatorOr,
				Children: []*FilterASTNode{
					{Condition: &FilterASTCondition{Field: "a", Value: map[string]interface{}{"value": 1}}},
					{Condition: &FilterASTCondition{Field: "b", Value: map[string]interface{}{"value": 2}}},
				},
			},
		},
	}
	flat, lossless := orAST.FlatObjectFilter()
	if lossless || len(flat) != 0 {
		t.Errorf("expected lossy empty flatten of OR root, got %v (lossless=%v)", flat, lossless)
	}
	if orAST.IsFlatRepresentable() {
		t.Error("expected OR root to be non-flat")
	}

	// AND root with a nested group keeps the plain conditions only
	nestedAST := &FilterAST{
		Root: &FilterASTNode{
			Group: &FilterASTGroup{
				Operator: FilterGroupOperatorAnd,
				Children: []*FilterASTNode{
					{Condition: &FilterASTCondition{Field: "a", Value: map[string]interface{}{"value": 1}}},
					{Group: orAST.Root.Group},
				},
			},
		},
	}
	flat, lossless = nestedAST.FlatObjectFilter()
	if lossless {
		t.Error("expected lossy flatten with nested group")
	}
	if len(flat) != 1 {
		t.Errorf("expected only the plain condition, got %v", flat)
	}

	// nil AST flattens losslessly to nothing
	var nilAST *FilterAST
	flat, lossless = nilAST.FlatObjectFilter()
	if !lossless || flat != nil {
		t.Errorf("expected nil/lossless for nil AST, got %v (lossless=%v)", flat, lossless)
	}
}

func TestFlatObjectFilterOmitsV3AllNamesCriterion(t *testing.T) {
	ast := &FilterAST{Root: &FilterASTNode{Condition: &FilterASTCondition{
		Field: "names",
		Value: map[string]interface{}{
			"value":    "Jane",
			"modifier": "INCLUDES",
		},
	}}}

	flat, lossless := ast.FlatObjectFilter()
	if lossless {
		t.Fatal("all-names criterion was incorrectly marked v2.5-compatible")
	}
	if len(flat) != 0 {
		t.Fatalf("all-names criterion leaked into v2.5 projection: %#v", flat)
	}
}
