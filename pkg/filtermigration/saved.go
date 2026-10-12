// LEGACY-UPGRADE-ONLY: frozen historical filter codecs. See doc.go.
package filtermigration

import (
	"errors"
	"fmt"
	"github.com/stashapp/stash/pkg/models"
	"slices"
)

// Historical saved-filter import codecs producing models.FilterAST:
//
//   - decoding the old fork's compact AST encoding, stored under __filter_ast
//     in a saved filter's object_filter map;
//   - converting a legacy v2.5 saved-filter criteria map into a models.FilterAST;
//   - reconstructing historical flat criteria for migration reconciliation.
//
// Condition values throughout use the labeled saved-criterion shape
// ({"value": ..., "modifier": ...}) retained by native saved filters.

// LegacyFilterASTKey is the object_filter key the v3 UI transitionally used
// to embed the compact-encoded AST alongside legacy criteria entries.
const LegacyFilterASTKey = "__filter_ast"

// Historical compact node kinds and index tables. These indices are frozen for
// already-written upgrade inputs; do not synchronize them with the current UI.
const (
	compactKindGroup     = 0
	compactKindCondition = 1
)

var compactOperators = []models.FilterGroupOperator{
	models.FilterGroupOperatorAnd,
	models.FilterGroupOperatorOr,
}

var compactModifiers = []models.CriterionModifier{
	models.CriterionModifierEquals,
	models.CriterionModifierNotEquals,
	models.CriterionModifierGreaterThan,
	models.CriterionModifierLessThan,
	models.CriterionModifierIsNull,
	models.CriterionModifierNotNull,
	models.CriterionModifierIncludes,
	models.CriterionModifierIncludesAll,
	models.CriterionModifierExcludes,
	models.CriterionModifierMatchesRegex,
	models.CriterionModifierNotMatchesRegex,
	models.CriterionModifierBetween,
	models.CriterionModifierNotBetween,
}

// DecodeCompactFilterAST decodes a historical compact AST for upgrade input.
func DecodeCompactFilterAST(raw interface{}) (*models.FilterAST, error) {
	root, err := decodeCompactNode(raw)
	if err != nil {
		return nil, err
	}

	ret := &models.FilterAST{Root: root}
	if err := ret.Validate(); err != nil {
		return nil, err
	}

	return ret, nil
}

func decodeCompactNode(raw interface{}) (*models.FilterASTNode, error) {
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

		children := make([]*models.FilterASTNode, 0, len(rawChildren))
		for i, rawChild := range rawChildren {
			child, err := decodeCompactNode(rawChild)
			if err != nil {
				return nil, fmt.Errorf("compact AST group child %d: %w", i, err)
			}
			children = append(children, child)
		}

		return &models.FilterASTNode{
			Group: &models.FilterASTGroup{
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

		return &models.FilterASTNode{
			Condition: &models.FilterASTCondition{
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
// map into a models.FilterAST. It understands the transitional v3 form where a
// compact-encoded AST is embedded under the __filter_ast key alongside
// legacy entries, and splits hierarchical/multi values carrying legacy
// `excluded` entries into separate EXCLUDES conditions. Returns nil for a
// map with no criteria.
func FilterASTFromLegacySavedFilter(objectFilter map[string]interface{}) (*models.FilterAST, error) {
	if len(objectFilter) == 0 {
		return nil, nil
	}

	var astRoot *models.FilterASTNode
	var conditions []*models.FilterASTNode

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
			conditions = append(conditions, &models.FilterASTNode{
				Condition: &models.FilterASTCondition{
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

	ret := &models.FilterAST{Root: root}
	ret, err := ret.Normalize()
	if err != nil {
		return nil, err
	}

	return ret, nil
}

// mergeIntoANDRoot combines an optional existing AST root with additional
// condition nodes, AND-ing everything together.
func mergeIntoANDRoot(root *models.FilterASTNode, conditions []*models.FilterASTNode) *models.FilterASTNode {
	if root == nil && len(conditions) == 0 {
		return nil
	}

	if root == nil {
		if len(conditions) == 1 {
			return conditions[0]
		}
		return &models.FilterASTNode{
			Group: &models.FilterASTGroup{
				Operator: models.FilterGroupOperatorAnd,
				Children: conditions,
			},
		}
	}

	if len(conditions) == 0 {
		return root
	}

	if root.Group != nil && root.Group.Operator == models.FilterGroupOperatorAnd {
		return &models.FilterASTNode{
			Group: &models.FilterASTGroup{
				Operator: models.FilterGroupOperatorAnd,
				Children: append(slices.Clone(root.Group.Children), conditions...),
			},
		}
	}

	return &models.FilterASTNode{
		Group: &models.FilterASTGroup{
			Operator: models.FilterGroupOperatorAnd,
			Children: append([]*models.FilterASTNode{root}, conditions...),
		},
	}
}

// hierarchicalOrMultiFields is the historical set of criteria whose values
// carried `items` + `excluded`. New native criteria do not belong in this table.
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
func splitLegacyExcluded(field string, entry map[string]interface{}) []*models.FilterASTNode {
	single := func() []*models.FilterASTNode {
		return []*models.FilterASTNode{{
			Condition: &models.FilterASTCondition{
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

	excludesNode := &models.FilterASTNode{
		Condition: &models.FilterASTCondition{
			Field: field,
			Value: map[string]interface{}{
				"modifier": string(models.CriterionModifierExcludes),
				"value":    buildValue(excluded),
			},
		},
	}

	if len(items) == 0 {
		return []*models.FilterASTNode{excludesNode}
	}

	modifier, _ := entry["modifier"].(string)
	if modifier == "" {
		modifier = string(models.CriterionModifierIncludesAll)
	}

	includesNode := &models.FilterASTNode{
		Condition: &models.FilterASTCondition{
			Field: field,
			Value: map[string]interface{}{
				"modifier": modifier,
				"value":    buildValue(items),
			},
		},
	}

	return []*models.FilterASTNode{includesNode, excludesNode}
}

// splitExcludedInAST applies the legacy `excluded` split to every condition
// in the tree. A condition that splits into two nodes is replaced by an AND
// group at its position; Normalize flattens it into a same-operator parent.
func splitExcludedInAST(node *models.FilterASTNode) *models.FilterASTNode {
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
		return &models.FilterASTNode{
			Group: &models.FilterASTGroup{
				Operator: models.FilterGroupOperatorAnd,
				Children: split,
			},
		}
	}

	if node.Group == nil {
		return node
	}

	children := make([]*models.FilterASTNode, 0, len(node.Group.Children))
	for _, child := range node.Group.Children {
		children = append(children, splitExcludedInAST(child))
	}

	return &models.FilterASTNode{
		Group: &models.FilterASTGroup{
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
func FlatObjectFilter(a *models.FilterAST) (map[string]interface{}, bool) {
	if a == nil || a.Root == nil {
		return nil, true
	}

	ret := make(map[string]interface{})

	if a.Root.Condition != nil {
		if forkOnlySavedCriterion(a.Root.Condition.Field) {
			return ret, false
		}
		ret[a.Root.Condition.Field] = flatConditionValue(a.Root.Condition.Value)
		return ret, true
	}

	if a.Root.Group == nil {
		return ret, true
	}

	if a.Root.Group.Operator != models.FilterGroupOperatorAnd {
		// flattening an OR would change semantics; emit nothing
		return ret, false
	}

	lossless := true
	for _, child := range a.Root.Group.Children {
		if child == nil || child.Condition == nil {
			lossless = false
			continue
		}
		if forkOnlySavedCriterion(child.Condition.Field) {
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

func forkOnlySavedCriterion(field string) bool {
	return field == "names" || field == "cover_frame"
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
func IsFlatRepresentable(a *models.FilterAST) bool {
	_, lossless := FlatObjectFilter(a)
	return lossless
}

func cloneASTMap(input map[string]interface{}) map[string]interface{} {
	ret := make(map[string]interface{}, len(input))
	for k, v := range input {
		ret[k] = cloneASTValue(v)
	}

	return ret
}

func cloneASTSlice(input []interface{}) []interface{} {
	ret := make([]interface{}, 0, len(input))
	for _, value := range input {
		ret = append(ret, cloneASTValue(value))
	}

	return ret
}

func cloneASTValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return cloneASTMap(typed)
	case []interface{}:
		return cloneASTSlice(typed)
	default:
		return typed
	}
}
