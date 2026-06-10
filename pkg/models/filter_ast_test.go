package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterASTValidateRejectsMissingRoot(t *testing.T) {
	t.Parallel()

	err := (&FilterAST{}).Validate()
	require.Error(t, err)
}

func TestFilterASTValidateRejectsInvalidNodeShape(t *testing.T) {
	t.Parallel()

	err := (&FilterAST{
		Root: &FilterASTNode{},
	}).Validate()
	require.Error(t, err)
}

func TestFilterASTNormalizeFlattensNestedGroups(t *testing.T) {
	t.Parallel()

	ast := &FilterAST{
		Root: &FilterASTNode{
			Group: &FilterASTGroup{
				Operator: FilterGroupOperatorAnd,
				Children: []*FilterASTNode{
					{
						Condition: &FilterASTCondition{
							Field: "tags",
							Value: map[string]interface{}{"value": []interface{}{"1"}},
						},
					},
					{
						Group: &FilterASTGroup{
							Operator: FilterGroupOperatorAnd,
							Children: []*FilterASTNode{
								{
									Condition: &FilterASTCondition{
										Field: "studios",
										Value: map[string]interface{}{"value": []interface{}{"2"}},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	normalized, err := ast.Normalize()
	require.NoError(t, err)
	require.NotNil(t, normalized.Root.Group)
	require.Len(t, normalized.Root.Group.Children, 2)
}

func TestFilterASTToObjectFilterSupportsNestedOrBranches(t *testing.T) {
	t.Parallel()

	ast := &FilterAST{
		Root: &FilterASTNode{
			Group: &FilterASTGroup{
				Operator: FilterGroupOperatorAnd,
				Children: []*FilterASTNode{
					{
						Group: &FilterASTGroup{
							Operator: FilterGroupOperatorOr,
							Children: []*FilterASTNode{
								{
									Condition: &FilterASTCondition{
										Field: "tags",
										Value: map[string]interface{}{"value": []interface{}{"1"}},
									},
								},
								{
									Condition: &FilterASTCondition{
										Field: "tags",
										Value: map[string]interface{}{"value": []interface{}{"2"}},
									},
								},
							},
						},
					},
					{
						Group: &FilterASTGroup{
							Operator: FilterGroupOperatorOr,
							Children: []*FilterASTNode{
								{
									Condition: &FilterASTCondition{
										Field: "studios",
										Value: map[string]interface{}{"value": []interface{}{"10"}},
									},
								},
								{
									Condition: &FilterASTCondition{
										Field: "studios",
										Value: map[string]interface{}{"value": []interface{}{"20"}},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	objectFilter, err := ast.ToObjectFilter()
	require.NoError(t, err)
	require.Equal(t,
		map[string]interface{}{
			"OR": map[string]interface{}{
				"tags": map[string]interface{}{"value": []interface{}{"2"}},
			},
			"tags": map[string]interface{}{"value": []interface{}{"1"}},
			"AND": map[string]interface{}{
				"OR": map[string]interface{}{
					"studios": map[string]interface{}{"value": []interface{}{"20"}},
				},
				"studios": map[string]interface{}{"value": []interface{}{"10"}},
			},
		},
		objectFilter,
	)
}

func TestFilterASTFromObjectFilterRoundTripsBooleanTree(t *testing.T) {
	t.Parallel()

	objectFilter := map[string]interface{}{
		"tags": map[string]interface{}{"value": []interface{}{"1"}},
		"AND": map[string]interface{}{
			"OR": map[string]interface{}{
				"studios": map[string]interface{}{"value": []interface{}{"20"}},
			},
			"studios": map[string]interface{}{"value": []interface{}{"10"}},
		},
	}

	ast, err := FilterASTFromObjectFilter(objectFilter)
	require.NoError(t, err)
	require.NotNil(t, ast.Root.Group)
	require.Equal(t, FilterGroupOperatorAnd, ast.Root.Group.Operator)
	require.Len(t, ast.Root.Group.Children, 2)
	require.NotNil(t, ast.Root.Group.Children[0].Condition)
	require.Equal(t, "tags", ast.Root.Group.Children[0].Condition.Field)
	require.NotNil(t, ast.Root.Group.Children[1].Group)
	require.Equal(t, FilterGroupOperatorOr, ast.Root.Group.Children[1].Group.Operator)

	compiled, err := ast.ToObjectFilter()
	require.NoError(t, err)
	require.Equal(t, map[string]interface{}{
		"tags":    map[string]interface{}{"value": []interface{}{"1"}},
		"studios": map[string]interface{}{"value": []interface{}{"10"}},
		"OR": map[string]interface{}{
			"studios": map[string]interface{}{"value": []interface{}{"20"}},
		},
	}, compiled)
}

func TestFilterASTFromObjectFilterRejectsNot(t *testing.T) {
	t.Parallel()

	_, err := FilterASTFromObjectFilter(map[string]interface{}{
		"NOT": map[string]interface{}{
			"tags": map[string]interface{}{"value": []interface{}{"1"}},
		},
	})
	require.Error(t, err)
}
