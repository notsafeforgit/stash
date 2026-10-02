package sqlite_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stashapp/stash/pkg/translation"
	"github.com/stretchr/testify/require"
)

func activationPlan(t *testing.T, repo models.Repository, input models.TranslationActivationInput) *models.TranslationActivationPlan {
	t.Helper()
	var ret *models.TranslationActivationPlan
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.TranslationWork.PreviewActivation(ctx, input)
		return err
	}))
	return ret
}

func activateTranslationPlan(repo models.Repository, plan *models.TranslationActivationPlan, now time.Time) (*models.TranslationActivation, error) {
	var ret *models.TranslationActivation
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.TranslationWork.Activate(ctx, plan.Input, plan.PlanSHA256, now)
		return err
	})
	return ret, err
}

func TestTranslationActivationPreservesSchedulesReplaysAfterCompletionAndRestores(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	s, clock := translationService(t, repo)
	request := retainTranslationRequest(t, repo, "Shared held original")
	now := s.Durable.Now()
	first := translationJobTarget(t, repo, request.UUID, "activation-first", models.TranslationTargetSchedule{State: "held", Priority: 7}, now)
	second := translationJobTarget(t, repo, request.UUID, "activation-later", models.TranslationTargetSchedule{State: "held", Priority: 100, NotBefore: now.Add(time.Hour)}, now)
	input := models.TranslationActivationInput{UUID: uuid.NewString(), Targets: []models.TranslationTargetRef{{TargetUUID: second.UUID, Revision: 1}, {TargetUUID: first.UUID, Revision: 1}}}
	plan := activationPlan(t, repo, input)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM translation_activations"), "preview is read-only")
	receipt, err := activateTranslationPlan(repo, plan, now)
	require.NoError(t, err)
	require.Len(t, receipt.Activated, 2)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM archive_jobs"), "activation is not worker admission")
	for _, old := range []*models.TranslationTarget{first, second} {
		current := translationTarget(t, repo, old.UUID)
		require.Equal(t, 2, current.Revision)
		require.Equal(t, "pending", current.State)
		require.Equal(t, old.Priority, current.Priority)
		require.True(t, old.NotBefore.Equal(current.NotBefore))
	}
	calls := 0
	worker := translation.NewWorker(s, translation.ProviderFunc(func(context.Context, models.TranslationRequest) (models.TranslationCacheInput, error) {
		calls++
		return translationOutput(), nil
	}))
	processTranslation(t, worker)
	require.Equal(t, "completed", translationTarget(t, repo, first.UUID).State)
	require.Equal(t, "pending", translationTarget(t, repo, second.UUID).State)
	clock.Add(time.Hour.Milliseconds())
	processTranslation(t, worker)
	require.Equal(t, 1, calls, "shared request is translated once, while the later deadline is respected")
	backup := filepath.Join(t.TempDir(), "activation-backup.sqlite")
	require.NoError(t, db.Backup(backup))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(backup))
	repo = db.Repository()
	plan.Input.Targets = slices.Clone(plan.Input.Targets)
	slices.Reverse(plan.Input.Targets)
	replayed, err := activateTranslationPlan(repo, plan, s.Durable.Now())
	require.NoError(t, err)
	require.Equal(t, receipt, replayed, "lost responses replay the original receipt after worker completion and restart")
	plan.Input.UUID = uuid.NewString()
	_, err = activateTranslationPlan(repo, plan, s.Durable.Now())
	require.ErrorIs(t, err, models.ErrTranslationWorkConflict)
	anonPath := filepath.Join(t.TempDir(), "activation-anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(db, anonPath)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	anon := openRawDB(t, anonPath)
	defer anon.Close()
	for _, table := range []string{"translation_activations", "translation_activation_targets", "translation_targets"} {
		require.Zero(t, queryUint(t, anon, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, anon, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestTranslationActivationConflictsAndLateFailuresAreAtomic(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	request := retainTranslationRequest(t, repo, "Held original")
	now := time.Now().UTC()
	input := models.TranslationActivationInput{UUID: uuid.NewString()}
	for i := range 2 {
		target := translationJobTarget(t, repo, request.UUID, fmt.Sprintf("atomic-activation-%d", i), models.TranslationTargetSchedule{State: "held", Priority: 10}, now)
		input.Targets = append(input.Targets, models.TranslationTargetRef{TargetUUID: target.UUID, Revision: target.Revision})
	}
	plan := activationPlan(t, repo, input)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec(`CREATE TRIGGER activation_late_failure BEFORE INSERT ON translation_activation_targets
 WHEN NEW.target_uuid='` + plan.Input.Targets[1].TargetUUID + `' BEGIN SELECT RAISE(ABORT,'Injected late activation failure'); END`)
	require.NoError(t, err)
	require.ErrorIs(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.TranslationWork.Activate(ctx, plan.Input, plan.PlanSHA256, now)
		require.ErrorContains(t, err, "Injected late activation failure")
		return nil // Even a caller accidentally swallowing the error must roll back.
	}), models.ErrTranslationWorkAtomic)
	for _, ref := range input.Targets {
		target := translationTarget(t, repo, ref.TargetUUID)
		require.Equal(t, "held", target.State)
		require.Equal(t, ref.Revision, target.Revision)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM translation_activations"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM translation_activation_targets"))
	_, err = raw.Exec("DROP TRIGGER activation_late_failure")
	require.NoError(t, err)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.TranslationWork.ScheduleTarget(ctx, input.Targets[1].TargetUUID, 1, models.TranslationTargetSchedule{State: "held", Priority: 11, NotBefore: now}, now.Add(time.Second))
		return err
	}))
	_, err = activateTranslationPlan(repo, plan, now.Add(time.Second))
	require.ErrorIs(t, err, models.ErrTranslationWorkConflict)
	require.Equal(t, 1, translationTarget(t, repo, input.Targets[0].TargetUUID).Revision)
	input.Targets[1].Revision = 2
	plan = activationPlan(t, repo, input)
	forged := *plan
	forged.PlanSHA256 = strings.Repeat("0", 64)
	_, err = activateTranslationPlan(repo, &forged, now.Add(time.Second))
	require.ErrorIs(t, err, models.ErrTranslationWorkConflict)
	receipt, err := activateTranslationPlan(repo, plan, now.Add(time.Second))
	require.NoError(t, err)
	_, err = activateTranslationPlan(repo, &forged, now.Add(time.Second))
	require.ErrorIs(t, err, models.ErrTranslationWorkConflict, "an existing operation UUID cannot accept a different plan")
	ref := receipt.Activated[0]
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.TranslationWork.ScheduleTarget(ctx, ref.TargetUUID, ref.Revision, models.TranslationTargetSchedule{State: "held", Priority: 50, NotBefore: now}, now.Add(2*time.Second))
		return err
	}))
	replayed, err := activateTranslationPlan(repo, plan, now.Add(3*time.Second))
	require.NoError(t, err)
	require.Equal(t, receipt, replayed)
	require.Equal(t, "held", translationTarget(t, repo, ref.TargetUUID).State, "replay must preserve a later hold")
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
}

func TestTranslationActivationImportedCandidatesAreBoundedAndKeepLaterChoices(t *testing.T) {
	job := automationJob(t, "Imported original", "pending", nil)
	var targets []map[string]any
	for i := range 105 {
		targets = append(targets, automationTarget(job, fmt.Sprintf("post-%03d", i), "title", 0))
	}
	f := translationAutomationFixture(t, []map[string]any{job}, targets)
	result := advanceAutomationTranslations(t, f, 0)
	for result.State == "running" {
		result = advanceAutomationTranslations(t, f, result.LastOrdinal)
	}
	read := func(after int64, limit int) []models.AutomationTranslationCandidate {
		t.Helper()
		var ret []models.AutomationTranslationCandidate
		require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			ret, err = f.repo.AutomationTranslationImport.HeldTargets(ctx, f.manifest.UUID, f.sha, after, limit)
			return err
		}))
		return ret
	}
	first := read(0, 100)
	require.Len(t, first, 100)
	last := read(first[99].Ordinal, 100)
	require.Len(t, last, 5)
	input := models.TranslationActivationInput{UUID: uuid.NewString(), SnapshotUUID: f.manifest.UUID, ManifestSHA256: f.sha}
	for _, row := range first {
		require.Equal(t, "eligible", row.Disposition)
		input.Targets = append(input.Targets, row.TranslationTargetRef)
	}
	plan := activationPlan(t, f.repo, input)
	now := automationImportNow.Add(2 * time.Hour)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.TranslationWork.ScheduleTarget(ctx, first[0].TargetUUID, first[0].Revision, models.TranslationTargetSchedule{State: "held", Priority: 1, NotBefore: now}, now)
		return err
	}))
	_, err := activateTranslationPlan(f.repo, plan, now)
	require.ErrorIs(t, err, models.ErrTranslationWorkConflict)
	input.Targets = input.Targets[1:]
	plan = activationPlan(t, f.repo, input)
	_, err = activateTranslationPlan(f.repo, plan, now)
	require.NoError(t, err)
	rows := read(0, 100)
	for _, row := range rows {
		require.Equal(t, "changed", row.Disposition)
	}
	require.Equal(t, "held", rows[0].State)
	require.Equal(t, "pending", rows[1].State)
	// Current revision is not the imported hold, even if it is still held.
	input.UUID, input.Targets = uuid.NewString(), []models.TranslationTargetRef{{TargetUUID: rows[0].TargetUUID, Revision: rows[0].CurrentRevision}}
	require.ErrorIs(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.TranslationWork.PreviewActivation(ctx, input)
		return err
	}), models.ErrTranslationWorkConflict)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=?", last[0].PostUUID)
	require.NoError(t, err)
	forgotten := models.TranslationActivationInput{UUID: uuid.NewString(), SnapshotUUID: f.manifest.UUID, ManifestSHA256: f.sha, Targets: []models.TranslationTargetRef{last[0].TranslationTargetRef}}
	require.ErrorIs(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.TranslationWork.PreviewActivation(ctx, forgotten)
		return err
	}), models.ErrSourcePostForgotten)
	retainTranslationCache(t, f.repo, models.TranslationCacheInput{RequestUUID: last[1].RequestUUID, Status: "translated", TranslatedText: translationPointer("Cached output"), Origin: "worker"})
	readyPlan := activationPlan(t, f.repo, models.TranslationActivationInput{UUID: uuid.NewString(), SnapshotUUID: f.manifest.UUID, ManifestSHA256: f.sha, Targets: []models.TranslationTargetRef{last[1].TranslationTargetRef}})
	_, err = activateTranslationPlan(f.repo, readyPlan, now)
	require.NoError(t, err)
	ready := translationTarget(t, f.repo, last[1].TargetUUID)
	publicationTime := now
	if ready.NotBefore.After(publicationTime) {
		publicationTime = ready.NotBefore
	}
	_, err = publishTranslationTarget(f.repo, ready, publicationTime)
	require.NoError(t, err)
	last = read(first[99].Ordinal, 100)
	require.Equal(t, "post_forgotten", last[0].Disposition)
	require.Equal(t, "completed", last[1].Disposition)
	require.Equal(t, "eligible", last[2].Disposition)
	var id, parent, unused int
	var queryPlan string
	require.NoError(t, raw.QueryRow(`EXPLAIN QUERY PLAN SELECT ordinal FROM automation_translation_records INDEXED BY automation_translation_held
 WHERE snapshot_uuid=? AND ordinal>? AND disposition='held' ORDER BY ordinal LIMIT 100`, f.manifest.UUID, 0).Scan(&id, &parent, &unused, &queryPlan))
	require.Contains(t, queryPlan, "SEARCH")
	require.Contains(t, queryPlan, "automation_translation_held")
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestTranslationActivationConcurrentReplayRetainsOneReceipt(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	now := time.Now().UTC()
	request := retainTranslationRequest(t, repo, "Concurrent original")
	target := translationJobTarget(t, repo, request.UUID, "concurrent-activation", models.TranslationTargetSchedule{State: "held"}, now)
	plan := activationPlan(t, repo, models.TranslationActivationInput{UUID: uuid.NewString(), Targets: []models.TranslationTargetRef{{TargetUUID: target.UUID, Revision: 1}}})
	type outcome struct {
		value *models.TranslationActivation
		err   error
	}
	out := make(chan outcome, 4)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			value, err := activateTranslationPlan(repo, plan, now)
			out <- outcome{value, err}
		})
	}
	wg.Wait()
	close(out)
	var receipt *models.TranslationActivation
	for result := range out {
		require.NoError(t, result.err)
		if receipt == nil {
			receipt = result.value
		} else {
			require.Equal(t, receipt, result.value)
		}
	}
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM translation_activations"))
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM translation_target_history"))
}

func TestTranslationActivationStartupRejectsCorruptReceiptsWithoutWrites(t *testing.T) {
	for _, item := range []struct{ name, mutation string }{
		{"hash", "UPDATE translation_activations SET input_sha256=printf('%064d',0)"},
		{"plan", "UPDATE translation_activations SET plan=json_set(plan,'$.entries[0].not_before','2020-01-01T00:00:00Z')"},
		{"rehashed-deadline", ""},
		{"empty", "UPDATE translation_activations SET plan=json_set(plan,'$.entries',json('[]')); DELETE FROM translation_activation_targets"},
		{"missing", "DELETE FROM translation_activation_targets"},
		{"time", "UPDATE translation_activations SET created_at='2020-01-01T00:00:00Z'"},
	} {
		t.Run(item.name, func(t *testing.T) {
			db, repo := archiveTestDatabase(t)
			request := retainTranslationRequest(t, repo, "Retained activation original")
			now := time.Now().UTC()
			target := translationJobTarget(t, repo, request.UUID, "corrupt-activation", models.TranslationTargetSchedule{State: "held"}, now)
			plan := activationPlan(t, repo, models.TranslationActivationInput{UUID: uuid.NewString(), Targets: []models.TranslationTargetRef{{TargetUUID: target.UUID, Revision: 1}}})
			_, err := activateTranslationPlan(repo, plan, now)
			require.NoError(t, err)
			path := db.DatabasePath()
			require.NoError(t, db.Close())
			raw := openRawDB(t, path)
			var guard string
			require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='translation_activation_immutable'").Scan(&guard))
			_, err = raw.Exec("DROP TRIGGER translation_activation_immutable")
			require.NoError(t, err)
			if item.name == "rehashed-deadline" {
				plan.Entries[0].NotBefore = plan.Entries[0].NotBefore.Add(time.Hour)
				plan.PlanSHA256, err = archive.TranslationActivationDigest(*plan)
				require.NoError(t, err)
				body, err := json.Marshal(plan)
				require.NoError(t, err)
				_, err = raw.Exec("UPDATE translation_activations SET plan=?,plan_sha256=?", string(body), plan.PlanSHA256)
				require.NoError(t, err)
			} else {
				_, err = raw.Exec(item.mutation)
				require.NoError(t, err)
			}
			_, err = raw.Exec(guard)
			require.NoError(t, err)
			require.NoError(t, raw.Close())
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			check := sqlite.NewDatabase()
			require.Error(t, check.Open(path))
			require.NoError(t, check.Close())
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
