package sqlite_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

const catalogSurvivor = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
const catalogMerged = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

var catalogImportNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func catalogIdentityFixture(t *testing.T) (*sqlite.Database, models.Repository, models.CatalogIdentityImportInput) {
	t.Helper()
	db, repo := archiveTestDatabase(t)
	body, err := os.ReadFile("../scrape/testdata/legacy_performer_registry.json")
	require.NoError(t, err)
	input := models.CatalogIdentityImportInput{UUID: uuid.NewString(), SourceUUID: uuid.NewString(), Namespace: "stash", Document: body, AccountBindings: map[string]string{}}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `UPDATE performers SET created_at='2020-01-01 00:00:00'; UPDATE archive_entities SET created_at='2020-01-01 00:00:00' WHERE kind='performer'`, nil)
		if err != nil {
			return err
		}
		for _, key := range []string{"twitter:id:9007199254740993", "reddit:handle:deliberately-unlinked"} {
			account, err := repo.SourceAccount.Create(ctx, "native:"+strings.SplitN(key, ":", 2)[0], "Reviewed account")
			if err != nil {
				return err
			}
			input.AccountBindings[key] = account.UUID
		}
		return nil
	}))
	return db, repo, input
}

func catalogPreview(t *testing.T, repo models.Repository, input models.CatalogIdentityImportInput) *models.CatalogIdentityImportPlan {
	t.Helper()
	var plan *models.CatalogIdentityImportPlan
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		plan, err = repo.CatalogIdentityImport.Preview(ctx, input, catalogImportNow)
		return err
	}))
	return plan
}

func catalogApply(t *testing.T, repo models.Repository, input models.CatalogIdentityImportInput, plan *models.CatalogIdentityImportPlan) *models.CatalogIdentityImport {
	t.Helper()
	var receipt *models.CatalogIdentityImport
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		receipt, err = repo.CatalogIdentityImport.Apply(ctx, input, plan.PlanSHA256, catalogImportNow)
		return err
	}))
	return receipt
}

func changeCatalogDocument(t *testing.T, input models.CatalogIdentityImportInput, fn func(map[string][]map[string]any)) models.CatalogIdentityImportInput {
	t.Helper()
	var doc struct {
		CapturedAt string                      `json:"captured_at"`
		Tables     map[string][]map[string]any `json:"tables"`
		External   map[string]int              `json:"external_tables"`
	}
	require.NoError(t, json.Unmarshal(input.Document, &doc))
	fn(doc.Tables)
	var err error
	input.Document, err = json.Marshal(doc)
	require.NoError(t, err)
	return input
}

func TestCatalogIdentityImportPreservesUUIDsSelectedFieldsMergeAndUnlink(t *testing.T) {
	db, repo, input := catalogIdentityFixture(t)
	before := archiveFind(t, repo, models.ArchivePerformer, 71)
	plan := catalogPreview(t, repo, input)
	require.Equal(t, "adopt", plan.Identities[0].Action)
	require.Equal(t, "redirect", plan.Identities[1].Action)
	require.Equal(t, "review", plan.Identities[2].Action)
	require.Equal(t, 13, plan.RecordCount)
	require.Equal(t, before, archiveFind(t, repo, models.ArchivePerformer, 71), "preview cannot mutate identities")
	first := catalogApply(t, repo, input, plan)
	require.Equal(t, catalogSurvivor, archiveFind(t, repo, models.ArchivePerformer, 71).UUID)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, id := range []string{before.UUID, catalogMerged} {
			entity, err := repo.ArchiveEntity.Resolve(ctx, id)
			require.NoError(t, err)
			require.Equal(t, catalogSurvivor, entity.UUID)
		}
		performer, err := repo.Performer.Find(ctx, 71)
		require.NoError(t, err)
		require.Equal(t, "Shared name", performer.Name)
		deleted, err := repo.Performer.Find(ctx, 999)
		require.NoError(t, err)
		require.Nil(t, deleted)
		linked, err := repo.SourceAccount.Ownership(ctx, input.AccountBindings["twitter:id:9007199254740993"])
		require.NoError(t, err)
		require.Equal(t, catalogSurvivor, *linked.PerformerUUID)
		unlinked, err := repo.SourceAccount.Ownership(ctx, input.AccountBindings["reddit:handle:deliberately-unlinked"])
		require.NoError(t, err)
		require.Equal(t, models.AccountOwnershipUnlinked, unlinked.State)
		rows := []models.CatalogIdentityImportRecord{}
		for after := int64(0); ; {
			page, err := repo.CatalogIdentityImport.Records(ctx, input.UUID, after, 2)
			require.NoError(t, err)
			if len(page) == 0 {
				break
			}
			rows = append(rows, page...)
			after = page[len(page)-1].Sequence
		}
		require.Len(t, rows, 13)
		for _, r := range rows {
			if r.Table == "catalog_metadata_accounts" || r.Table == "catalog_metadata_performers" {
				require.Equal(t, "superseded", r.Outcome)
			}
			if r.Table == "performer_identities" && strings.Contains(r.SourceKey, catalogSurvivor) {
				require.Contains(t, string(r.Evidence), "9007199254740993")
				require.Equal(t, catalogSurvivor, *r.ArchiveUUID)
			}
		}
		return nil
	}))
	// A replay after a later native unlink/UUID adoption must return the original
	// receipt, preserve that unlink, and allow FK UUID cascades through evidence.
	nextUUID := uuid.NewString()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		entity, err := repo.ArchiveEntity.Find(ctx, catalogSurvivor)
		if err != nil {
			return err
		}
		if _, err := repo.ArchiveEntity.AdoptUUID(ctx, catalogSurvivor, nextUUID, entity.Revision); err != nil {
			return err
		}
		account, err := repo.SourceAccount.Find(ctx, input.AccountBindings["twitter:id:9007199254740993"])
		if err != nil {
			return err
		}
		_, err = repo.SourceAccount.DecideOwnership(ctx, models.AccountOwnershipInput{AccountUUID: account.UUID, ExpectedAccountRevision: account.Revision, State: models.AccountOwnershipUnlinked, Origin: "review", Reason: "Later choice"})
		return err
	}))
	require.Equal(t, first, catalogApply(t, repo, input, plan))
	require.Equal(t, nextUUID, archiveFind(t, repo, models.ArchivePerformer, 71).UUID)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		receipt, err := repo.CatalogIdentityImport.Find(ctx, input.UUID)
		require.NoError(t, err)
		require.Equal(t, first, receipt)
		owner, err := repo.SourceAccount.Ownership(ctx, input.AccountBindings["twitter:id:9007199254740993"])
		require.NoError(t, err)
		require.Equal(t, models.AccountOwnershipUnlinked, owner.State)
		return nil
	}))
	input.UUID = uuid.NewString()
	require.ErrorIs(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.CatalogIdentityImport.Preview(ctx, input, catalogImportNow)
		return err
	}), models.ErrCatalogIdentityImportConflict)
}

func TestCatalogIdentityImportStalePreviewAndAtomicFailure(t *testing.T) {
	db, repo, input := catalogIdentityFixture(t)
	plan := catalogPreview(t, repo, input)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, "UPDATE performers SET details='A later native edit' WHERE id=71", nil)
		return err
	}))
	require.ErrorIs(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.CatalogIdentityImport.Apply(ctx, input, plan.PlanSHA256, catalogImportNow)
		return err
	}), models.ErrCatalogIdentityImportConflict)
	plan = catalogPreview(t, repo, input)
	before := archiveFind(t, repo, models.ArchivePerformer, 71)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `CREATE TRIGGER fail_identity_import BEFORE INSERT ON catalog_identity_import_records BEGIN SELECT RAISE(ABORT,'fixture late write failure'); END`, nil)
		return err
	}))
	require.ErrorContains(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.CatalogIdentityImport.Apply(ctx, input, plan.PlanSHA256, catalogImportNow)
		return err
	}), "fixture late write failure")
	require.ErrorContains(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.CatalogIdentityImport.Apply(ctx, input, plan.PlanSHA256, catalogImportNow)
		require.ErrorContains(t, err, "fixture late write failure")
		return nil // A caller cannot accidentally commit by swallowing the error.
	}), "catalog identity import did not finish atomically")
	require.Equal(t, before, archiveFind(t, repo, models.ArchivePerformer, 71))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		receipt, err := repo.CatalogIdentityImport.Find(ctx, input.UUID)
		require.NoError(t, err)
		require.Nil(t, receipt)
		owner, err := repo.SourceAccount.Ownership(ctx, input.AccountBindings["twitter:id:9007199254740993"])
		require.NoError(t, err)
		require.Nil(t, owner)
		return nil
	}))
}

func TestCatalogIdentityImportDoesNotGuessOrOverwriteOwnership(t *testing.T) {
	db, repo, input := catalogIdentityFixture(t)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		account, err := repo.SourceAccount.Find(ctx, input.AccountBindings["twitter:id:9007199254740993"])
		if err != nil {
			return err
		}
		_, err = repo.SourceAccount.DecideOwnership(ctx, models.AccountOwnershipInput{AccountUUID: account.UUID, ExpectedAccountRevision: account.Revision, State: models.AccountOwnershipUnlinked, Origin: "review"})
		return err
	}))
	plan := catalogPreview(t, repo, input)
	for _, a := range plan.Ownership {
		if a.AccountKey == "twitter:id:9007199254740993" {
			require.Equal(t, "review", a.Action)
			require.Equal(t, "native_ownership_already_decided", a.Reason)
		}
	}
	// Local row 71 has been reused since the frozen binding was saved.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `UPDATE archive_entities SET created_at='2026-09-30 00:00:00' WHERE performer_id=71`, nil)
		return err
	}))
	plan = catalogPreview(t, repo, input)
	require.Equal(t, "review", plan.Identities[0].Action)
	require.Equal(t, "local_performer_id_may_have_been_reused", plan.Identities[0].Reason)
	catalogApply(t, repo, input, plan)
	require.NotEqual(t, catalogSurvivor, archiveFind(t, repo, models.ArchivePerformer, 71).UUID)
}

func TestCatalogIdentityImportConflictingBindingsAndMalformedSnapshots(t *testing.T) {
	_, repo, input := catalogIdentityFixture(t)
	conflict := changeCatalogDocument(t, input, func(tables map[string][]map[string]any) {
		row := tables["performer_identity_bindings"][0]
		copyRow := map[string]any{}
		for k, v := range row {
			copyRow[k] = v
		}
		copyRow["performer_id"] = "72"
		tables["performer_identity_bindings"] = append(tables["performer_identity_bindings"], copyRow)
	})
	plan := catalogPreview(t, repo, conflict)
	require.Equal(t, "multiple_local_performer_bindings", plan.Identities[0].Reason)
	for _, test := range []struct {
		name   string
		change func(map[string][]map[string]any)
	}{
		{"redirect cycle", func(tables map[string][]map[string]any) {
			tables["performer_identities"][0]["redirect_to"] = catalogMerged
		}},
		{"missing merge target", func(tables map[string][]map[string]any) {
			tables["performer_identities"][1]["redirect_to"] = uuid.NewString()
		}},
		{"duplicate identity", func(tables map[string][]map[string]any) {
			tables["performer_identities"] = append(tables["performer_identities"], tables["performer_identities"][0])
		}},
		{"unknown column", func(tables map[string][]map[string]any) { tables["performer_identities"][0]["extra"] = true }},
		{"duplicate embedded key", func(tables map[string][]map[string]any) {
			tables["performer_identities"][0]["profile_json"] = `{"name":"one","name":"two"}`
		}},
		{"wrong binding redirect", func(tables map[string][]map[string]any) {
			tables["performer_identity_bindings"][1]["redirect_to"] = "72"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := scrape.PrepareCatalogIdentities(changeCatalogDocument(t, input, test.change))
			require.ErrorIs(t, err, models.ErrCatalogIdentityImportInvalid)
		})
	}
	// Contradictory saved choices for one explicitly bound native account are
	// both review items, including an unresolved/legacy choice.
	input.AccountBindings["reddit:handle:deliberately-unlinked"] = input.AccountBindings["twitter:id:9007199254740993"]
	plan = catalogPreview(t, repo, input)
	for _, a := range plan.Ownership {
		if a.AccountRevision > 0 {
			require.Equal(t, "review", a.Action)
			require.Equal(t, "conflicting_saved_account_choices", a.Reason)
		}
	}
}

func TestCatalogIdentityImportCoalescesSameChoiceAndGuardsHistoricalBindings(t *testing.T) {
	db, repo, input := catalogIdentityFixture(t)
	input.AccountBindings["instagram (from aggregator reddit):handle:someone"] = input.AccountBindings["twitter:id:9007199254740993"]
	plan := catalogPreview(t, repo, input)
	// Creating a live row for a recorded old merged ID changes the review plan.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `INSERT INTO performers(id,created_at,updated_at) VALUES(999,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP); INSERT INTO performer_names(performer_id,name,position) VALUES(999,'Reused old ID',0)`, nil)
		return err
	}))
	require.ErrorIs(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.CatalogIdentityImport.Apply(ctx, input, plan.PlanSHA256, catalogImportNow)
		return err
	}), models.ErrCatalogIdentityImportConflict)
	plan = catalogPreview(t, repo, input)
	catalogApply(t, repo, input, plan)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		performer, err := repo.Performer.Find(ctx, 999)
		require.NoError(t, err)
		require.Equal(t, "Reused old ID", performer.Name)
		history, err := repo.SourceAccount.OwnershipHistory(ctx, input.AccountBindings["twitter:id:9007199254740993"], 0, 100)
		require.NoError(t, err)
		require.Len(t, history, 1)
		return nil
	}))
	// An anonymised copy must not leak retained profiles, account keys or UUIDs.
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(db, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	raw := openRawDB(t, output)
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM catalog_identity_imports"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM catalog_identity_import_records"))
}
