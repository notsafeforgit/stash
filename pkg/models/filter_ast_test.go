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
