package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func normalizeScanActivation(input models.ScanJournalActivationInput) (models.ScanJournalActivationInput, error) {
	for _, id := range []string{input.UUID, input.ScanRecordUUID, input.CollectionUUID} {
		if !validSourceRunUUID(id) {
			return input, models.ErrScanJournalInvalid
		}
	}
	if (input.CheckpointRecordUUID != "" && !validSourceRunUUID(input.CheckpointRecordUUID)) || input.CollectionRevision < 1 || input.RootRevision < 1 ||
		!archive.ValidSHA256(input.PolicySHA256) || input.CooldownSeconds < 0 || input.CooldownSeconds > 86400 {
		return input, models.ErrScanJournalInvalid
	}
	w, err := scrape.NormalizeWindow(models.SourceWindow{Until: input.Cutoff})
	if err != nil {
		return input, models.ErrScanJournalInvalid
	}
	input.Cutoff = w.Until
	return input, nil
}

func (s *ScanJournalStore) PreviewActivation(ctx context.Context, input models.ScanJournalActivationInput, now time.Time) (*models.ScanJournalActivationPlan, error) {
	input, err := normalizeScanActivation(input)
	if err != nil || !validJobTime(now) || input.Cutoff.After(now.Add(time.Minute)) {
		return nil, models.ErrScanJournalInvalid
	}
	anchor, err := s.Record(ctx, input.ScanRecordUUID)
	if err != nil {
		return nil, err
	}
	if anchor == nil || anchor.Table != "scan_jobs" {
		return nil, models.ErrScanJournalInvalid
	}
	journal, err := s.Find(ctx, anchor.JournalUUID)
	if err != nil {
		return nil, err
	}
	captured, err := time.Parse(time.RFC3339Nano, journal.CapturedAt)
	if err != nil || input.Cutoff.Before(captured) {
		return nil, models.ErrScanJournalInvalid
	}
	c, err := (&SourceCollectionStore{}).Find(ctx, input.CollectionUUID)
	if err != nil {
		return nil, err
	}
	if c == nil || c.State != "active" || c.Revision != input.CollectionRevision || c.RootUUID == nil || *c.RootUUID != journal.RootUUID || c.TargetURL != anchor.TargetURL {
		return nil, models.ErrSourceDefinitionConflict
	}
	root, err := (&MediaRootStore{}).Find(ctx, journal.RootUUID)
	if err != nil {
		return nil, err
	}
	if root == nil || root.State != "active" || root.Revision != input.RootRevision {
		return nil, models.ErrSourceDefinitionConflict
	}
	var rows []scanJournalRecordRow
	if err := dbWrapper.Select(ctx, &rows, `SELECT * FROM scan_journal_records WHERE journal_uuid=? AND context=? AND target_url=? ORDER BY id`, journal.UUID, anchor.Context, anchor.TargetURL); err != nil {
		return nil, err
	}
	records := make([]models.ScanJournalRecord, 0, len(rows))
	for _, row := range rows {
		records = append(records, row.resolve())
	}
	plan, err := scrape.PrepareScanActivation(input, journal, records)
	if err != nil {
		return nil, err
	}
	// A second snapshot of the same old job cannot create another native run.
	var used bool
	if err := dbWrapper.Get(ctx, &used, `SELECT EXISTS(SELECT 1 FROM scan_journal_records r JOIN scan_journal_activation_jobs a
ON a.source_uuid=? AND a.source_key=r.source_key WHERE r.journal_uuid=? AND r.context=? AND r.target_url=? AND r.source_table='scan_jobs')`, journal.SourceUUID, journal.UUID, anchor.Context, anchor.TargetURL); err != nil {
		return nil, err
	}
	if used {
		return nil, models.ErrScanJournalConflict
	}
	plan.PlanSHA256, err = sourceRunHash(plan)
	return plan, err
}

func (s *ScanJournalStore) Activation(ctx context.Context, id string) (*models.ScanJournalActivation, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrScanJournalInvalid
	}
	var row struct {
		Plan    string `db:"plan"`
		Run     string `db:"run_uuid"`
		Created int64  `db:"created_at_ms"`
	}
	if err := dbWrapper.Get(ctx, &row, "SELECT plan,run_uuid,created_at_ms FROM scan_journal_activations WHERE uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	result := &models.ScanJournalActivation{RunUUID: row.Run, CreatedAt: time.UnixMilli(row.Created).UTC()}
	if err := json.Unmarshal([]byte(row.Plan), &result.ScanJournalActivationPlan); err != nil {
		return nil, err
	}
	return result, nil
}

type scanActivationSeed struct {
	UUID          string                   `db:"uuid"`
	ReplayArchive bool                     `db:"replay_archive"`
	WindowJSON    string                   `db:"window"`
	ProgressJSON  string                   `db:"progress"`
	Window        models.SourceWindow      `db:"-"`
	Progress      models.SourceRunProgress `db:"-"`
}

// Lease/progress reads need only the bounded resume policy, not thousands of
// retained job IDs in the maintenance plan. The run UUID has a unique index.
func sourceRunRecoverySeed(ctx context.Context, id string) (*scanActivationSeed, error) {
	var row scanActivationSeed
	if err := dbWrapper.Get(ctx, &row, `SELECT uuid,json_extract(plan,'$.replay_archive') AS replay_archive,
json_extract(plan,'$.window') AS window,json_extract(plan,'$.progress') AS progress
FROM scan_journal_activations WHERE run_uuid=?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if err := json.Unmarshal([]byte(row.WindowJSON), &row.Window); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(row.ProgressJSON), &row.Progress); err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *ScanJournalStore) Activate(ctx context.Context, input models.ScanJournalActivationInput, expected string, now time.Time) (*models.ScanJournalActivation, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	input, err := normalizeScanActivation(input)
	if err != nil || !archive.ValidSHA256(expected) || !validJobTime(now) {
		return nil, models.ErrScanJournalInvalid
	}
	prior, err := s.Activation(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if prior.Binding != input || prior.PlanSHA256 != expected {
			return nil, models.ErrScanJournalConflict
		}
		return prior, nil // response loss cannot reset a running/completed native job
	}
	plan, err := s.PreviewActivation(ctx, input, now)
	if err != nil {
		return nil, err
	}
	if plan.PlanSHA256 != expected {
		return nil, models.ErrScanJournalConflict
	}
	run, created, err := (&SourceRunStore{}).enqueue(ctx, models.SourceRunRequest{
		CollectionUUID: input.CollectionUUID, CollectionRevision: input.CollectionRevision, Operation: "download",
		PolicySHA256: input.PolicySHA256, Window: plan.Window, CooldownSeconds: input.CooldownSeconds,
	}, now, 100000)
	if err != nil {
		return nil, err
	}
	if !created {
		return nil, models.ErrScanJournalConflict // do not replace native ownership or progress
	}
	if _, err := dbWrapper.Exec(ctx, `UPDATE source_runs SET state=?,failures=?,available_at_ms=?,error_code=?,revision=revision+1 WHERE uuid=?`,
		plan.State, plan.Failures, plan.AvailableAt.UnixMilli(), plan.ErrorCode, run.UUID); err != nil {
		return nil, err
	}
	body, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO scan_journal_activations(uuid,journal_uuid,run_uuid,plan,created_at_ms) VALUES(?,?,?,?,?)`,
		input.UUID, plan.JournalUUID, run.UUID, string(body), now.UnixMilli()); err != nil {
		return nil, err
	}
	for _, id := range plan.JobUUIDs {
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO scan_journal_activation_jobs(source_uuid,source_key,record_uuid,activation_uuid)
SELECT ?,source_key,uuid,? FROM scan_journal_records WHERE uuid=?`, plan.SourceUUID, input.UUID, id); err != nil {
			return nil, err
		}
	}
	return s.Activation(ctx, input.UUID)
}
