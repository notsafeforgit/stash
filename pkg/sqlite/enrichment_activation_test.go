package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func enrichmentSelection(target *models.EnrichmentTarget, collectionRevision int) models.EnrichmentActivationSelection {
	return models.EnrichmentActivationSelection{EnrichmentTargetRef: models.EnrichmentTargetRef{TargetUUID: target.UUID, Revision: target.Revision}, CollectionRevision: collectionRevision}
}

func removeEnrichmentActivationSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeAutomationCheckpointSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE enrichment_activation_targets; DROP TABLE enrichment_activations;
 DROP INDEX automation_enrichment_held; DROP INDEX automation_enrichment_held_targets;
 DELETE FROM native_migration_history WHERE version=1000059`)
	require.NoError(t, err)
}

func reviseEnrichmentCollection(repo models.Repository, id string, expected int, input models.SourceCollectionInput) (*models.SourceCollection, error) {
	input.UUID, input.ExpectedRevision = id, expected
	var result *models.SourceCollection
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		result, err = repo.SourceCollection.Put(ctx, input)
		return err
	})
	return result, err
}

func enrichmentActivationPlan(t *testing.T, repo models.Repository, input models.EnrichmentActivationInput) *models.EnrichmentActivationPlan {
	t.Helper()
	var ret *models.EnrichmentActivationPlan
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.EnrichmentWork.PreviewActivation(ctx, input)
		return err
	}))
	return ret
}

func activateEnrichmentPlan(repo models.Repository, plan *models.EnrichmentActivationPlan, now time.Time) (*models.EnrichmentActivation, error) {
	var ret *models.EnrichmentActivation
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.EnrichmentWork.Activate(ctx, plan.Input, plan.PlanSHA256, now)
		return err
	})
	return ret, err
}

func readEnrichmentTarget(t *testing.T, repo models.Repository, id string) *models.EnrichmentTarget {
	t.Helper()
	var ret *models.EnrichmentTarget
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.EnrichmentWork.Target(ctx, id)
		return err
	}))
	return ret
}

func TestEnrichmentActivationPreservesSchedulesReplaysAndRestores(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	input, collection := enrichmentInput(t, repo)
	now := time.Now().UTC()
	first := retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "held", Priority: 7, Reason: "migration_review"}, now)
	url, err := observePostURL(repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(input.PostUUID), URL: "https://twitter.com/source/status/123"})
	require.NoError(t, err)
	input.URLUUID = url.URLUUID
	second := retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "held", Priority: 100, NotBefore: now.Add(time.Hour)}, now)
	plan := enrichmentActivationPlan(t, repo, models.EnrichmentActivationInput{UUID: uuid.NewString(), Targets: []models.EnrichmentActivationSelection{enrichmentSelection(second, 1), enrichmentSelection(first, 1)}})
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_activations"))
	receipt, err := activateEnrichmentPlan(repo, plan, now)
	require.NoError(t, err)
	for _, prior := range []*models.EnrichmentTarget{first, second} {
		current := readEnrichmentTarget(t, repo, prior.UUID)
		require.Equal(t, 2, current.Revision)
		require.Equal(t, "pending", current.State)
		require.Empty(t, current.Reason)
		require.Equal(t, prior.Priority, current.Priority)
		require.True(t, prior.NotBefore.Equal(current.NotBefore))
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM archive_jobs"))
	capture := enrichmentCapture(t, repo, first.EnrichmentTargetInput, "gallery-dl-enrichment")
	_, err = completeEnrichment(repo, models.EnrichmentCompletionInput{UUID: uuid.NewString(), TargetUUID: first.UUID, ExpectedRevision: 2, CaptureUUIDs: []string{capture}}, now)
	require.NoError(t, err)
	_, err = scheduleEnrichment(repo, readEnrichmentTarget(t, repo, second.UUID), models.EnrichmentSchedule{State: "held", Priority: 99, NotBefore: now.Add(2 * time.Hour)}, now.Add(time.Minute))
	require.NoError(t, err)
	_, err = reviseEnrichmentCollection(repo, collection.UUID, collection.Revision, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
		Label: "Retired later", Kind: collection.Kind, Namespace: collection.Namespace, State: "retired", TargetURL: collection.TargetURL}})
	require.NoError(t, err)
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten',revision=revision+1 WHERE uuid=?", input.PostUUID)
	require.NoError(t, err)
	backup := filepath.Join(t.TempDir(), "activation-backup.sqlite")
	require.NoError(t, db.Backup(backup))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(backup))
	repo = db.Repository()
	plan.Input.Targets = slices.Clone(plan.Input.Targets)
	slices.Reverse(plan.Input.Targets)
	replayed, err := activateEnrichmentPlan(repo, plan, now.Add(2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, receipt, replayed, "replay preserves the historical receipt after completion, hold, retirement and forgetting")
	require.Equal(t, "held", readEnrichmentTarget(t, repo, second.UUID).State)
	plan.Input.UUID = uuid.NewString()
	_, err = activateEnrichmentPlan(repo, plan, now.Add(2*time.Hour))
	require.Error(t, err)
	anonPath := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(db, anonPath)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	anon := openRawDB(t, anonPath)
	defer anon.Close()
	for _, table := range []string{"enrichment_activations", "enrichment_activation_targets", "enrichment_targets"} {
		require.Zero(t, queryUint(t, anon, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, anon, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestEnrichmentActivationRejectsStalePostAndCollectionAndRollsBackLateFailure(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	input, collection := enrichmentInput(t, repo)
	now := time.Now().UTC()
	held := retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "held", Priority: 10}, now)
	request := models.EnrichmentActivationInput{UUID: uuid.NewString(), Targets: []models.EnrichmentActivationSelection{enrichmentSelection(held, 1)}}
	plan := enrichmentActivationPlan(t, repo, request)
	_, err := observePostURL(repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(input.PostUUID), URL: "https://x.com/source/status/123?context=updated"})
	require.NoError(t, err)
	_, err = activateEnrichmentPlan(repo, plan, now)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict, "new source evidence requires a fresh preview")
	plan = enrichmentActivationPlan(t, repo, request)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec(`CREATE TRIGGER activation_late_failure BEFORE INSERT ON enrichment_activation_targets BEGIN SELECT RAISE(ABORT,'Injected late activation failure'); END`)
	require.NoError(t, err)
	require.ErrorIs(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.EnrichmentWork.Activate(ctx, plan.Input, plan.PlanSHA256, now)
		require.ErrorContains(t, err, "Injected late activation failure")
		return nil
	}), models.ErrEnrichmentAtomic)
	require.Equal(t, held, readEnrichmentTarget(t, repo, held.UUID))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_activations"))
	_, err = raw.Exec("DROP TRIGGER activation_late_failure")
	require.NoError(t, err)
	forged := *plan
	forged.PlanSHA256 = strings.Repeat("0", 64)
	_, err = activateEnrichmentPlan(repo, &forged, now)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	_, err = reviseEnrichmentCollection(repo, collection.UUID, collection.Revision, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
		Label: "Changed after review", Kind: collection.Kind, Namespace: collection.Namespace, State: "active", TargetURL: collection.TargetURL}})
	require.NoError(t, err)
	_, err = activateEnrichmentPlan(repo, plan, now)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	require.Equal(t, held, readEnrichmentTarget(t, repo, held.UUID))
}

func TestEnrichmentActivationImportedAliasesAppearOnceAtLatestHold(t *testing.T) {
	for _, reschedule := range []bool{false, true} {
		t.Run(fmt.Sprint(reschedule), func(t *testing.T) {
			catalog := relationFixture(t, nil)
			advanceCatalogEnrichment(t, catalog, 0)
			progress := advanceRelations(t, catalog, 0)
			for progress.State == "running" {
				progress = advanceRelations(t, catalog, progress.LastOrdinal)
			}
			jobs := []map[string]any{legacyEnrichmentJob(relationTwitterPost, "pending"), legacyEnrichmentJob("twitter:post:original-local-alias", "pending")}
			for _, job := range jobs {
				job["platform"], job["account_key"], job["url"] = "twitter", relationTwitterAccount, "https://x.com/source/status/123"
			}
			if reschedule {
				jobs[1]["priority"], jobs[1]["next_attempt"] = 90, automationImportNow.Add(24*time.Hour).Unix()
			}
			f := enrichmentAutomationFixture(t, catalog, map[string][]map[string]any{"enrichment_jobs": jobs})
			read := func(after int64) ([]models.AutomationEnrichmentCandidate, error) {
				var result []models.AutomationEnrichmentCandidate
				err := f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
					var err error
					result, err = f.repo.AutomationEnrichmentImport.HeldTargets(ctx, f.manifest.UUID, f.sha, after, 1)
					return err
				})
				return result, err
			}
			_, err := read(0)
			require.ErrorIs(t, err, models.ErrEnrichmentConflict, "mapping must finish before discovery")
			advanceAutomationEnrichment(t, f, 0)
			rows, err := read(0)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.EqualValues(t, 2, rows[0].Ordinal)
			require.Equal(t, "collection_disabled", rows[0].Disposition)
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				c, err := f.repo.SourceCollection.Find(ctx, rows[0].CollectionUUID)
				if err != nil {
					return err
				}
				definition := c.SourceCollectionDefinition
				definition.State = "active"
				_, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: c.UUID, ExpectedRevision: c.Revision, Origin: "review", SourceCollectionDefinition: definition})
				return err
			}))
			rows, err = read(0)
			require.NoError(t, err)
			require.Equal(t, "eligible", rows[0].Disposition)
			require.Equal(t, 2, rows[0].ActivationCollectionRevision)
			require.NotEqual(t, rows[0].TargetUUID, rows[0].ReleasedTargetUUID)
			if reschedule {
				require.Equal(t, 2, rows[0].Revision)
			} else {
				require.Equal(t, 1, rows[0].Revision)
			}
			last, err := read(rows[0].Ordinal)
			require.NoError(t, err)
			require.Empty(t, last)
			request := models.EnrichmentActivationInput{UUID: uuid.NewString(), SnapshotUUID: f.manifest.UUID, ManifestSHA256: f.sha, Targets: []models.EnrichmentActivationSelection{{EnrichmentTargetRef: rows[0].EnrichmentTargetRef, CollectionRevision: rows[0].ActivationCollectionRevision}}}
			plan := enrichmentActivationPlan(t, f.repo, request)
			request.ManifestSHA256 = strings.Repeat("0", 64)
			require.ErrorIs(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				_, err := f.repo.EnrichmentWork.PreviewActivation(ctx, request)
				return err
			}), models.ErrEnrichmentConflict)
			receipt, err := activateEnrichmentPlan(f.repo, plan, automationImportNow.Add(2*time.Hour))
			require.NoError(t, err)
			old := readEnrichmentTarget(t, f.repo, rows[0].TargetUUID)
			require.Equal(t, "excluded", old.State)
			require.Equal(t, "activation_rebound", old.Reason)
			replacement := readEnrichmentTarget(t, f.repo, receipt.Activated[0].TargetUUID)
			require.Equal(t, "pending", replacement.State)
			require.Equal(t, old.Priority, replacement.Priority)
			require.True(t, old.NotBefore.Equal(replacement.NotBefore))
			rows, err = read(0)
			require.NoError(t, err)
			require.Equal(t, "changed", rows[0].Disposition)
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
		})
	}
}

func TestEnrichmentActivationAuditRejectsChangedScopeAndHistory(t *testing.T) {
	for _, field := range []string{"url", "post_revision", "not_before", "input_hash", "missing"} {
		t.Run(field, func(t *testing.T) {
			db, repo := archiveTestDatabase(t)
			input, _ := enrichmentInput(t, repo)
			now := time.Now().UTC()
			held := retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "held"}, now)
			plan := enrichmentActivationPlan(t, repo, models.EnrichmentActivationInput{UUID: uuid.NewString(), Targets: []models.EnrichmentActivationSelection{enrichmentSelection(held, 1)}})
			_, err := activateEnrichmentPlan(repo, plan, now)
			require.NoError(t, err)
			path := db.DatabasePath()
			require.NoError(t, db.Close())
			raw := openRawDB(t, path)
			var guard string
			require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='enrichment_activation_immutable'").Scan(&guard))
			_, err = raw.Exec("DROP TRIGGER enrichment_activation_immutable")
			require.NoError(t, err)
			switch field {
			case "input_hash":
				_, err = raw.Exec("UPDATE enrichment_activations SET input_sha256=printf('%064d',0)")
			case "missing":
				_, err = raw.Exec("DELETE FROM enrichment_activation_targets")
			default:
				switch field {
				case "url":
					plan.Entries[0].URL = "https://x.com/other/status/123"
				case "post_revision":
					plan.Entries[0].PostRevision++
				case "not_before":
					plan.Entries[0].NotBefore = now.Add(time.Hour)
				}
				plan.PlanSHA256, err = archive.EnrichmentActivationDigest(*plan)
				require.NoError(t, err)
				body, marshalErr := json.Marshal(plan)
				require.NoError(t, marshalErr)
				_, err = raw.Exec("UPDATE enrichment_activations SET plan=?,plan_sha256=?", string(body), plan.PlanSHA256)
			}
			require.NoError(t, err)
			_, err = raw.Exec(guard)
			require.NoError(t, err)
			require.NoError(t, raw.Close())
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			check := sqlite.NewDatabase()
			require.Error(t, check.AuditForTesting(path))
			require.NoError(t, check.Close())
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestEnrichmentActivationPreservesAnExistingNativeDestination(t *testing.T) {
	catalog := enrichmentImportFixture(t, 1, nil)
	advanceCatalogEnrichment(t, catalog, 0)
	f := enrichmentAutomationFixture(t, catalog, map[string][]map[string]any{
		"enrichment_jobs": {legacyEnrichmentJob("reddit:post:receipt000", "pending")},
	})
	advanceAutomationEnrichment(t, f, 0)
	var candidate models.AutomationEnrichmentCandidate
	read := func() {
		t.Helper()
		require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			rows, err := f.repo.AutomationEnrichmentImport.HeldTargets(ctx, f.manifest.UUID, f.sha, 0, 100)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			candidate = rows[0]
			return nil
		}))
	}
	read()
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		collection, err := f.repo.SourceCollection.Find(ctx, candidate.CollectionUUID)
		if err != nil {
			return err
		}
		definition := collection.SourceCollectionDefinition
		definition.State = "active"
		_, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: collection.UUID,
			ExpectedRevision: collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
		return err
	}))
	read()
	require.Equal(t, "eligible", candidate.Disposition)
	native := retainEnrichment(t, f.repo, models.EnrichmentTargetInput{PostUUID: candidate.PostUUID, URLUUID: candidate.URLUUID,
		CollectionUUID: candidate.CollectionUUID, CollectionRevision: 2, Policy: candidate.Policy, Origin: "review"},
		models.EnrichmentSchedule{State: "excluded", Priority: 73, NotBefore: automationImportNow.Add(24 * time.Hour), Reason: "owner_excluded"}, automationImportNow.Add(2*time.Hour))
	read()
	require.Equal(t, "replacement_exists", candidate.Disposition)
	input := models.EnrichmentActivationInput{UUID: uuid.NewString(), SnapshotUUID: f.manifest.UUID, ManifestSHA256: f.sha,
		Targets: []models.EnrichmentActivationSelection{{EnrichmentTargetRef: candidate.EnrichmentTargetRef, CollectionRevision: 2}}}
	require.ErrorIs(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.EnrichmentWork.PreviewActivation(ctx, input)
		return err
	}), models.ErrEnrichmentConflict)
	require.Equal(t, native, readEnrichmentTarget(t, f.repo, native.UUID))
	require.Equal(t, "held", readEnrichmentTarget(t, f.repo, candidate.TargetUUID).State)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}
