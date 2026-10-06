package sqlite

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostSelectionProvenanceStartupRejectsUnrelatedCaptureOrManifest(t *testing.T) {
	for _, evidence := range []string{"capture", "manifest"} {
		t.Run(evidence, func(t *testing.T) {
			db, repo := postIdentityFixture(t)
			a := identityPost(t, repo, "native:reddit", "a")
			b := identityPost(t, repo, "native:reddit", "b")
			capture := postSelectionCapture(t, repo, a, 0, 1)
			other := postSelectionCapture(t, repo, b, 0, 1)
			selected := postSelectionApply(t, repo, a, capture.UUID, "pinned", "review")
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				guard := "post_attachment_decision_immutable"
				query := "UPDATE post_attachment_decisions SET capture_uuid=? WHERE uuid=?"
				args := []any{other.UUID, selected.Decision.UUID}
				if evidence == "manifest" {
					guard = "post_attachment_decision_manifest_immutable"
					query = `UPDATE post_attachment_decision_manifests SET manifest_uuid=(
SELECT manifest_uuid FROM source_capture_attachment_manifests WHERE capture_uuid=?) WHERE decision_uuid=?`
				}
				var definition string
				require.NoError(t, dbWrapper.Get(ctx, &definition, "SELECT sql FROM sqlite_schema WHERE name=?", guard))
				_, err := dbWrapper.Exec(ctx, "DROP TRIGGER "+guard)
				require.NoError(t, err)
				_, err = dbWrapper.Exec(ctx, query, args...)
				require.NoError(t, err)
				_, err = dbWrapper.Exec(ctx, definition)
				return err
			}))
			require.NoError(t, db.Close())
			before, err := os.ReadFile(db.DatabasePath())
			require.NoError(t, err)
			require.ErrorContains(t, db.Open(db.DatabasePath()), "another post identity")
			after, err := os.ReadFile(db.DatabasePath())
			require.NoError(t, err)
			require.Equal(t, before, after, "invalid provenance must fail read-only startup validation")
		})
	}
}

func TestPostSelectionProvenanceUsesDirectIndexedEvidenceLookups(t *testing.T) {
	_, repo := postIdentityFixture(t)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, test := range []struct {
			query string
			args  []any
		}{
			{selectionCaptureQuery, []any{"post", "capture"}},
			{selectionManifestHeadersQuery + "(?,?)", []any{"post", "first", "second"}},
		} {
			var rows []struct {
				ID, Parent, Notused int
				Detail              string
			}
			require.NoError(t, dbWrapper.Select(ctx, &rows, "EXPLAIN QUERY PLAN "+test.query, test.args...))
			plan := fmt.Sprint(rows)
			require.Len(t, rows, 3, plan)
			for _, alias := range []string{"selected", "owner"} {
				require.Regexp(t, "SEARCH "+alias+" USING [^}]*\\(post_uuid=\\?", plan)
			}
			require.NotContains(t, plan, "SCAN ")
			require.NotContains(t, plan, "TEMP B-TREE")
		}
		return nil
	}))
}
