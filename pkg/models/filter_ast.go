package models

import (
	"errors"
	"fmt"
	"slices"
)

const maxFilterASTDepth = 4

type FilterGroupOperator string

const (
	FilterGroupOperatorAnd FilterGroupOperator = "AND"
	FilterGroupOperatorOr  FilterGroupOperator = "OR"
)

func (o FilterGroupOperator) IsValid() bool {
	switch o {
	case FilterGroupOperatorAnd, FilterGroupOperatorOr:
		return true
	default:
		return false
	}
}

type FilterAST struct {
	Root *FilterASTNode `json:"root,omitempty"`
}

func (a *FilterAST) Validate() error {
	if a == nil || a.Root == nil {
		return errors.New("filter AST must have a root node")
	}

	if err := a.Root.Validate(); err != nil {
		return err
	}

	return validateFilterASTDepth(a.Root, 0)
}

func validateFilterASTDepth(n *FilterASTNode, depth int) error {
	if n == nil || n.Group == nil {
		return nil
	}
	if depth > maxFilterASTDepth {
		return fmt.Errorf("filter AST group nesting exceeds maximum depth of %d", maxFilterASTDepth)
	}
	for i, child := range n.Group.Children {
		if err := validateFilterASTDepth(child, depth+1); err != nil {
			return fmt.Errorf("child %d: %w", i, err)
		}
	}
	return nil
}

func (a *FilterAST) Normalize() (*FilterAST, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}

	return &FilterAST{
		Root: a.Root.Normalize(),
	}, nil
}

func (a *FilterAST) ToObjectFilter() (map[string]interface{}, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}

	return a.Root.ToObjectFilter()
}

func FilterASTFromObjectFilter(input map[string]interface{}) (*FilterAST, error) {
	root, err := filterASTNodeFromObjectFilter(input)
	if err != nil {
		return nil, err
	}

	ret := &FilterAST{Root: root}
	if err := ret.Validate(); err != nil {
		return nil, err
	}

	return ret, nil
}

type FilterASTNode struct {
	Group     *FilterASTGroup     `json:"group,omitempty"`
	Condition *FilterASTCondition `json:"condition,omitempty"`
}

func (n *FilterASTNode) Validate() error {
	if n == nil {
		return errors.New("filter AST node cannot be nil")
	}

	hasGroup := n.Group != nil
	hasCondition := n.Condition != nil
	if hasGroup == hasCondition {
		return errors.New("filter AST node must contain exactly one of group or condition")
	}

	if n.Group != nil {
		return n.Group.Validate()
	}

	return n.Condition.Validate()
}

func (n *FilterASTNode) Normalize() *FilterASTNode {
	if n == nil {
		return nil
	}

	if n.Condition != nil {
		return &FilterASTNode{
			Condition: &FilterASTCondition{
				Field: n.Condition.Field,
				Value: cloneASTValue(n.Condition.Value),
			},
		}
	}

	return &FilterASTNode{
		Group: n.Group.Normalize(),
	}
}

func (n *FilterASTNode) ToObjectFilter() (map[string]interface{}, error) {
	if err := n.Validate(); err != nil {
		return nil, err
	}

	if n.Condition != nil {
		return map[string]interface{}{
			n.Condition.Field: cloneASTValue(n.Condition.Value),
		}, nil
	}

	return n.Group.ToObjectFilter()
}

type FilterASTGroup struct {
	Operator FilterGroupOperator `json:"operator"`
	Children []*FilterASTNode    `json:"children"`
}

func (g *FilterASTGroup) Validate() error {
	if g == nil {
		return errors.New("filter AST group cannot be nil")
	}

	if !g.Operator.IsValid() {
		return fmt.Errorf("invalid filter group operator %q", g.Operator)
	}

	if len(g.Children) == 0 {
		return errors.New("filter AST group must contain at least one child")
	}

	for i, child := range g.Children {
		if err := child.Validate(); err != nil {
			return fmt.Errorf("invalid filter AST child %d: %w", i, err)
		}
	}

	return nil
}

func (g *FilterASTGroup) Normalize() *FilterASTGroup {
	if g == nil {
		return nil
	}

	children := make([]*FilterASTNode, 0, len(g.Children))
	for _, child := range g.Children {
		normalized := child.Normalize()
		if normalized == nil {
			continue
		}

		// Flatten nested groups of the same operator into one group.
		if normalized.Group != nil && normalized.Group.Operator == g.Operator {
			children = append(children, normalized.Group.Children...)
			continue
		}

		children = append(children, normalized)
	}

	if len(children) == 1 {
		return &FilterASTGroup{
			Operator: g.Operator,
			Children: children,
		}
	}

	return &FilterASTGroup{
		Operator: g.Operator,
		Children: children,
	}
}

func (g *FilterASTGroup) ToObjectFilter() (map[string]interface{}, error) {
	if err := g.Validate(); err != nil {
		return nil, err
	}

	return groupChildrenToObjectFilter(g.Operator, g.Children)
}

type FilterASTCondition struct {
	Field string      `json:"field"`
	Value interface{} `json:"value"`
}

func (c *FilterASTCondition) Validate() error {
	if c == nil {
		return errors.New("filter AST condition cannot be nil")
	}

	if c.Field == "" {
		return errors.New("filter AST condition field must be non-empty")
	}

	return nil
}

func groupChildrenToObjectFilter(operator FilterGroupOperator, children []*FilterASTNode) (map[string]interface{}, error) {
	if len(children) == 0 {
		return nil, errors.New("cannot compile group with no children")
	}

	current, err := children[len(children)-1].ToObjectFilter()
	if err != nil {
		return nil, err
	}

	for i := len(children) - 2; i >= 0; i-- {
		left, err := children[i].ToObjectFilter()
		if err != nil {
			return nil, err
		}

		if canMergeObjectFilterNodes(left, current, operator) {
			current = mergeObjectFilterNodes(left, current, operator)
			continue
		}

		current = mergeObjectFilterNodes(left, map[string]interface{}{
			string(operator): current,
		}, operator)
	}

	return current, nil
}

func canMergeObjectFilterNodes(left map[string]interface{}, right map[string]interface{}, operator FilterGroupOperator) bool {
	// OR never merges sibling conditions at the same level because that would
	// change semantics from a branch to a leaf-level field union.
	if operator == FilterGroupOperatorOr {
		return false
	}

	return !mapsOverlap(left, right)
}

func mergeObjectFilterNodes(left map[string]interface{}, right map[string]interface{}, operator FilterGroupOperator) map[string]interface{} {
	ret := cloneASTMap(left)
	for k, v := range right {
		if existing, ok := ret[k]; ok {
			if k == string(operator) {
				ret[k] = mergeOperatorValues(existing, v)
				continue
			}
		}

		ret[k] = cloneASTValue(v)
	}

	return ret
}

func mergeOperatorValues(existing interface{}, incoming interface{}) interface{} {
	existingMap, existingOK := existing.(map[string]interface{})
	incomingMap, incomingOK := incoming.(map[string]interface{})
	if existingOK && incomingOK {
		return mergeObjectFilterNodes(existingMap, incomingMap, FilterGroupOperatorAnd)
	}

	return cloneASTValue(incoming)
}

func mapsOverlap(left map[string]interface{}, right map[string]interface{}) bool {
	for k := range left {
		if _, ok := right[k]; ok {
			return true
		}
	}

	return false
}

func filterASTNodeFromObjectFilter(input map[string]interface{}) (*FilterASTNode, error) {
	if len(input) == 0 {
		return nil, errors.New("object filter cannot be empty")
	}

	leafMap := make(map[string]interface{})
	var operatorKey string
	var operatorValue interface{}
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	for _, key := range keys {
		value := input[key]
		switch key {
		case string(FilterGroupOperatorAnd), string(FilterGroupOperatorOr):
			if operatorKey != "" {
				return nil, errors.New("object filter cannot contain more than one boolean operator at the same level")
			}
			operatorKey = key
			operatorValue = value
		case "NOT":
			return nil, errors.New("NOT filters are not yet supported by FilterAST")
		default:
			leafMap[key] = cloneASTValue(value)
		}
	}

	leftNode, err := filterASTLeafNode(leafMap)
	if err != nil {
		return nil, err
	}

	if operatorKey == "" {
		return leftNode, nil
	}

	subMap, ok := operatorValue.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s filter node must be an object", operatorKey)
	}

	rightNode, err := filterASTNodeFromObjectFilter(subMap)
	if err != nil {
		return nil, err
	}

	children := make([]*FilterASTNode, 0, 2)
	if leftNode != nil {
		children = append(children, leftNode)
	}
	children = append(children, rightNode)

	return &FilterASTNode{
		Group: &FilterASTGroup{
			Operator: FilterGroupOperator(operatorKey),
			Children: children,
		},
	}, nil
}

func filterASTLeafNode(input map[string]interface{}) (*FilterASTNode, error) {
	if len(input) == 0 {
		return nil, nil
	}

	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	children := make([]*FilterASTNode, 0, len(keys))
	for _, key := range keys {
		children = append(children, &FilterASTNode{
			Condition: &FilterASTCondition{
				Field: key,
				Value: cloneASTValue(input[key]),
			},
		})
	}

	if len(children) == 1 {
		return children[0], nil
	}

	return &FilterASTNode{
		Group: &FilterASTGroup{
			Operator: FilterGroupOperatorAnd,
			Children: children,
		},
	}, nil
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
