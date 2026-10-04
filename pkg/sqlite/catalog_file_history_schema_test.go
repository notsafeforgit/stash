package sqlite_test

import (
	"os"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCatalogFileHistoryStartupRejectsReclassifiedReceiptWithoutWriting(t *testing.T) {
	f := fileHistoryImportFixture(t, 0, nil)
	require.EqualValues(t, 1, advanceFileHistory(t, f, 0).ReviewRecords)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	var guard string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='catalog_file_history_record_immutable'").Scan(&guard))
	_, err := raw.Exec("DROP TRIGGER catalog_file_history_record_immutable; UPDATE catalog_file_history_records SET reason='another classification' WHERE outcome='review';" + guard)
	require.NoError(t, err)
	before, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.ErrorIs(t, f.db.Open(f.db.DatabasePath()), models.ErrSourcePayloadCorrupt)
	after, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}
