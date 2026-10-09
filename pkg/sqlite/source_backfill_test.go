package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func backfillLegacy(t *testing.T, root, source, account, component string) models.LegacyBackfillRecord {
	t.Helper()
	result := `{"command_failed":false,"exit_code":0,"network_blocked":false,"exhaustive_history_verified":false,"user_accepted_as_complete":true,"user_confirmation":{"historical_completion_time_unknown":true},"unknown_old_field":90071992547409931234,"stdout_tail":"private historical log"}`
	body, err := json.Marshal(map[string]string{"platform": "reddit", "account": account, "component": component,
		"completed_at": "2026-09-29T12:34:56.123456-07:00", "result_json": result})
	require.NoError(t, err)
	return models.LegacyBackfillRecord{RootUUID: root, SourceUUID: source, Table: "backfill_completion", Record: body}
}

func importBackfill(t *testing.T, f *sourceRunFixture, input models.LegacyBackfillRecord) *models.BackfillDecision {
	t.Helper()
	var decision *models.BackfillDecision
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		decision, err = f.repo.SourceBackfill.ImportLegacy(ctx, input, f.now)
		return err
	}))
	return decision
}

func backfillStatus(t *testing.T, f *sourceRunFixture, account, component string) *models.BackfillStatus {
	t.Helper()
	var state *models.BackfillStatus
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		state, err = f.repo.SourceBackfill.Status(ctx, models.BackfillSubject{RootUUID: f.root.UUID, Platform: "reddit", Account: account}, component)
		return err
	}))
	return state
}

func TestSourceBackfillLegacyEvidenceIsLosslessAndNeverCreatesCoverage(t *testing.T) {
	f := newSourceRunFixture(t)
	input := backfillLegacy(t, f.root.UUID, uuid.NewString(), "Mixed_Name", "reddit-new")
	decision := importBackfill(t, f, input)
	require.Equal(t, "legacy_completion", decision.Basis)
	require.Equal(t, "mixed_name", decision.Account)
	require.JSONEq(t, string(input.Record), string(decision.Evidence))
	require.Contains(t, string(decision.Evidence), "90071992547409931234")
	require.Contains(t, string(decision.Evidence), `\"exhaustive_history_verified\":false`)
	require.Equal(t, decision, importBackfill(t, f, input))
	status := backfillStatus(t, f, "MIXED_NAME", "reddit-new")
	require.Equal(t, "completed", status.State)
	require.False(t, status.AccountComplete)
	body, err := json.Marshal(status)
	require.NoError(t, err)
	require.NotContains(t, string(body), "private historical log")
	require.Equal(t, "needed", backfillStatus(t, f, "mixed_name", "reddit-top").State)
	importBackfill(t, f, backfillLegacy(t, f.root.UUID, *decision.SourceUUID, "Mixed_Name", "reddit-top"))
	status = backfillStatus(t, f, "mixed_name", "reddit-search-top-year")
	require.Equal(t, "completed", status.State)
	require.True(t, status.AccountComplete)
	require.Len(t, status.Decisions, 2)
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(path))
	f.repo = f.db.Repository()
	require.Equal(t, decision, importBackfill(t, f, input))
	raw := openRawDB(t, path)
	defer raw.Close()
	for _, table := range []string{"source_runs", "source_run_requests", "source_backfill_requests"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestSourceBackfillSkipAndConflictingImportRemainDistinct(t *testing.T) {
	f := newSourceRunFixture(t)
	source := uuid.NewString()
	row := models.LegacyBackfillRecord{RootUUID: f.root.UUID, SourceUUID: source, Table: "legacy_backfill_skip",
		Record: []byte(`{"platform":"reddit","account":"Example","recorded_at":"2026-09-29T00:00:00Z","reason":"legacy list guard; completion unknown"}`)}
	skip := importBackfill(t, f, row)
	require.Equal(t, "skipped", skip.Outcome)
	status := backfillStatus(t, f, "example", "reddit-top")
	require.Equal(t, "skipped", status.State)
	require.False(t, status.AccountComplete)
	old := backfillLegacy(t, f.root.UUID, source, "Example", "reddit-new")
	accepted := importBackfill(t, f, old)
	require.Equal(t, "completed", backfillStatus(t, f, "example", "reddit-new").State)
	require.Equal(t, "skipped", backfillStatus(t, f, "example", "reddit-top").State)
	changed := old
	var record map[string]string
	require.NoError(t, json.Unmarshal(old.Record, &record))
	record["completed_at"] = "2026-09-30T00:00:00Z"
	changedBody, err := json.Marshal(record)
	require.NoError(t, err)
	changed.Record = changedBody
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceBackfill.ImportLegacy(ctx, backfillLegacy(t, f.root.UUID, source, "Example", "reddit-top"), f.now)
		if err != nil {
			return err
		}
		_, err = f.repo.SourceBackfill.ImportLegacy(ctx, changed, f.now)
		return err
	})
	require.ErrorIs(t, err, models.ErrBackfillConflict)
	require.Equal(t, accepted, importBackfill(t, f, old))
	require.Equal(t, "skipped", backfillStatus(t, f, "example", "reddit-top").State, "late conflict rolls back the preceding row")
	other := putMediaRoot(t, f.repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Other", State: "active"}})
	changed = old
	changed.RootUUID = other.UUID
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceBackfill.ImportLegacy(ctx, changed, f.now)
		return err
	})
	require.ErrorIs(t, err, models.ErrBackfillConflict, "moving a legacy source row to another root is not a replay")
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec("UPDATE source_backfill_decisions SET component='reddit-top'")
	require.ErrorContains(t, err, "immutable")
}

func TestSourceBackfillNativeProofRequiresEveryOriginalRequestWindow(t *testing.T) {
	f := newSourceRunFixture(t)
	var err error
	_, f.token, err = f.service.IssueCredential(t.Context(), f.producer.UUID, nil, nil, f.root.UUID)
	require.NoError(t, err)
	input := models.BackfillCompletion{UUID: uuid.NewString(), BackfillSubject: models.BackfillSubject{RootUUID: f.root.UUID, Platform: "reddit", Account: "Mixed_Name"},
		Component: "reddit-new", Window: models.SourceWindow{Until: f.now}, PolicySHA256: f.request().PolicySHA256}
	targets, err := scrape.BackfillTargets(input.BackfillSubject, input.Component)
	require.NoError(t, err)
	middle := f.now.Add(-24 * time.Hour)
	var last *models.SourceRun
	collection := putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
		Label: "Backfill profile", Kind: "account", Namespace: "native:reddit", State: "active", TargetURL: scrape.RedditProfileURL(targets[0]), RootUUID: &f.root.UUID, PathPrefix: "Example"}})
	for i, target := range targets {
		windows := []models.SourceWindow{{Until: f.now}}
		if i == 0 {
			windows = []models.SourceWindow{{Until: middle}, {Since: &middle, Until: f.now}}
		}
		for _, window := range windows {
			request := f.request()
			request.RetrievalURL = target
			request.CollectionUUID, request.CollectionRevision, request.Window, request.CooldownSeconds = collection.UUID, collection.Revision, window, 0
			input.Requests = append(input.Requests, request)
			last = f.submit(t, request)
			if i == 0 {
				f.finish(t, f.claim(t, last), "succeeded")
			}
		}
	}
	_, err = f.service.CompleteBackfill(t.Context(), f.token, input)
	require.ErrorIs(t, err, models.ErrBackfillIncomplete, "a queued search target cannot become completed backfill")
	f.finish(t, f.claim(t, last), "succeeded")
	missing := input
	missing.Requests = input.Requests[1:]
	_, err = f.service.CompleteBackfill(t.Context(), f.token, missing)
	require.ErrorIs(t, err, models.ErrBackfillIncomplete, "each original slice must cover the requested window")
	changed := input
	changed.Requests = append([]models.SourceRunRequest(nil), input.Requests...)
	changed.Requests[0].Window.Until = f.now
	_, err = f.service.CompleteBackfill(t.Context(), f.token, changed)
	require.ErrorIs(t, err, models.ErrBackfillConflict, "request hashes prevent fabricated time coverage")
	wrong := input
	wrong.Account = "another_account"
	_, err = f.service.CompleteBackfill(t.Context(), f.token, wrong)
	require.ErrorIs(t, err, models.ErrBackfillConflict)
	decision, err := f.service.CompleteBackfill(t.Context(), f.token, input)
	require.NoError(t, err)
	require.Equal(t, "source_runs", decision.Basis)
	replayed, err := f.service.CompleteBackfill(t.Context(), f.token, input)
	require.NoError(t, err)
	require.Equal(t, decision, replayed)
	changed = input
	changed.Window.Until = middle
	_, err = f.service.CompleteBackfill(t.Context(), f.token, changed)
	require.ErrorIs(t, err, models.ErrBackfillConflict)
	status := backfillStatus(t, f, "mixed_name", "reddit-new")
	require.Equal(t, "completed", status.State)
	require.False(t, status.AccountComplete)
	// Persisted proof, including case-sensitive source URLs, must survive startup.
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(path))
	f.repo = f.db.Repository()
	f.service = ingest.New(f.repo)
	replayed, err = f.service.CompleteBackfill(t.Context(), f.token, input)
	require.NoError(t, err)
	require.Equal(t, decision, replayed)
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(f.db, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	raw := openRawDB(t, output)
	defer raw.Close()
	for _, table := range []string{"source_backfill_decisions", "source_backfill_requests", "source_runs"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	actual := openRawDB(t, f.db.DatabasePath())
	_, err = actual.Exec("DELETE FROM source_backfill_requests")
	require.NoError(t, err)
	require.NoError(t, actual.Close())
	broken := sqlite.NewDatabase()
	require.ErrorContains(t, broken.Open(f.db.DatabasePath()), "inconsistent source backfill proof")
}

func TestSourceBackfillRejectsUnknownLegacyShapesAndRollsBack(t *testing.T) {
	f := newSourceRunFixture(t)
	valid := backfillLegacy(t, f.root.UUID, uuid.NewString(), "example", "reddit-new")
	for _, record := range []json.RawMessage{
		[]byte(`{"platform":"reddit","platform":"twitter"}`),
		[]byte(`{"platform":"reddit","account":"example","component":"reddit-new","completed_at":"2026-09-29T00:00:00Z","result_json":"{\"command_failed\":true,\"exit_code\":4}"}`),
		[]byte(`{"platform":"reddit","account":"example","component":"new-unknown-mode","completed_at":"2026-09-29T00:00:00Z","result_json":"{}"}`),
	} {
		changed := valid
		changed.Record = record
		err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.SourceBackfill.ImportLegacy(ctx, changed, f.now)
			return err
		})
		require.ErrorIs(t, err, models.ErrBackfillInvalid)
	}
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceBackfill.ImportLegacy(ctx, valid, f.now)
		require.NoError(t, err)
		return errors.New("interrupted import")
	})
	require.ErrorContains(t, err, "interrupted")
	require.Equal(t, "needed", backfillStatus(t, f, "example", "reddit-new").State)
}
