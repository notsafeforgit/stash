//go:build integration

package sqlite_test

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestStringCaseASTQueries(t *testing.T) {
	runWithRollbackTxn(t, "case-sensitive text conditions", func(t *testing.T, ctx context.Context) {
		prefix := "string case AST fixture"
		for _, value := range []string{"BRIGHT", "bright"} {
			scene := models.NewScene()
			scene.Title, scene.Details = prefix, value
			scene.URLs = models.NewRelatedStrings([]string{"https://example.test/" + value, "https://example.test/unrelated"})
			require.NoError(t, db.Scene.Create(ctx, &scene, nil))
			require.NoError(t, db.Scene.SetCustomFields(ctx, scene.ID, models.CustomFieldsInput{Full: map[string]any{"text": value, "number": 42}}))
			performer := models.NewPerformer()
			performer.Name = prefix + " " + value
			performer.Aliases = models.NewRelatedPerformerAliases([]models.PerformerAlias{{Alias: value}, {Alias: "unrelated"}})
			require.NoError(t, db.Performer.Create(ctx, &models.CreatePerformerInput{Performer: &performer}))
		}
		for _, sensitive := range []bool{false, true} {
			for _, modifier := range []models.CriterionModifier{models.CriterionModifierIncludes, models.CriterionModifierExcludes, models.CriterionModifierEquals, models.CriterionModifierNotEquals, models.CriterionModifierMatchesRegex, models.CriterionModifierNotMatchesRegex} {
				negative := modifier == models.CriterionModifierExcludes || modifier == models.CriterionModifierNotEquals || modifier == models.CriterionModifierNotMatchesRegex
				want := 2
				if negative {
					want = 0
				}
				if sensitive {
					want = 1
				}
				for _, field := range []string{"details", "url", "custom_fields", "aliases", "names", "name"} {
					t.Run(field+"/"+string(modifier)+"/"+map[bool]string{true: "sensitive", false: "insensitive"}[sensitive], func(t *testing.T) {
						value := "bright"
						if field == "url" {
							value = "https://example.test/bright"
						}
						if field == "name" {
							value = prefix + " bright"
						}
						// Includes uses individual query words; quote a phrase when testing names.
						if field == "name" && (modifier == models.CriterionModifierIncludes || modifier == models.CriterionModifierExcludes) {
							value = `"` + value + `"`
						}
						var input any = models.StringCriterionInput{Value: value, Modifier: modifier, CaseSensitive: &sensitive}
						if field == "custom_fields" {
							input = []models.CustomFieldCriterionInput{{Field: "text", Value: []any{value}, Modifier: modifier, CaseSensitive: &sensitive}}
						}
						ast := &models.FilterAST{Root: &models.FilterASTNode{Condition: &models.FilterASTCondition{Field: field, Value: input}}}
						if field == "aliases" || field == "names" || field == "name" {
							_, count, err := db.Performer.QueryAST(ctx, ast, &models.FindFilterType{Q: &prefix})
							require.NoError(t, err)
							require.Equal(t, want, count)
						} else {
							_, count, err := db.Scene.QueryAST(ctx, ast, &models.FindFilterType{Q: &prefix})
							require.NoError(t, err)
							require.Equal(t, want, count)
						}
					})
				}
			}
		}
	})
}
