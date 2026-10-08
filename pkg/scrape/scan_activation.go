package scrape

import (
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

const LegacyScanCursorPrefix = "gallery-dl-archive-v1:"

// PrepareScanActivation consolidates only an exact target/context group from
// one immutable snapshot. No command, current process, or name is resolved here.
func PrepareScanActivation(input models.ScanJournalActivationInput, journal *models.ScanJournal, rows []models.ScanJournalRecord) (*models.ScanJournalActivationPlan, error) {
	p := &models.ScanJournalActivationPlan{Binding: input, JournalUUID: journal.UUID, RootUUID: journal.RootUUID,
		SourceUUID: journal.SourceUUID, State: "queued", AvailableAt: input.Cutoff,
		JobUUIDs: []string{}, DeferralUUIDs: []string{}, ReplayArchive: input.CheckpointRecordUUID == ""}
	windows := []models.SourceWindow{}
	scopes := map[string]models.SourceWindow{}
	var checkpoint struct {
		Scope string `json:"scope"`
		Key   string `json:"last_key"`
		Index int64  `json:"last_index"`
	}
	command := ""
	for _, row := range rows {
		if p.TargetURL == "" {
			p.TargetURL, p.Context = row.TargetURL, row.Context
		}
		if row.JournalUUID != journal.UUID || row.TargetURL != p.TargetURL || row.Context != p.Context {
			return nil, models.ErrScanJournalInvalid
		}
		switch row.Table {
		case "scan_jobs":
			var scan struct {
				ID         string  `json:"id"`
				Command    string  `json:"command_json"`
				Attempts   int     `json:"attempts"`
				RetryAfter float64 `json:"retry_after"`
			}
			if json.Unmarshal(row.Evidence, &scan) != nil {
				return nil, models.ErrScanJournalInvalid
			}
			window, base, err := legacyScanWindow(scan.Command, input.Cutoff)
			if err != nil || (command != "" && base != command) {
				return nil, models.ErrScanJournalInvalid // different old policies need separate review
			}
			command = base
			windows = append(windows, window)
			scopes[scan.ID] = window
			p.JobUUIDs = append(p.JobUUIDs, row.UUID)
			p.Failures = max(p.Failures, min(8, scan.Attempts))
			// Fractional legacy seconds become an explicit millisecond deadline.
			retry := time.UnixMilli(int64(math.Ceil(scan.RetryAfter * 1000))).UTC()
			if retry.After(p.AvailableAt) {
				p.AvailableAt = retry
			}
		case "scan_deferrals":
			p.DeferralUUIDs = append(p.DeferralUUIDs, row.UUID)
		case "extractor_jobs":
			if row.UUID == input.CheckpointRecordUUID && json.Unmarshal(row.Evidence, &checkpoint) != nil {
				return nil, models.ErrScanJournalInvalid
			}
		}
	}
	merged := Union(windows)
	if len(merged) != 1 {
		return nil, models.ErrScanJournalInvalid
	}
	p.Window = merged[0]
	if input.CheckpointRecordUUID != "" {
		window, found := scopes[checkpoint.Scope]
		if !found || strings.HasPrefix(checkpoint.Scope, "manual:") || !SameWindow(window, p.Window) || !archive.ValidSHA256(checkpoint.Key) || checkpoint.Index < 0 {
			return nil, models.ErrScanJournalInvalid
		}
		p.Progress = models.SourceRunProgress{ItemsSeen: checkpoint.Index, Cursor: LegacyScanCursorPrefix + checkpoint.Key}
	}
	if len(p.DeferralUUIDs) > 0 {
		p.State, p.ErrorCode = "deferred", "legacy_scan_deferred"
	} else if p.Failures >= 8 {
		p.State, p.ErrorCode = "deferred", "legacy_retry_limit"
	}
	return p, nil
}

func SameWindow(a, b models.SourceWindow) bool {
	return a.Basis == b.Basis && a.Until.Equal(b.Until) && ((a.Since == nil && b.Since == nil) || (a.Since != nil && b.Since != nil && a.Since.Equal(*b.Since)))
}

func legacyScanWindow(command string, cutoff time.Time) (models.SourceWindow, string, error) {
	lower, ok := journalCommand(command)
	if !ok {
		return models.SourceWindow{}, "", models.ErrScanJournalInvalid
	}
	w := models.SourceWindow{Until: cutoff}
	if lower != "" {
		date, err := time.Parse(time.RFC3339Nano, lower)
		if err != nil {
			// gallery-dl interpreted the old launcher's unzoned date as UTC.
			date, err = time.Parse("2006-01-02T15:04:05", lower)
		}
		if err != nil {
			return w, "", models.ErrScanJournalInvalid
		}
		w.Since = &date
	}
	var args []string
	if err := json.Unmarshal([]byte(command), &args); err != nil {
		return w, "", err
	}
	base := []string{}
	for i := 0; i < len(args); i++ {
		if (args[i] == "-o" || args[i] == "--option") && i+1 < len(args) && strings.HasPrefix(args[i+1], "extractor.reddit.date-min=") {
			i++
			continue
		}
		base = append(base, args[i])
	}
	encoded, err := json.Marshal(base)
	if err != nil {
		return w, "", err
	}
	w, err = NormalizeWindow(w)
	return w, string(encoded), err
}
