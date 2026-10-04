package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stashapp/stash/pkg/txn"
	"github.com/stretchr/testify/require"
)

func removeDiscoveryActivationSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeDiscoveryPublicationSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE discovery_activation_targets; DROP TABLE discovery_activations;
 DELETE FROM native_migration_history WHERE version=1000073`)
	require.NoError(t, err)
}

func discoveryActivationInput(f *discoveryMatchFixture) models.DiscoveryActivationInput {
	listing := f.listing.DiscoveryListingInput
	listing.UUID = uuid.NewString()
	return models.DiscoveryActivationInput{UUID: uuid.NewString(), ManifestSHA256: f.sha, Listing: listing,
		Targets: []models.DiscoveryActivationSelection{{SourceOrdinal: f.input.SourceOrdinal, SourceSHA256: f.input.ExpectedSourceSHA256}}}
}

func previewDiscoveryActivation(t *testing.T, f *discoveryMatchFixture, input models.DiscoveryActivationInput) *models.DiscoveryActivationPlan {
	t.Helper()
	var plan *models.DiscoveryActivationPlan
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		plan, err = f.repo.DiscoveryMatch.PreviewActivation(ctx, input)
		return err
	}))
	return plan
}

func activateDiscoveryPlan(t *testing.T, f *discoveryMatchFixture, plan *models.DiscoveryActivationPlan) (*models.DiscoveryActivation, error) {
	t.Helper()
	var result *models.DiscoveryActivation
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.DiscoveryMatch.Activate(ctx, plan.Input, plan.PlanSHA256, f.now)
		return err
	})
	return result, err
}

func TestDiscoveryActivationKeepsOriginalPlanAcrossProgressAndNativeEdits(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	input := discoveryActivationInput(f)
	plan := previewDiscoveryActivation(t, f, input)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM discovery_listings"), "preview is read-only")
	receipt, err := activateDiscoveryPlan(t, f, plan)
	require.NoError(t, err)
	require.Equal(t, *plan, receipt.DiscoveryActivationPlan)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM archive_jobs"), "activation neither fetches nor admits source jobs")
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		f.listing, err = f.repo.DiscoveryJob.Listing(ctx, plan.Input.Listing.UUID)
		if err == nil {
			f.target, err = f.repo.DiscoveryMatch.Target(ctx, plan.Entries[0].TargetUUID)
		}
		return err
	}))
	f.append(t, listingContinuation(t, f.page, nil, nil, true))
	require.True(t, f.advance(t, 0).Complete)
	replayed, err := activateDiscoveryPlan(t, f, plan)
	require.NoError(t, err)
	require.Equal(t, receipt, replayed)
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten',revision=revision+1 WHERE uuid=?", f.target.PostUUID)
	require.NoError(t, err)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		collection, err := f.repo.SourceCollection.Find(ctx, f.listing.CollectionUUID)
		if err != nil {
			return err
		}
		definition := collection.SourceCollectionDefinition
		definition.State = "disabled"
		_, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
		return err
	}))
	backup := filepath.Join(t.TempDir(), "receipt-backup.sqlite")
	require.NoError(t, f.db.Backup(backup))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(backup))
	f.repo = f.db.Repository()
	replayed, err = activateDiscoveryPlan(t, f, plan)
	require.NoError(t, err)
	require.Equal(t, receipt, replayed, "lost responses replay original review after restart, comparison, retirement and forgetting")
	plan.Input.UUID = uuid.NewString()
	_, err = activateDiscoveryPlan(t, f, plan)
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	path := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(f.db, path)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	checked := sqlite.NewDatabase()
	defer checked.Close()
	require.NoError(t, checked.Open(path))
}

func TestDiscoveryActivationRejectsChangedSourceAndRollsBackCaughtFailures(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	plan := previewDiscoveryActivation(t, f, discoveryActivationInput(f))
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec("CREATE TRIGGER stop_discovery_activation BEFORE INSERT ON discovery_activation_targets BEGIN SELECT RAISE(ABORT,'fixture interruption'); END")
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.DiscoveryMatch.Activate(ctx, plan.Input, plan.PlanSHA256, f.now)
		require.ErrorContains(t, err, "fixture interruption")
		return nil
	})
	require.ErrorIs(t, err, models.ErrDiscoveryAtomic)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_activations"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM discovery_listings"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM discovery_match_targets"))
	_, err = raw.Exec("DROP TRIGGER stop_discovery_activation")
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
			_, _, err := f.db.ExecSQL(ctx, "UPDATE source_posts SET revision=revision+1 WHERE uuid=?", []any{plan.Entries[0].PostUUID})
			return err
		})
		_, err := f.repo.DiscoveryMatch.Activate(ctx, plan.Input, plan.PlanSHA256, f.now)
		return err
	})
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM discovery_activations"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM discovery_listings"))
	_, err = activateDiscoveryPlan(t, f, plan)
	require.NoError(t, err, "both the native edit and partial activation rolled back")
}

func TestDiscoveryActivationRejectsWrongHashesRevisionsAndReplayInputs(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	input := discoveryActivationInput(f)
	plan := previewDiscoveryActivation(t, f, input)
	for _, change := range []func(*models.DiscoveryActivationInput){
		func(in *models.DiscoveryActivationInput) { in.ManifestSHA256 = strings.Repeat("b", 64) },
		func(in *models.DiscoveryActivationInput) { in.Targets[0].SourceSHA256 = strings.Repeat("b", 64) },
		func(in *models.DiscoveryActivationInput) { in.Listing.CollectionRevision++ },
		func(in *models.DiscoveryActivationInput) {
			in.Listing.ProfileURL = "https://www.reddit.com/user/other/submitted/"
		},
		func(in *models.DiscoveryActivationInput) {
			in.Listing.NotBefore = in.Listing.NotBefore.Add(-time.Minute)
		},
	} {
		body, err := json.Marshal(input)
		require.NoError(t, err)
		var changed models.DiscoveryActivationInput
		require.NoError(t, json.Unmarshal(body, &changed))
		change(&changed)
		err = f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.DiscoveryMatch.PreviewActivation(ctx, changed)
			return err
		})
		require.ErrorIs(t, err, models.ErrDiscoveryConflict)
	}
	_, err := activateDiscoveryPlan(t, f, plan)
	require.NoError(t, err)
	plan.Input.Listing.PolicySHA256 = strings.Repeat("c", 64)
	_, err = activateDiscoveryPlan(t, f, plan)
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
}
