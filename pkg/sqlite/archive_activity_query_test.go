package sqlite

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestArchiveActivityQueriesKeepOrderedIndexedRanges(t *testing.T) {
	_, repo := postIdentityFixture(t)
	page := models.ArchiveActivityPage{Before: 20, Limit: 5}
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		check := func(query string, args []any, index string) {
			t.Helper()
			var rows []struct {
				ID     int    `db:"id"`
				Parent int    `db:"parent"`
				Unused int    `db:"notused"`
				Detail string `db:"detail"`
			}
			require.NoError(t, dbWrapper.Select(ctx, &rows, "EXPLAIN QUERY PLAN "+query, args...))
			var parts []string
			for _, row := range rows {
				parts = append(parts, row.Detail)
			}
			plan := strings.Join(parts, "\n")
			require.Contains(t, plan, index)
			require.NotContains(t, plan, "TEMP B-TREE")
			require.NotContains(t, plan, "SCAN", "every older-page read has a bounded key range")
		}
		for filter, index := range map[models.ArchiveJobActivityFilter]string{
			{ArchiveActivityPage: page}:                                                      "INTEGER PRIMARY KEY",
			{ArchiveActivityPage: page, Kind: models.ArchiveJobVerifyMedia}:                  "archive_jobs_kind_history",
			{ArchiveActivityPage: page, State: "failed"}:                                     "archive_jobs_state_history",
			{ArchiveActivityPage: page, Kind: models.ArchiveJobVerifyMedia, State: "failed"}: "archive_jobs_list",
		} {
			query, args, err := jobActivityQuery(filter)
			require.NoError(t, err)
			check(query, args, index)
		}
		collection := uuid.NewString()
		for filter, index := range map[models.SourceRunActivityFilter]string{
			{ArchiveActivityPage: page}:                                                "INTEGER PRIMARY KEY",
			{ArchiveActivityPage: page, CollectionUUID: collection}:                    "source_runs_collection_page",
			{ArchiveActivityPage: page, State: "deferred"}:                             "source_runs_state_history",
			{ArchiveActivityPage: page, CollectionUUID: collection, State: "deferred"}: "source_runs_collection_state_history",
		} {
			query, args, err := runActivityQuery(filter)
			require.NoError(t, err)
			check(query, args, index)
		}
		return nil
	}))
}
