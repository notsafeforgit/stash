package sqlite_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func activationFixture(t *testing.T, f *sourceRunFixture, deferred bool) (models.ScanJournalInput, models.ScanJournalActivationInput) {
	t.Helper()
	input := scanJournalFixture(t, f.root.UUID)
	var data map[string]any
	require.NoError(t, json.Unmarshal(input.Document, &data))
	data["captured_at"] = f.now.Add(-time.Minute).Format(time.RFC3339Nano)
	tables := data["tables"].(map[string]any)
	jobs := tables["scan_jobs"].([]any)
	first := jobs[0].(map[string]any)
	first["url"] = f.collection.TargetURL
	first["retry_after"] = float64(f.now.Add(time.Hour).UnixMilli()) / 1000
	second := map[string]any{}
	for k, v := range first {
		second[k] = v
	}
	second["id"], second["attempts"] = strings.Repeat("f", 64), 1
	second["command_json"] = strings.ReplaceAll(first["command_json"].(string), "2026-09-20T12:34:56", "2026-09-22T12:34:56")
	tables["scan_jobs"] = append(jobs, second)
	tables["extractor_jobs"].([]any)[0].(map[string]any)["url"] = f.collection.TargetURL
	if deferred {
		tables["scan_deferrals"].([]any)[0].(map[string]any)["url"] = f.collection.TargetURL
	} else {
		tables["scan_deferrals"] = []any{}
	}
	var err error
	input.Document, err = json.Marshal(data)
	require.NoError(t, err)
	binding := models.ScanJournalActivationInput{UUID: uuid.NewString(), CollectionUUID: f.collection.UUID,
		CollectionRevision: f.collection.Revision, RootRevision: f.root.Revision, PolicySHA256: f.request().PolicySHA256,
		CooldownSeconds: 5, Cutoff: f.now}
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if _, err := f.repo.ScanJournal.Import(ctx, input, f.now); err != nil {
			return err
		}
		records, err := f.repo.ScanJournal.Records(ctx, input.UUID, "", 0, 100)
		for _, row := range records {
			if row.Table == "scan_jobs" && strings.Contains(row.SourceKey, strings.Repeat("a", 64)) {
				binding.ScanRecordUUID = row.UUID
			}
			if row.Table == "extractor_jobs" && row.TargetURL == f.collection.TargetURL {
				binding.CheckpointRecordUUID = row.UUID
			}
		}
		return err
	}))
	return input, binding
}

func previewActivation(t *testing.T, f *sourceRunFixture, input models.ScanJournalActivationInput) *models.ScanJournalActivationPlan {
	t.Helper()
	var plan *models.ScanJournalActivationPlan
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		plan, err = f.repo.ScanJournal.PreviewActivation(ctx, input, f.now)
		return err
	}))
	return plan
}

func applyActivation(t *testing.T, f *sourceRunFixture, plan *models.ScanJournalActivationPlan) *models.ScanJournalActivation {
	t.Helper()
	var result *models.ScanJournalActivation
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.ScanJournal.Activate(ctx, plan.Binding, plan.PlanSHA256, f.now)
		return err
	}))
	return result
}

func TestScanActivationConsolidatesWithoutInventingCompletionOrOwnership(t *testing.T) {
	f := newSourceRunFixture(t)
	snapshot, binding := activationFixture(t, f, true)
	plan := previewActivation(t, f, binding)
	require.Len(t, plan.JobUUIDs, 2)
	require.Len(t, plan.DeferralUUIDs, 1)
	require.Equal(t, "2026-09-20T12:34:56Z", plan.Window.Since.Format(time.RFC3339))
	require.Equal(t, 3, plan.Failures)
	require.Equal(t, f.now.Add(time.Hour), plan.AvailableAt)
	require.Equal(t, int64(257), plan.Progress.ItemsSeen)
	require.Zero(t, plan.Progress.FilesCompleted)
	raw := openRawDB(t, f.db.DatabasePath())
	t.Cleanup(func() { require.NoError(t, raw.Close()) })
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_runs"))
	result := applyActivation(t, f, plan)
	run := f.find(t, result.RunUUID)
	require.Equal(t, "deferred", run.State)
	require.Equal(t, "legacy_scan_deferred", run.ErrorCode)
	require.Equal(t, 3, run.Failures)
	require.Zero(t, run.Fence)
	require.Nil(t, f.claim(t, run))
	for _, table := range []string{"source_run_requests", "source_run_attempts", "source_backfill_decisions"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	// Timers coalesce coverage but cannot clear the imported deferral/delay.
	fresh := f.request()
	fresh.Window = plan.Window
	run = f.submit(t, fresh)
	require.Equal(t, result.RunUUID, run.UUID)
	require.Equal(t, "deferred", run.State)
	require.Equal(t, plan.AvailableAt, run.AvailableAt)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceRun.Review(ctx, run.UUID, run.Revision, "retry", f.now)
		return err
	}))
	f.now = plan.AvailableAt
	run = f.claim(t, f.find(t, run.UUID))
	require.NotNil(t, run)
	require.Equal(t, plan.Progress, run.Progress)
	require.False(t, run.Recovery.ReplayArchive)
	require.Equal(t, int64(1), run.Fence)
	attempts, err := f.coordinator.Attempts(t.Context(), f.token, run.UUID, 0)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Equal(t, plan.Progress, attempts[0].Progress)
	// Exact replay survives reopen and does not reset the live attempt.
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(path))
	f.repo = f.db.Repository()
	require.Equal(t, result, applyActivation(t, f, plan))
	require.Equal(t, run, f.find(t, run.UUID))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		row, err := f.repo.ScanJournal.Record(ctx, binding.ScanRecordUUID)
		require.Equal(t, binding.UUID, row.ActivationUUID)
		return err
	}))
	// Importing another frozen snapshot cannot schedule the same legacy IDs.
	snapshot.UUID = uuid.NewString()
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ScanJournal.Import(ctx, snapshot, f.now)
		return err
	}))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.ScanJournal.Records(ctx, snapshot.UUID, "scan_jobs", 0, 100)
		if err != nil {
			return err
		}
		binding.UUID, binding.ScanRecordUUID, binding.CheckpointRecordUUID = uuid.NewString(), rows[0].UUID, ""
		_, err = f.repo.ScanJournal.PreviewActivation(ctx, binding, f.now)
		require.ErrorIs(t, err, models.ErrScanJournalConflict)
		return nil
	}))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(f.db, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
}

func TestScanActivationRejectsChangedBindingsAndRollsBackNativeCollision(t *testing.T) {
	f := newSourceRunFixture(t)
	_, binding := activationFixture(t, f, false)
	plan := previewActivation(t, f, binding)
	for _, change := range []func(*models.ScanJournalActivationInput){
		func(b *models.ScanJournalActivationInput) { b.RootRevision++ },
		func(b *models.ScanJournalActivationInput) { b.CollectionRevision++ },
		func(b *models.ScanJournalActivationInput) { b.CheckpointRecordUUID = uuid.NewString() },
		func(b *models.ScanJournalActivationInput) { b.Cutoff = f.now.Add(-2 * time.Hour) },
	} {
		bad := binding
		change(&bad)
		require.Error(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.ScanJournal.Activate(ctx, bad, plan.PlanSHA256, f.now)
			return err
		}))
	}
	native := f.submit(t, f.request())
	require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ScanJournal.Activate(ctx, binding, plan.PlanSHA256, f.now)
		return err
	}), models.ErrScanJournalConflict)
	require.Equal(t, native, f.find(t, native.UUID), "failed activation must roll back coalesced pending work")
}

func TestScanActivationExpandedWindowReplaysWithoutStaleCheckpoint(t *testing.T) {
	for _, expand := range []bool{false, true} {
		t.Run(map[bool]string{false: "same window", true: "expanded window"}[expand], func(t *testing.T) {
			f := newSourceRunFixture(t)
			_, binding := activationFixture(t, f, false)
			plan := previewActivation(t, f, binding)
			result := applyActivation(t, f, plan)
			run := f.find(t, result.RunUUID)
			require.Nil(t, f.claim(t, run), "imported retry delay remains effective")
			f.now = plan.AvailableAt
			if expand {
				extra := f.request()
				extra.Window = plan.Window
				extra.Window.Since = nil
				run = f.submit(t, extra)
			}
			run = f.claim(t, run)
			require.NotNil(t, run)
			require.Equal(t, expand, run.Recovery.ReplayArchive)
			if expand {
				require.Empty(t, run.Progress.Cursor)
			} else {
				require.Equal(t, plan.Progress, run.Progress)
			}
			finished, err := f.coordinator.Finish(t.Context(), f.token, run.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "network_error"})
			require.NoError(t, err)
			f.now = finished.AvailableAt
			again := f.claim(t, finished)
			require.Equal(t, run.Progress, again.Progress)
			require.Equal(t, run.Recovery, again.Recovery)
		})
	}
}
