package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

type SourceBackfillStore struct{}

type backfillRow struct {
	ID          int64   `db:"id"`
	UUID        string  `db:"uuid"`
	Root        string  `db:"root_uuid"`
	Platform    string  `db:"platform"`
	Account     string  `db:"account"`
	Component   string  `db:"component"`
	Outcome     string  `db:"outcome"`
	Basis       string  `db:"basis"`
	DecidedAt   string  `db:"decided_at"`
	Producer    *string `db:"producer_uuid"`
	Source      *string `db:"source_uuid"`
	SourceTable *string `db:"source_table"`
	SourceKey   *string `db:"source_key"`
	Evidence    string  `db:"evidence"`
	Digest      string  `db:"input_sha256"`
	CreatedAt   string  `db:"created_at"`
}

func (row backfillRow) resolve() (*models.BackfillDecision, error) {
	decided, err := time.Parse(time.RFC3339Nano, row.DecidedAt)
	if err != nil {
		return nil, err
	}
	created, err := time.Parse(time.RFC3339Nano, row.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &models.BackfillDecision{
		BackfillDecisionSummary: models.BackfillDecisionSummary{UUID: row.UUID, Component: row.Component, Outcome: row.Outcome, Basis: row.Basis, DecidedAt: decided},
		BackfillSubject:         models.BackfillSubject{RootUUID: row.Root, Platform: row.Platform, Account: row.Account},
		ProducerUUID:            row.Producer, SourceUUID: row.Source, SourceTable: row.SourceTable, Evidence: json.RawMessage(row.Evidence), CreatedAt: created,
	}, nil
}

func findBackfillRow(ctx context.Context, query string, args ...any) (*backfillRow, error) {
	var row backfillRow
	if err := dbWrapper.Get(ctx, &row, query, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func (s *SourceBackfillStore) Find(ctx context.Context, id string) (*models.BackfillDecision, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrBackfillInvalid
	}
	row, err := findBackfillRow(ctx, "SELECT * FROM source_backfill_decisions WHERE uuid=?", id)
	if err != nil || row == nil {
		return nil, err
	}
	return row.resolve()
}

func (s *SourceBackfillStore) Status(ctx context.Context, subject models.BackfillSubject, component string) (*models.BackfillStatus, error) {
	subject, err := scrape.NormalizeBackfillSubject(subject)
	if err != nil {
		return nil, err
	}
	if _, err := scrape.BackfillTargets(subject, component); err != nil {
		return nil, err
	}
	ret := &models.BackfillStatus{BackfillSubject: subject, Component: component, State: "needed", Decisions: []models.BackfillDecisionSummary{}}
	required := scrape.RequiredBackfillComponents(subject.Platform)
	completed := make(map[string]models.BackfillDecisionSummary)
	for _, part := range append(required, component) {
		if _, found := completed[part]; found {
			continue
		}
		row, err := findBackfillRow(ctx, `SELECT uuid,component,outcome,basis,decided_at,created_at FROM source_backfill_decisions
WHERE root_uuid=? AND platform=? AND account=? AND component=? AND outcome='completed' ORDER BY id LIMIT 1`, subject.RootUUID, subject.Platform, subject.Account, part)
		if err != nil {
			return nil, err
		}
		if row != nil {
			decision, err := row.resolve()
			if err != nil {
				return nil, err
			}
			completed[part] = decision.BackfillDecisionSummary
		}
	}
	ret.AccountComplete = true
	for _, part := range required {
		if decision, found := completed[part]; found {
			ret.Decisions = append(ret.Decisions, decision)
		} else {
			ret.AccountComplete = false
		}
	}
	if decision, found := completed[component]; found || ret.AccountComplete {
		ret.State = "completed"
		if found && !ret.AccountComplete {
			ret.Decisions = []models.BackfillDecisionSummary{decision}
		}
		return ret, nil
	}
	row, err := findBackfillRow(ctx, `SELECT uuid,component,outcome,basis,decided_at,created_at FROM source_backfill_decisions
WHERE root_uuid=? AND platform=? AND account=? AND component='*' AND outcome='skipped' ORDER BY id LIMIT 1`, subject.RootUUID, subject.Platform, subject.Account)
	if err != nil {
		return nil, err
	}
	if row != nil {
		decision, err := row.resolve()
		if err != nil {
			return nil, err
		}
		ret.State = "skipped"
		ret.Decisions = append(ret.Decisions, decision.BackfillDecisionSummary)
	}
	return ret, nil
}

func (s *SourceBackfillStore) replay(ctx context.Context, id, digest string) (*models.BackfillDecision, error) {
	row, err := findBackfillRow(ctx, "SELECT * FROM source_backfill_decisions WHERE uuid=?", id)
	if err != nil || row == nil {
		return nil, err
	}
	if row.Digest != digest {
		return nil, models.ErrBackfillConflict
	}
	return row.resolve()
}

func (s *SourceBackfillStore) put(ctx context.Context, row backfillRow) (*models.BackfillDecision, error) {
	_, err := dbWrapper.Exec(ctx, `INSERT INTO source_backfill_decisions
(uuid,root_uuid,platform,account,component,outcome,basis,decided_at,producer_uuid,source_uuid,source_table,source_key,evidence,input_sha256,created_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, row.UUID, row.Root, row.Platform, row.Account, row.Component, row.Outcome, row.Basis, row.DecidedAt,
		row.Producer, row.Source, row.SourceTable, row.SourceKey, row.Evidence, row.Digest, row.CreatedAt)
	if err != nil {
		return nil, err
	}
	return row.resolve()
}

func (s *SourceBackfillStore) ImportLegacy(ctx context.Context, input models.LegacyBackfillRecord, now time.Time) (*models.BackfillDecision, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validJobTime(now) || !validSourceRunUUID(input.SourceUUID) {
		return nil, models.ErrBackfillInvalid
	}
	row, err := legacyBackfillRow(input, now)
	if err != nil {
		return nil, err
	}
	if prior, err := s.replay(ctx, row.UUID, row.Digest); err != nil || prior != nil {
		return prior, err
	}
	root, err := (&MediaRootStore{}).Find(ctx, row.Root)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, models.ErrBackfillInvalid
	}
	return s.put(ctx, *row)
}

func legacyBackfillRow(input models.LegacyBackfillRecord, now time.Time) (*backfillRow, error) {
	object, err := archive.DecodeJSONObject(input.Record, 1<<20)
	if err != nil {
		return nil, models.ErrBackfillInvalid
	}
	text := func(key string) string { value, _ := object[key].(string); return value }
	subject, err := scrape.NormalizeBackfillSubject(models.BackfillSubject{RootUUID: input.RootUUID, Platform: text("platform"), Account: text("account")})
	if err != nil {
		return nil, err
	}
	row := &backfillRow{Root: subject.RootUUID, Platform: subject.Platform, Account: subject.Account, Source: &input.SourceUUID,
		SourceTable: &input.Table, CreatedAt: now.UTC().Format(time.RFC3339Nano)}
	keys := []string{text("platform"), text("account")}
	switch input.Table {
	case "backfill_completion":
		if len(object) != 5 || text("component") == "" || text("result_json") == "" {
			return nil, models.ErrBackfillInvalid
		}
		if _, err := scrape.BackfillTargets(subject, text("component")); err != nil {
			return nil, err
		}
		result, err := archive.DecodeJSONObject([]byte(text("result_json")), 1<<20)
		if err != nil || result["command_failed"] != false || result["exit_code"] != json.Number("0") || (result["network_blocked"] != nil && result["network_blocked"] != false) {
			return nil, models.ErrBackfillInvalid
		}
		row.Component, row.DecidedAt = text("component"), text("completed_at")
		row.Outcome, row.Basis = "completed", "legacy_completion"
		keys = append(keys, row.Component)
	case "legacy_backfill_skip":
		if len(object) != 4 || text("reason") == "" {
			return nil, models.ErrBackfillInvalid
		}
		row.Component, row.DecidedAt = "*", text("recorded_at")
		row.Outcome, row.Basis = "skipped", "legacy_skip"
	default:
		return nil, models.ErrBackfillInvalid
	}
	if decided, err := time.Parse(time.RFC3339Nano, row.DecidedAt); err != nil || decided.Year() < 1 || decided.Year() > 9999 {
		return nil, models.ErrBackfillInvalid
	}
	body, err := archive.EncodeSourceJSON(object)
	if err != nil {
		return nil, err
	}
	key, err := json.Marshal(keys)
	if err != nil {
		return nil, err
	}
	keyString := string(key)
	row.SourceKey, row.Evidence = &keyString, string(body)
	row.UUID = uuid.NewSHA1(uuid.MustParse(input.SourceUUID), []byte(input.Table+"/"+keyString)).String()
	row.Digest, err = sourceRunHash([]any{input.RootUUID, input.SourceUUID, input.Table, object})
	return row, err
}

func (s *SourceBackfillStore) Complete(ctx context.Context, producer string, input models.BackfillCompletion, now time.Time) (*models.BackfillDecision, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validJobTime(now) || !validSourceRunUUID(producer) || !validSourceRunUUID(input.UUID) || !archive.ValidSHA256(input.PolicySHA256) || len(input.Requests) < 1 || len(input.Requests) > 512 {
		return nil, models.ErrBackfillInvalid
	}
	targets, err := scrape.BackfillTargets(input.BackfillSubject, input.Component)
	if err != nil {
		return nil, err
	}
	input.BackfillSubject, err = scrape.NormalizeBackfillSubject(input.BackfillSubject)
	if err != nil {
		return nil, err
	}
	input.Window, err = scrape.NormalizeWindow(input.Window)
	if err != nil || input.Window.Basis != "" || input.Window.Since != nil || input.Window.Until.After(now.Add(time.Minute)) {
		return nil, models.ErrBackfillInvalid
	}
	input.Requests = append([]models.SourceRunRequest(nil), input.Requests...)
	sort.Slice(input.Requests, func(i, j int) bool { return input.Requests[i].RequestUUID < input.Requests[j].RequestUUID })
	for i := range input.Requests {
		r := &input.Requests[i]
		r.Window, err = scrape.NormalizeWindow(r.Window)
		if err != nil || !validSourceRunUUID(r.RequestUUID) || r.Operation != "download" || r.PolicySHA256 != input.PolicySHA256 || (i > 0 && input.Requests[i-1].RequestUUID == r.RequestUUID) {
			return nil, models.ErrBackfillInvalid
		}
	}
	digest, err := sourceRunHash([]any{producer, input, targets})
	if err != nil {
		return nil, err
	}
	if prior, err := s.replay(ctx, input.UUID, digest); err != nil || prior != nil {
		return prior, err
	}
	if err := proveBackfillCoverage(ctx, producer, input, targets); err != nil {
		return nil, err
	}
	body, err := json.Marshal(struct {
		Completion models.BackfillCompletion `json:"completion"`
		Targets    []string                  `json:"targets"`
	}{input, targets})
	if err != nil || len(body) > 1<<20 {
		return nil, models.ErrBackfillInvalid
	}
	when := now.UTC().Format(time.RFC3339Nano)
	ret, err := s.put(ctx, backfillRow{UUID: input.UUID, Root: input.RootUUID, Platform: input.Platform, Account: input.Account,
		Component: input.Component, Outcome: "completed", Basis: "source_runs", DecidedAt: when, Producer: &producer,
		Evidence: string(body), Digest: digest, CreatedAt: when})
	if err != nil {
		return nil, err
	}
	for _, request := range input.Requests {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO source_backfill_requests(decision_uuid,producer_uuid,request_uuid) VALUES(?,?,?)", input.UUID, producer, request.RequestUUID); err != nil {
			return nil, err
		}
	}
	return ret, nil
}

func proveBackfillCoverage(ctx context.Context, producer string, input models.BackfillCompletion, targets []string) error {
	covered := make(map[string][]models.SourceWindow, len(targets))
	for _, target := range targets {
		covered[target] = nil
	}
	for _, request := range input.Requests {
		var stored struct {
			Digest string `db:"digest"`
			Run    string `db:"run_uuid"`
		}
		err := dbWrapper.Get(ctx, &stored, "SELECT digest,run_uuid FROM source_run_requests WHERE producer_uuid=? AND request_uuid=?", producer, request.RequestUUID)
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrBackfillIncomplete
		}
		if err != nil {
			return err
		}
		digest, err := sourceRunHash(request)
		if err != nil {
			return err
		}
		if stored.Digest != digest {
			return models.ErrBackfillConflict
		}
		run, err := (&SourceRunStore{}).Find(ctx, stored.Run)
		if err != nil {
			return err
		}
		if run == nil || run.RootUUID == nil || *run.RootUUID != input.RootUUID || run.Operation != "download" || run.PolicySHA256 != input.PolicySHA256 {
			return models.ErrBackfillConflict
		}
		if _, found := covered[run.TargetURL]; !found {
			return models.ErrBackfillConflict
		}
		requested := []models.SourceWindow{request.Window}
		verified := scrape.Subtract(requested, scrape.Subtract(requested, run.Completed))
		covered[run.TargetURL] = scrape.Union(covered[run.TargetURL], verified)
	}
	for _, coverage := range covered {
		if len(scrape.Subtract([]models.SourceWindow{input.Window}, coverage)) != 0 {
			return models.ErrBackfillIncomplete
		}
	}
	return nil
}
