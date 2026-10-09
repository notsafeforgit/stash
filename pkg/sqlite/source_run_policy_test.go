package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func (f *sourceRunFixture) upgrade(t *testing.T, input models.SourceRunPolicyUpgradeInput) (*models.SourceRunPolicyUpgrade, error) {
	t.Helper()
	var result *models.SourceRunPolicyUpgrade
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.SourceRun.UpgradePolicy(ctx, input, f.now)
		return err
	})
	return result, err
}

func TestSourceRunPolicyUpgradePreservesRequestsProgressAndAttemptPolicies(t *testing.T) {
	f := newSourceRunFixture(t)
	request := f.request()
	run := f.claim(t, f.submit(t, request))
	progress := models.SourceRunProgress{ItemsSeen: 12, FilesCompleted: 9, Cursor: "completed-attachment"}
	_, err := f.coordinator.Progress(t.Context(), f.token, run.Lease(), progress)
	require.NoError(t, err)
	run = f.finish(t, run, "deferred")
	input := models.SourceRunPolicyUpgradeInput{RequestUUID: uuid.NewString(), RunUUID: run.UUID,
		ExpectedRevision: run.Revision, ExpectedPolicySHA256: run.PolicySHA256, PolicySHA256: strings.Repeat("b", 64), Reason: "Repair attachment matching; source window and checkpoints are compatible"}
	first, err := f.upgrade(t, input)
	require.NoError(t, err)
	updated := f.find(t, run.UUID)
	require.Equal(t, run.Revision+1, updated.Revision)
	require.Equal(t, run.PolicySHA256, updated.PolicySHA256)
	require.Equal(t, input.PolicySHA256, updated.ExecutionPolicySHA256)
	require.Equal(t, run.Pending, updated.Pending)
	require.Equal(t, run.Completed, updated.Completed)
	require.Equal(t, run.Progress, updated.Progress)
	require.Equal(t, run.AvailableAt, updated.AvailableAt)
	require.Equal(t, run.Failures, updated.Failures)
	require.Equal(t, "deferred", updated.State, "upgrade does not silently retry deferred work")
	require.Equal(t, updated, f.submit(t, request), "original request receipt still identifies the same run and policy")
	_, err = f.coordinator.Claim(t.Context(), f.token, run.UUID, uuid.NewString(), request.PolicySHA256, time.Minute)
	require.ErrorIs(t, err, models.ErrSourceRunConflict)

	// A second reviewed repair before another claim replaces no attempt history.
	secondInput := input
	secondInput.RequestUUID = uuid.NewString()
	secondInput.ExpectedRevision = updated.Revision
	secondInput.ExpectedPolicySHA256 = input.PolicySHA256
	secondInput.PolicySHA256 = strings.Repeat("c", 64)
	_, err = f.upgrade(t, secondInput)
	require.NoError(t, err)
	replay, err := f.upgrade(t, input)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	changed := input
	changed.Reason = "A different request"
	_, err = f.upgrade(t, changed)
	require.ErrorIs(t, err, models.ErrSourceRunConflict)
	changed.RequestUUID = uuid.NewString()
	_, err = f.upgrade(t, changed)
	require.ErrorIs(t, err, models.ErrSourceRunConflict, "stale revision cannot overwrite a newer review")
	updated = f.find(t, run.UUID)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceRun.Review(ctx, run.UUID, updated.Revision, "retry", f.now)
		return err
	}))
	claimed, err := f.coordinator.Claim(t.Context(), f.token, run.UUID, uuid.NewString(), secondInput.PolicySHA256, time.Minute)
	require.NoError(t, err)
	require.Nil(t, claimed, "review cannot shorten the retry deadline")
	f.now = run.AvailableAt.Add(time.Second)
	ready, err := f.coordinator.Ready(t.Context(), f.token, f.root.UUID, secondInput.PolicySHA256, 0)
	require.NoError(t, err)
	require.Equal(t, []models.SourceRunCandidate{{Sequence: run.Sequence, UUID: run.UUID}}, ready)
	ready, err = f.coordinator.Ready(t.Context(), f.token, f.root.UUID, request.PolicySHA256, 0)
	require.NoError(t, err)
	require.Empty(t, ready)
	claimed, err = f.coordinator.Claim(t.Context(), f.token, run.UUID, uuid.NewString(), secondInput.PolicySHA256, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, progress, claimed.Progress)
	third := secondInput
	third.RequestUUID = uuid.NewString()
	third.ExpectedRevision = claimed.Revision
	third.ExpectedPolicySHA256 = secondInput.PolicySHA256
	third.PolicySHA256 = strings.Repeat("d", 64)
	_, err = f.upgrade(t, third)
	require.ErrorIs(t, err, models.ErrSourceRunConflict, "a live worker cannot be upgraded")
	f.finish(t, claimed, "succeeded")
	attempts, err := f.coordinator.Attempts(t.Context(), f.token, run.UUID, 0)
	require.NoError(t, err)
	require.Len(t, attempts, 2)
	require.Equal(t, request.PolicySHA256, attempts[0].PolicySHA256)
	require.Equal(t, secondInput.PolicySHA256, attempts[1].PolicySHA256)
	require.Equal(t, "succeeded", f.submit(t, request).State)
	third.ExpectedRevision = f.find(t, run.UUID).Revision
	_, err = f.upgrade(t, third)
	require.ErrorIs(t, err, models.ErrSourceRunConflict, "completed work is immutable")
	// Restart and exact replay retain the upgrade and its original acknowledgement.
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	replay, err = f.upgrade(t, input)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec("UPDATE source_run_policy_upgrades SET reason='rewrite' WHERE request_uuid=?", input.RequestUUID)
	require.ErrorContains(t, err, "immutable")
	_, err = raw.Exec("DELETE FROM source_run_policy_upgrades WHERE request_uuid=?", input.RequestUUID)
	require.ErrorContains(t, err, "retained")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(f.db, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	anonymous := openRawDB(t, output)
	defer anonymous.Close()
	require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM source_run_policy_upgrades"))
}

func TestSourceRunPolicyUpgradeMigrationLeavesPendingWorkUnchanged(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "upgrade.sqlite")
	buildLegacyDatabase(t, path, sqlite.NativeSchemaBaseline+102, false)
	raw := openRawDB(t, path)
	defer raw.Close()
	collection, run := uuid.NewString(), uuid.NewString()
	policy := strings.Repeat("a", 64)
	tx, err := raw.Begin()
	require.NoError(t, err)
	_, err = tx.Exec("INSERT INTO source_collections(uuid) VALUES(?)", collection)
	require.NoError(t, err)
	_, err = tx.Exec(`INSERT INTO source_collection_revisions(collection_uuid,revision,label,kind,namespace,state,target_url,path_prefix,origin,reason)
VALUES(?,1,'Source','feed','native:reddit','active','https://www.reddit.com/user/example/submitted/','','review','Fixture')`, collection)
	require.NoError(t, err)
	_, err = tx.Exec(`INSERT INTO source_runs(uuid,collection_uuid,collection_revision,operation,policy_sha256,cooldown_seconds,work_key,target_key,
pending,available_at_ms,created_at_ms,updated_at_ms) VALUES(?,?,1,'enrich',?,5,?,?,?,1,1,1)`, run, collection, policy, policy, policy, `[{"since":null,"until":"2026-10-01T00:00:00Z"}]`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	repo := db.Repository()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		result, err := repo.SourceRun.Find(ctx, run)
		require.NoError(t, err)
		require.Equal(t, policy, result.PolicySHA256)
		require.Equal(t, policy, result.ExecutionPolicySHA256)
		require.Equal(t, "queued", result.State)
		require.EqualValues(t, 1, result.Revision)
		require.Len(t, result.Pending, 1)
		return nil
	}))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_run_policy_upgrades"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}
