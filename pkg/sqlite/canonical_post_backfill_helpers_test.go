package sqlite

import (
	"context"
	"fmt"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

// Test-only bridge lets the external file/worker fixtures exercise the internal
// identity writer before the encompassing reviewed merge API is exposed.
func ConsolidatePostBackfillFixture(t *testing.T, repo models.Repository, source, destination, selectedCapture string) {
	t.Helper()
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, source, destination))
	if selectedCapture != "" {
		applyConsolidationSelection(t, repo, destination, selectedCapture, "pinned", merge.UUID)
	}
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var plan []struct {
			ID, Parent, Notused int
			Detail              string
		}
		require.NoError(t, dbWrapper.Select(ctx, &plan, "EXPLAIN QUERY PLAN "+sourceAlbumEvidenceQuery, 100, destination, 100))
		text := fmt.Sprint(plan)
		require.Contains(t, text, "source_post_identities_canonical")
		require.Contains(t, text, "SEARCH m USING INDEX source_media_evidence_post (post_uuid=?)")
		require.NotContains(t, text, "SCAN m")
		require.NotContains(t, text, "source_payloads")
		return nil
	}))
}
