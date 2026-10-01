package scrape

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestScanActivationPlanRequiresCompatibleScopeAndCommandPolicy(t *testing.T) {
	body, err := os.ReadFile("testdata/legacy_scan_journal.json")
	require.NoError(t, err)
	journal, err := PrepareScanJournal(models.ScanJournalInput{UUID: uuid.NewString(), RootUUID: uuid.NewString(), SourceUUID: uuid.NewString(), Document: body})
	require.NoError(t, err)
	input := models.ScanJournalActivationInput{Cutoff: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)}
	var records []models.ScanJournalRecord
	var scan models.ScanJournalRecord
	for _, record := range journal.Records {
		if record.Context == "host" && strings.Contains(record.TargetURL, "/user/Example/") {
			records = append(records, record)
			if record.Table == "scan_jobs" {
				scan = record
			}
			if record.Table == "extractor_jobs" {
				input.CheckpointRecordUUID = record.UUID
			}
		}
	}
	for _, replacement := range [][2]string{
		{"2026-09-20T12:34:56", "2026-09-01T12:34:56"}, // selected checkpoint has narrower scope
		{`\"-i\"`, `\"--no-skip\",\"-i\"`},             // incompatible old command policy
	} {
		other := scan
		other.UUID = uuid.NewString()
		other.Evidence = []byte(strings.ReplaceAll(strings.ReplaceAll(string(scan.Evidence), strings.Repeat("a", 64), strings.Repeat("f", 64)), replacement[0], replacement[1]))
		_, err := PrepareScanActivation(input, &journal.Receipt, append(append([]models.ScanJournalRecord{}, records...), other))
		require.ErrorIs(t, err, models.ErrScanJournalInvalid)
	}
	// Full replay preserves a retry limit even without a separate deferral row,
	// and fractional deadlines can never be shortened by millisecond conversion.
	var evidence map[string]any
	require.NoError(t, json.Unmarshal(scan.Evidence, &evidence))
	evidence["attempts"], evidence["retry_after"] = 9, 1790899200.2505
	scan.Evidence, err = json.Marshal(evidence)
	require.NoError(t, err)
	input.CheckpointRecordUUID = ""
	input.Cutoff = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	plan, err := PrepareScanActivation(input, &journal.Receipt, []models.ScanJournalRecord{scan})
	require.NoError(t, err)
	require.True(t, plan.ReplayArchive)
	require.Empty(t, plan.Progress.Cursor)
	require.Equal(t, "legacy_retry_limit", plan.ErrorCode)
	require.Equal(t, 8, plan.Failures)
	require.Equal(t, int64(1790899200251), plan.AvailableAt.UnixMilli())
}
