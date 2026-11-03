package sqlite_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func removeSourceRunConsolidationSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeSourceAccountStatusSchema(t, raw)
	_, err := raw.Exec(`DROP TRIGGER IF EXISTS source_run_attachment_retry_completed;
 DROP TABLE IF EXISTS source_run_attachment_retries;
 DROP TABLE IF EXISTS source_run_consolidations;
 DELETE FROM native_migration_history WHERE version IN (1000135,1000136,1000137);`)
	require.NoError(t, err)
}

func (f *sourceRunFixture) consolidate(t *testing.T, input models.SourceRunConsolidationInput) (*models.SourceRunConsolidation, error) {
	t.Helper()
	var result *models.SourceRunConsolidation
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.SourceRun.Consolidate(ctx, input, f.now)
		return err
	})
	return result, err
}

func sourceRunConsolidationInput(from, to *models.SourceRun) models.SourceRunConsolidationInput {
	return models.SourceRunConsolidationInput{RequestUUID: uuid.NewString(), SourceUUID: from.UUID, TargetUUID: to.UUID,
		SourceRevision: from.Revision, TargetRevision: to.Revision, Reason: "Combine reviewed compatible work; preserve every requested range"}
}

func TestSourceRunConsolidationPreservesRequestsCoverageHistoryAndBackoff(t *testing.T) {
	f := newSourceRunFixture(t)
	old := f.request()
	old.Window.Until = f.now.Add(-48 * time.Hour)
	from := f.claim(t, f.submit(t, old))
	progress := models.SourceRunProgress{ItemsSeen: 30, FilesCompleted: 20, Cursor: "old-checkpoint"}
	_, err := f.coordinator.Progress(t.Context(), f.token, from.Lease(), progress)
	require.NoError(t, err)
	from = f.finish(t, from, "retry")
	request := f.request()
	request.PolicySHA256 = strings.Repeat("b", 64)
	since := f.now.Add(-24 * time.Hour)
	request.Window.Since = &since
	to := f.submit(t, request)
	_, err = f.upgrade(t, models.SourceRunPolicyUpgradeInput{RequestUUID: uuid.NewString(), RunUUID: from.UUID,
		ExpectedRevision: from.Revision, ExpectedPolicySHA256: old.PolicySHA256, PolicySHA256: request.PolicySHA256, Reason: "Compatible repair"})
	require.NoError(t, err)
	from = f.find(t, from.UUID)
	input := sourceRunConsolidationInput(from, to)
	result, err := f.consolidate(t, input)
	require.NoError(t, err)
	replayed, err := f.consolidate(t, input)
	require.NoError(t, err)
	require.Equal(t, result, replayed)
	changed := input
	changed.Reason = "Different review"
	_, err = f.consolidate(t, changed)
	require.ErrorIs(t, err, models.ErrSourceRunConflict)
	to = f.find(t, to.UUID)
	require.Equal(t, []models.SourceWindow{old.Window, request.Window}, to.Pending, "do not fill unrequested gaps")
	require.Equal(t, from.AvailableAt, to.AvailableAt, "consolidation cannot bypass retry timing")
	retained := f.submit(t, old)
	require.Equal(t, from.UUID, retained.UUID)
	require.Equal(t, old.PolicySHA256, retained.PolicySHA256)
	require.Equal(t, progress, retained.Progress)
	require.Equal(t, to.UUID, retained.ConsolidatedInto)
	require.Empty(t, retained.Completed)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	var stored string
	require.NoError(t, raw.QueryRow("SELECT state FROM source_runs WHERE uuid=?", from.UUID).Scan(&stored))
	require.Equal(t, "cancelled", stored)
	require.NoError(t, raw.QueryRow("SELECT outcome FROM source_run_attempts WHERE run_uuid=? AND fence=1", from.UUID).Scan(&stored))
	require.Equal(t, "retry", stored, "original attempt is immutable")
	f.now = to.AvailableAt
	to = f.claim(t, to)
	require.NotNil(t, to)
	to = f.finish(t, to, "succeeded")
	require.Empty(t, f.find(t, from.UUID).Completed, "another range's success does not complete the original request")
	f.now = to.AvailableAt
	to = f.claim(t, to)
	require.NotNil(t, to)
	to = f.finish(t, to, "succeeded")
	require.Equal(t, "succeeded", to.State)
	retained = f.submit(t, old)
	require.Equal(t, "succeeded", retained.State)
	require.Equal(t, []models.SourceWindow{old.Window}, retained.Completed)
	require.Empty(t, retained.Pending)
	_, err = raw.Exec("UPDATE source_run_consolidations SET reason='rewrite'")
	require.ErrorContains(t, err, "immutable")
	_, err = raw.Exec("DELETE FROM source_run_consolidations")
	require.ErrorContains(t, err, "retained")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.Equal(t, retained, f.submit(t, old))
}

func TestSourceRunConsolidationRejectsIncompatibleStaleAndRunningJobs(t *testing.T) {
	f := newSourceRunFixture(t)
	a := f.request()
	from := f.submit(t, a)
	b := f.request()
	b.PolicySHA256 = strings.Repeat("b", 64)
	to := f.submit(t, b)
	_, err := f.consolidate(t, sourceRunConsolidationInput(from, to))
	require.ErrorIs(t, err, models.ErrSourceRunConflict, "different policies require an explicit compatible upgrade first")
	_, err = f.upgrade(t, models.SourceRunPolicyUpgradeInput{RequestUUID: uuid.NewString(), RunUUID: from.UUID,
		ExpectedRevision: from.Revision, ExpectedPolicySHA256: a.PolicySHA256, PolicySHA256: b.PolicySHA256, Reason: "Compatible repair"})
	require.NoError(t, err)
	_, err = f.consolidate(t, sourceRunConsolidationInput(from, to))
	require.ErrorIs(t, err, models.ErrSourceRunConflict, "both expected revisions are checked")
	from = f.find(t, from.UUID)
	to = f.claim(t, to)
	_, err = f.consolidate(t, sourceRunConsolidationInput(from, to))
	require.ErrorIs(t, err, models.ErrSourceRunConflict, "a live lease cannot be moved")
	require.Equal(t, from, f.find(t, from.UUID))
	to = f.finish(t, to, "deferred")
	_, err = f.consolidate(t, sourceRunConsolidationInput(from, to))
	require.ErrorIs(t, err, models.ErrSourceRunConflict, "a review hold cannot silently become queued")
}

func TestSourceRunCompatibleAdmissionReusesReviewedPolicyWithoutRewritingIdentity(t *testing.T) {
	f := newSourceRunFixture(t)
	original := f.request()
	run := f.submit(t, original)
	policy := strings.Repeat("b", 64)
	_, err := f.upgrade(t, models.SourceRunPolicyUpgradeInput{RequestUUID: uuid.NewString(), RunUUID: run.UUID,
		ExpectedRevision: run.Revision, ExpectedPolicySHA256: original.PolicySHA256, PolicySHA256: policy, Reason: "Compatible repair"})
	require.NoError(t, err)
	f.now = f.now.Add(time.Hour)
	next := f.request()
	next.PolicySHA256 = policy
	next.ReuseCompatiblePolicy = true
	next.Window.Since = nil
	reused := f.submit(t, next)
	require.Equal(t, run.UUID, reused.UUID)
	require.Equal(t, original.PolicySHA256, reused.PolicySHA256)
	require.Equal(t, policy, reused.ExecutionPolicySHA256)
	require.Equal(t, []models.SourceWindow{next.Window}, reused.Pending)
	require.Equal(t, reused, f.submit(t, next), "exact receipt replay remains stable")
	other := next
	other.RequestUUID = uuid.NewString()
	other.ReuseCompatiblePolicy = false
	separate := f.submit(t, other)
	require.NotEqual(t, run.UUID, separate.UUID, "callers must opt into compatible admission")
}
