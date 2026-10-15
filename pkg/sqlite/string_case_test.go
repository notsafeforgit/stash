package sqlite

import (
	"database/sql"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestStringCaseMatching(t *testing.T) {
	db, err := sql.Open(sqlite3Driver, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	for _, sensitive := range []bool{true, false} {
		for _, tc := range []struct {
			name, text, value          string
			modifier                   models.CriterionModifier
			matchingCase, ignoringCase bool
		}{
			{"includes", "A Bright day", "bright", models.CriterionModifierIncludes, false, true},
			{"excludes", "A Bright day", "bright", models.CriterionModifierExcludes, true, false},
			{"equals", "Bright", "bright", models.CriterionModifierEquals, false, true},
			{"not equals", "Bright", "bright", models.CriterionModifierNotEquals, true, false},
			{"regex", "Bright", "^bright$", models.CriterionModifierMatchesRegex, false, true},
			{"negative regex", "Bright", "^bright$", models.CriterionModifierNotMatchesRegex, true, false},
			{"unicode", "Été Αθήνα", "été αθήνα", models.CriterionModifierEquals, false, true},
			{"wildcards", "Bright\nDay", "Br%_ay", models.CriterionModifierEquals, true, true},
			{"quoted phrase", "One BRIGHT day", `"bright day"`, models.CriterionModifierIncludes, false, true},
			{"any word", "A Bright day", "nothing bright", models.CriterionModifierIncludes, false, true},
			{"exclude any word", "A Bright day", "nothing bright", models.CriterionModifierExcludes, true, false},
			{"regex characters are literal", "A [Bright].* day", "[bright].*", models.CriterionModifierIncludes, false, true},
			{"no regex injection", "Something else", ".*", models.CriterionModifierIncludes, false, false},
		} {
			t.Run(tc.name+"/"+map[bool]string{true: "sensitive", false: "insensitive"}[sensitive], func(t *testing.T) {
				f := &filterBuilder{}
				stringCriterionHandler(&models.StringCriterionInput{Value: tc.value, Modifier: tc.modifier, CaseSensitive: &sensitive}, "value")(t.Context(), f)
				require.NoError(t, f.getError())
				where, args := f.generateWhereClauses()
				var matched bool
				require.NoError(t, db.QueryRow("WITH candidate(value) AS (SELECT ?) SELECT "+where+" FROM candidate", append([]any{tc.text}, args...)...).Scan(&matched))
				want := tc.ignoringCase
				if sensitive {
					want = tc.matchingCase
				}
				require.Equal(t, want, matched)
			})
		}
		// NULL remains unknown for LIKE-style comparisons, including negation.
		for _, not := range []bool{false, true} {
			clause := stringLikeClause("value", "%text%", not, &sensitive)
			var result sql.NullBool
			require.NoError(t, db.QueryRow("WITH candidate(value) AS (SELECT NULL) SELECT "+clause.sql+" FROM candidate", clause.args...).Scan(&result))
			require.False(t, result.Valid)
		}
	}
}

func TestStringCaseASTDecodeAndIndependentPredicates(t *testing.T) {
	db, err := sql.Open(sqlite3Driver, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	for _, sensitive := range []bool{false, true} {
		c, err := decodeASTValue[models.StringCriterionInput](map[string]any{"value": "bright", "modifier": "INCLUDES", "case_sensitive": sensitive})
		require.NoError(t, err)
		require.Equal(t, &sensitive, c.CaseSensitive)
	}
	_, err = decodeASTValue[models.StringCriterionInput](map[string]any{"value": "bright", "modifier": "INCLUDES", "case_sensitive": "false"})
	require.Error(t, err)
	yes, no := true, false
	a := stringLikeClause("value", "bright", false, &yes)
	b := stringLikeClause("value", "bright", false, &no)
	var exact, folded bool
	require.NoError(t, db.QueryRow("WITH candidate(value) AS (SELECT 'Bright') SELECT "+a.sql+", "+b.sql+" FROM candidate", append(a.args, b.args...)...).Scan(&exact, &folded))
	require.False(t, exact)
	require.True(t, folded)
}

func TestStringCasePathsAndCustomEquality(t *testing.T) {
	db, err := sql.Open(sqlite3Driver, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	for _, sensitive := range []bool{false, true} {
		for _, not := range []bool{false, true} {
			clause := getPathSearchClauseCase("folder", "filename", "/media/été.mp4", false, not, &sensitive)
			var matched bool
			require.NoError(t, db.QueryRow("WITH candidate(folder,filename) AS (SELECT '/Media', 'Été.mp4') SELECT "+clause.sql+" FROM candidate", clause.args...).Scan(&matched))
			require.Equal(t, sensitive == not, matched)
		}
		for _, tc := range []struct {
			value any
			query []any
			want  bool
		}{
			{"Été_%", []any{"été_%"}, !sensitive},
			{"Été_ab", []any{"été_%"}, false},
			{"42", []any{42}, false},
			{42, []any{42}, true},
			{42, []any{"42"}, false},
			{"Bright", []any{"other", "bright"}, !sensitive},
			{"Bright", nil, false},
		} {
			clause := customFieldEqualityClause("value", tc.query, &sensitive)
			var matched bool
			require.NoError(t, db.QueryRow("WITH candidate(value) AS (SELECT ?) SELECT "+clause.sql+" FROM candidate", append([]any{tc.value}, clause.args...)...).Scan(&matched))
			require.Equal(t, tc.want, matched, "custom equality value=%v query=%v case=%v", tc.value, tc.query, sensitive)
		}
	}
}
