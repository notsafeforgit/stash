package models

import (
	"errors"
	"fmt"
	"slices"
)

// This file implements the v2.5 saved-filter compatibility layer around
// FilterAST:
//
//   - decoding the v3 UI's compact AST encoding (used in URLs and, in the
//     transitional format, under the __filter_ast key of a saved filter's
//     object_filter map);
//   - converting a legacy v2.5 saved-filter criteria map into a FilterAST;
//   - flattening a FilterAST back into the v2.5 flat criteria map for
//     legacy API clients.
//
// Condition values throughout use the labeled saved-criterion shape
// ({"value": ..., "modifier": ...}) that v2.5 clients read and write.

// LegacyFilterASTKey is the object_filter key the v3 UI transitionally used
// to embed the compact-encoded AST alongside legacy criteria entries.
const LegacyFilterASTKey = "__filter_ast"

// Compact node kinds and index tables. These mirror COMPACT_OPERATORS /
// COMPACT_MODIFIERS in ui/v3/src/models/list-filter/filter-ast.ts; both
// lists are append-only.
const (
	compactKindGroup     = 0
	compactKindCondition = 1
)

var compactOperators = []FilterGroupOperator{
	FilterGroupOperatorAnd,
	FilterGroupOperatorOr,
}

var compactModifiers = []CriterionModifier{
	CriterionModifierEquals,
	CriterionModifierNotEquals,
	CriterionModifierGreaterThan,
	CriterionModifierLessThan,
	CriterionModifierIsNull,
	CriterionModifierNotNull,
	CriterionModifierIncludes,
	CriterionModifierIncludesAll,
	CriterionModifierExcludes,
	CriterionModifierMatchesRegex,
	CriterionModifierNotMatchesRegex,
	CriterionModifierBetween,
	CriterionModifierNotBetween,
}

// DecodeCompactFilterAST decodes a compact-encoded AST node tree into a
// FilterAST.
func DecodeCompactFilterAST(raw interface{}) (*FilterAST, error) {
	root, err := decodeCompactNode(raw)
	if err != nil {
		return nil, err
	}

	ret := &FilterAST{Root: root}
	if err := ret.Validate(); err != nil {
		return nil, err
	}

	return ret, nil
}

func decodeCompactNode(raw interface{}) (*FilterASTNode, error) {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil, errors.New("compact AST node must be an object")
	}

	kind, err := compactInt(m["k"])
	if err != nil {
		return nil, fmt.Errorf("compact AST node kind: %w", err)
	}

	switch kind {
	case compactKindGroup:
		opIdx, err := compactInt(m["o"])
		if err != nil {
			return nil, fmt.Errorf("compact AST group operator: %w", err)
		}
		if opIdx < 0 || opIdx >= len(compactOperators) {
			return nil, fmt.Errorf("unknown compact AST operator index %d", opIdx)
		}

		rawChildren, ok := m["c"].([]interface{})
		if !ok {
			return nil, errors.New("compact AST group children must be an array")
		}

		children := make([]*FilterASTNode, 0, len(rawChildren))
		for i, rawChild := range rawChildren {
			child, err := decodeCompactNode(rawChild)
			if err != nil {
				return nil, fmt.Errorf("compact AST group child %d: %w", i, err)
			}
			children = append(children, child)
		}

		return &FilterASTNode{
			Group: &FilterASTGroup{
				Operator: compactOperators[opIdx],
				Children: children,
			},
		}, nil

	case compactKindCondition:
		field, ok := m["f"].(string)
		if !ok || field == "" {
			return nil, errors.New("compact AST condition field must be a non-empty string")
		}

		value := make(map[string]interface{})
		if rawModifier, hasModifier := m["m"]; hasModifier {
			modIdx, err := compactInt(rawModifier)
			if err != nil {
				return nil, fmt.Errorf("compact AST condition modifier: %w", err)
			}
			if modIdx < 0 || modIdx >= len(compactModifiers) {
				return nil, fmt.Errorf("unknown compact AST modifier index %d", modIdx)
			}
			value["modifier"] = string(compactModifiers[modIdx])
		}
		if v, hasValue := m["v"]; hasValue {
			value["value"] = cloneASTValue(v)
		}
		if cf, ok := m["cf"].(string); ok && cf != "" {
			value["field"] = cf
		}

		return &FilterASTNode{
			Condition: &FilterASTCondition{
				Field: field,
				Value: value,
			},
		}, nil

	default:
		return nil, fmt.Errorf("unknown compact AST node kind %d", kind)
	}
}

func compactInt(raw interface{}) (int, error) {
	switch v := raw.(type) {
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case int64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("expected a number, got %T", raw)
	}
}

// FilterASTFromLegacySavedFilter converts a v2.5-style saved-filter criteria
// map into a FilterAST. It understands the transitional v3 form where a
// compact-encoded AST is embedded under the __filter_ast key alongside
// legacy entries, and splits hierarchical/multi values carrying legacy
// `excluded` entries into separate EXCLUDES conditions. Returns nil for a
// map with no criteria.
func FilterASTFromLegacySavedFilter(objectFilter map[string]interface{}) (*FilterAST, error) {
	if len(objectFilter) == 0 {
		return nil, nil
	}

	var astRoot *FilterASTNode
	var conditions []*FilterASTNode

	keys := make([]string, 0, len(objectFilter))
	for k := range objectFilter {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	for _, key := range keys {
		if key == LegacyFilterASTKey {
			ast, err := DecodeCompactFilterAST(objectFilter[key])
			if err != nil {
				return nil, fmt.Errorf("decoding %s: %w", LegacyFilterASTKey, err)
			}
			astRoot = ast.Root
			continue
		}

		entry, ok := objectFilter[key].(map[string]interface{})
		if !ok {
			// modifier-less legacy entries (custom_fields stores a bare
			// array) normalize into the {value} wrapper inside the AST;
			// FlatObjectFilter unwraps them again
			conditions = append(conditions, &FilterASTNode{
				Condition: &FilterASTCondition{
					Field: key,
					Value: map[string]interface{}{
						"value": cloneASTValue(objectFilter[key]),
					},
				},
			})
			continue
		}

		conditions = append(conditions, splitLegacyExcluded(key, entry)...)
	}

	if astRoot != nil {
		astRoot = splitExcludedInAST(astRoot)
	}

	root := mergeIntoANDRoot(astRoot, conditions)
	if root == nil {
		return nil, nil
	}

	ret := &FilterAST{Root: root}
	ret, err := ret.Normalize()
	if err != nil {
		return nil, err
	}

	return ret, nil
}

// mergeIntoANDRoot combines an optional existing AST root with additional
// condition nodes, AND-ing everything together.
func mergeIntoANDRoot(root *FilterASTNode, conditions []*FilterASTNode) *FilterASTNode {
	if root == nil && len(conditions) == 0 {
		return nil
	}

	if root == nil {
		if len(conditions) == 1 {
			return conditions[0]
		}
		return &FilterASTNode{
			Group: &FilterASTGroup{
				Operator: FilterGroupOperatorAnd,
				Children: conditions,
			},
		}
	}

	if len(conditions) == 0 {
		return root
	}

	if root.Group != nil && root.Group.Operator == FilterGroupOperatorAnd {
		return &FilterASTNode{
			Group: &FilterASTGroup{
				Operator: FilterGroupOperatorAnd,
				Children: append(slices.Clone(root.Group.Children), conditions...),
			},
		}
	}

	return &FilterASTNode{
		Group: &FilterASTGroup{
			Operator: FilterGroupOperatorAnd,
			Children: append([]*FilterASTNode{root}, conditions...),
		},
	}
}

// hierarchicalOrMultiFields lists criterion types whose legacy value carries
// `items` + `excluded`. Mirrors HIERARCHICAL_OR_MULTI_FIELDS in
// ui/v3/src/models/list-filter/migrate-legacy-excludes.ts.
var hierarchicalOrMultiFields = map[string]struct{}{
	"tags":              {},
	"scene_tags":        {},
	"performer_tags":    {},
	"studio_tags":       {},
	"studios":           {},
	"groups":            {},
	"containing_groups": {},
	"sub_groups":        {},
	"performers":        {},
	"galleries":         {},
	"scenes":            {},
	"parents":           {},
	"children":          {},
	"movies":            {},
}

// splitLegacyExcluded converts one saved criterion entry ({value, modifier})
// into condition nodes, splitting non-empty legacy `excluded` values into a
// separate EXCLUDES condition so the v3 builder can edit them.
func splitLegacyExcluded(field string, entry map[string]interface{}) []*FilterASTNode {
	single := func() []*FilterASTNode {
		return []*FilterASTNode{{
			Condition: &FilterASTCondition{
				Field: field,
				Value: cloneASTValue(entry),
			},
		}}
	}

	if _, ok := hierarchicalOrMultiFields[field]; !ok {
		return single()
	}

	inner, ok := entry["value"].(map[string]interface{})
	if !ok {
		return single()
	}

	items, _ := inner["items"].([]interface{})
	excluded, _ := inner["excluded"].([]interface{})
	if len(excluded) == 0 {
		return single()
	}

	rest := make(map[string]interface{})
	if depth, ok := inner["depth"]; ok {
		rest["depth"] = depth
	}
	if hm, ok := inner["hierarchyMode"]; ok {
		rest["hierarchyMode"] = hm
	}

	buildValue := func(items []interface{}) map[string]interface{} {
		v := map[string]interface{}{
			"items":    cloneASTValue(items),
			"excluded": []interface{}{},
		}
		for k, restV := range rest {
			v[k] = cloneASTValue(restV)
		}
		return v
	}

	excludesNode := &FilterASTNode{
		Condition: &FilterASTCondition{
			Field: field,
			Value: map[string]interface{}{
				"modifier": string(CriterionModifierExcludes),
				"value":    buildValue(excluded),
			},
		},
	}

	if len(items) == 0 {
		return []*FilterASTNode{excludesNode}
	}

	modifier, _ := entry["modifier"].(string)
	if modifier == "" {
		modifier = string(CriterionModifierIncludesAll)
	}

	includesNode := &FilterASTNode{
		Condition: &FilterASTCondition{
			Field: field,
			Value: map[string]interface{}{
				"modifier": modifier,
				"value":    buildValue(items),
			},
		},
	}

	return []*FilterASTNode{includesNode, excludesNode}
}

// splitExcludedInAST applies the legacy `excluded` split to every condition
// in the tree. A condition that splits into two nodes is replaced by an AND
// group at its position; Normalize flattens it into a same-operator parent.
func splitExcludedInAST(node *FilterASTNode) *FilterASTNode {
	if node == nil {
		return nil
	}

	if node.Condition != nil {
		entry, ok := node.Condition.Value.(map[string]interface{})
		if !ok {
			return node
		}

		split := splitLegacyExcluded(node.Condition.Field, entry)
		if len(split) == 1 {
			return split[0]
		}
		return &FilterASTNode{
			Group: &FilterASTGroup{
				Operator: FilterGroupOperatorAnd,
				Children: split,
			},
		}
	}

	if node.Group == nil {
		return node
	}

	children := make([]*FilterASTNode, 0, len(node.Group.Children))
	for _, child := range node.Group.Children {
		children = append(children, splitExcludedInAST(child))
	}

	return &FilterASTNode{
		Group: &FilterASTGroup{
			Operator: node.Group.Operator,
			Children: children,
		},
	}
}

// FlatObjectFilter renders the AST as the flat v2.5 saved-filter criteria
// map (criterion type -> {value, modifier}). The flat shape can only express
// an implicit AND of at most one criterion per type: nested groups and
// duplicate fields are dropped, and an OR root yields an empty map. The
// second return value reports whether the result is lossless.
func (a *FilterAST) FlatObjectFilter() (map[string]interface{}, bool) {
	if a == nil || a.Root == nil {
		return nil, true
	}

	ret := make(map[string]interface{})

	if a.Root.Condition != nil {
		if a.Root.Condition.Field == "names" {
			return ret, false
		}
		ret[a.Root.Condition.Field] = flatConditionValue(a.Root.Condition.Value)
		return ret, true
	}

	if a.Root.Group == nil {
		return ret, true
	}

	if a.Root.Group.Operator != FilterGroupOperatorAnd {
		// flattening an OR would change semantics; emit nothing
		return ret, false
	}

	lossless := true
	for _, child := range a.Root.Group.Children {
		if child == nil || child.Condition == nil {
			lossless = false
			continue
		}
		if child.Condition.Field == "names" {
			lossless = false
			continue
		}
		if _, exists := ret[child.Condition.Field]; exists {
			lossless = false
			continue
		}
		ret[child.Condition.Field] = flatConditionValue(child.Condition.Value)
	}

	return ret, lossless
}

// flatConditionValue renders a persisted condition value in the v2.5 saved
// shape. Modifier-less criteria (custom_fields) are stored bare in the v2.5
// shape, so a wrapper holding only "value" unwraps to its inner value.
func flatConditionValue(value interface{}) interface{} {
	if m, ok := value.(map[string]interface{}); ok && len(m) == 1 {
		if inner, ok := m["value"]; ok {
			return cloneASTValue(inner)
		}
	}
	return cloneASTValue(value)
}

// IsFlatRepresentable reports whether the AST round-trips losslessly through
// the flat v2.5 saved-filter shape.
func (a *FilterAST) IsFlatRepresentable() bool {
	_, lossless := a.FlatObjectFilter()
	return lossless
}
