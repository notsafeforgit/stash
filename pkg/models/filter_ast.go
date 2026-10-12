package models

import (
	"errors"
	"fmt"
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
