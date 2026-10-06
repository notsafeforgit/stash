package sqlite

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stretchr/testify/require"
)

func TestAttachmentDownloadTransferQueryBoundsAnchorsBeforeScopeJoins(t *testing.T) {
	config.InitializeEmpty()
	db := NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "library.sqlite")))
	defer db.Close()
	raw, err := sql.Open("sqlite3", db.DatabasePath())
	require.NoError(t, err)
	defer raw.Close()
	rows, err := raw.Query("EXPLAIN QUERY PLAN "+attachmentDownloadTransfersSQL, uuid.NewString(), int64(1000000), 26, time.Now().UnixMilli())
	require.NoError(t, err)
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var description string
		require.NoError(t, rows.Scan(&id, &parent, &unused, &description))
		details = append(details, description)
	}
	require.NoError(t, rows.Err())
	plan := strings.Join(details, "\n")
	require.Contains(t, plan, "MATERIALIZE recent")
	require.Contains(t, plan, "SEARCH d USING INDEX attachment_download_history (attachment_uuid=? AND id<?)")
	require.Contains(t, plan, "SEARCH p USING COVERING INDEX attachment_download_transfer_phase")
	require.NotContains(t, plan, "SCAN source_attachment_downloads")
}
