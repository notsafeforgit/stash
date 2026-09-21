package sqlite

import (
	"context"
	"fmt"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestMediaBrowsePagesAfterAnalyze(t *testing.T) {
	db, err := sqlx.Open(sqlite3Driver, ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	for _, table := range []string{sceneTable, imageTable} {
		_, err = db.Exec(fmt.Sprintf(`CREATE TABLE %[1]s (id INTEGER PRIMARY KEY, title TEXT, created_at TEXT);
CREATE INDEX fork_%[1]s_created_at ON %[1]s(created_at, title)`, table))
		require.NoError(t, err)
		for i := 1; i <= 90; i++ {
			titles := []any{nil, "", "Scene 2", "scene 2", "Scene 10"}
			_, err = db.Exec("INSERT INTO "+table+" VALUES (?, ?, ?)", i, titles[i%len(titles)], fmt.Sprint(i%3))
			require.NoError(t, err)
		}
	}
	// Deployment runs ANALYZE, unlike a database with freshly created indexes.
	// Keep the production STAT4 build in the test gate too.
	_, err = db.Exec("ANALYZE")
	require.NoError(t, err)
	tx, err := db.Beginx()
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback()) }()
	ctx := context.WithValue(context.Background(), txnKey, tx)

	for _, table := range []string{sceneTable, imageTable} {
		for _, ast := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/ast=%t", table, ast), func(t *testing.T) {
				build := func(find *models.FindFilterType, title *models.StringCriterionInput) *queryBuilder {
					t.Helper()
					var query *queryBuilder
					var err error
					if table == sceneTable {
						store := &SceneStore{}
						if ast {
							query, err = store.makeASTQuery(ctx, nil, find)
						} else {
							query, err = store.makeQuery(ctx, &models.SceneFilterType{Title: title}, find)
						}
					} else {
						store := &ImageStore{}
						if ast {
							query, err = store.makeASTQuery(ctx, nil, find)
						} else {
							query, err = store.makeQuery(ctx, &models.ImageFilterType{Title: title}, find)
						}
					}
					require.NoError(t, err)
					return query
				}
				sort, perPage := "created_at", 13
				for _, direction := range models.AllSortDirectionEnum {
					for page := 1; page <= 7; page++ {
						find := &models.FindFilterType{Sort: &sort, Direction: &direction, PerPage: &perPage, Page: &page}
						query := build(find, nil)
						reference := *query
						reference.from += " NOT INDEXED"
						reference.browseIndex = ""
						wantIDs, wantCount, err := reference.executeFind(ctx)
						require.NoError(t, err)
						gotIDs, gotCount, err := query.executeFind(ctx)
						require.NoError(t, err)
						require.Equal(t, wantIDs, gotIDs)
						require.Equal(t, wantCount, gotCount)
						require.NotContains(t, query.toSQL(false), "INDEXED BY")
						var plans []struct {
							ID, Parent, Notused int
							Detail              string
						}
						require.NoError(t, tx.Select(&plans, "EXPLAIN QUERY PLAN "+query.toSQL(true)))
						require.Contains(t, fmt.Sprint(plans), "USING COVERING INDEX fork_"+table+"_created_at")
					}
				}
				// Selective criteria and unbounded exports must remain free to use
				// a different index. A forced scan here can hurt large libraries.
				if !ast {
					query := build(&models.FindFilterType{Sort: &sort}, &models.StringCriterionInput{Value: "Scene 2", Modifier: models.CriterionModifierEquals})
					require.NotContains(t, query.toSQL(true), "INDEXED BY")
					ids, err := query.findIDs(ctx)
					require.NoError(t, err)
					require.NotEmpty(t, ids)
				}
				all := models.PerPageAll
				query := build(&models.FindFilterType{Sort: &sort, PerPage: &all}, nil)
				require.NotContains(t, query.toSQL(true), "INDEXED BY")
				ids, err := query.findIDs(ctx)
				require.NoError(t, err)
				require.Len(t, ids, 90)
			})
		}
	}
}
