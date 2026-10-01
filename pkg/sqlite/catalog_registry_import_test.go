package sqlite_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

type registryTestDocument struct {
	CapturedAt string                      `json:"captured_at"`
	Tables     map[string][]map[string]any `json:"tables"`
	External   map[string]int              `json:"external_tables"`
}

func registryFixture(t *testing.T, alter func(*registryTestDocument)) (*sqlite.Database, models.Repository, models.CatalogRegistryImportInput, models.CatalogIdentityImportInput) {
	t.Helper()
	db, repo, parent := catalogIdentityFixture(t)
	body, err := os.ReadFile("../scrape/testdata/legacy_registry.json")
	require.NoError(t, err)
	var document, identities registryTestDocument
	require.NoError(t, json.Unmarshal(body, &document))
	require.NoError(t, json.Unmarshal(parent.Document, &identities))
	if alter != nil {
		alter(&document)
	}
	identities.External = map[string]int{}
	document.External = map[string]int{}
	for table, rows := range document.Tables {
		identities.External[table] = len(rows)
	}
	for table, rows := range identities.Tables {
		document.External[table] = len(rows)
	}
	parent.Document, err = json.Marshal(identities)
	require.NoError(t, err)
	catalogApply(t, repo, parent, catalogPreview(t, repo, parent))
	body, err = json.Marshal(document)
	require.NoError(t, err)
	return db, repo, models.CatalogRegistryImportInput{UUID: uuid.NewString(), SourceUUID: parent.SourceUUID, IdentityImportUUID: parent.UUID, Document: body}, parent
}

func registryPreview(t *testing.T, repo models.Repository, input models.CatalogRegistryImportInput) *models.CatalogRegistryImportPlan {
	t.Helper()
	var plan *models.CatalogRegistryImportPlan
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		plan, err = repo.CatalogRegistryImport.Preview(ctx, input, catalogImportNow)
		return err
	}))
	return plan
}

func registryApply(t *testing.T, repo models.Repository, input models.CatalogRegistryImportInput, plan *models.CatalogRegistryImportPlan) *models.CatalogRegistryImport {
	t.Helper()
	var receipt *models.CatalogRegistryImport
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		receipt, err = repo.CatalogRegistryImport.Apply(ctx, input, plan.PlanSHA256, catalogImportNow)
		return err
	}))
	return receipt
}

func registryKey(t *testing.T, plan *models.CatalogRegistryImportPlan, key string) models.CatalogRegistryKeyAction {
	t.Helper()
	for _, a := range plan.AccountKeys {
		if a.AccountKey == key {
			return a
		}
	}
	t.Fatalf("missing key %s", key)
	return models.CatalogRegistryKeyAction{}
}

func TestCatalogRegistryImportPreservesAccountsCollectionsAndAllEvidence(t *testing.T) {
	db, repo, input, parent := registryFixture(t, nil)
	plan := registryPreview(t, repo, input)
	require.Equal(t, 21, plan.RecordCount)
	twitter := registryKey(t, plan, "twitter:id:9007199254740993")
	require.Equal(t, "mapped", twitter.Action)
	require.Equal(t, parent.AccountBindings[twitter.AccountKey], twitter.AccountUUID)
	require.Equal(t, twitter.AccountUUID, registryKey(t, plan, "twitter:handle:SURVIVOR").AccountUUID)
	require.Equal(t, twitter.AccountUUID, registryKey(t, plan, "twitter:handle:survivor").AccountUUID)
	mirror := registryKey(t, plan, "mirror:coomer:onlyfans:user:42")
	native := registryKey(t, plan, "onlyfans:id:42")
	require.NotEqual(t, mirror.AccountUUID, native.AccountUUID)
	require.Equal(t, "review", registryKey(t, plan, "instagram (from aggregator reddit):handle:someone").Action)
	first := registryApply(t, repo, input, plan)
	require.Equal(t, first, registryApply(t, repo, input, plan))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, a := range plan.Collections {
			if a.Action == "review" {
				continue
			}
			c, err := repo.SourceCollection.Find(ctx, a.CollectionUUID)
			require.NoError(t, err)
			require.Equal(t, "disabled", c.State)
			require.Equal(t, "legacy_catalog", c.Kind)
			require.Nil(t, c.RootUUID)
			require.Empty(t, c.TargetURL)
			if a.CatalogID == "aggregator" {
				require.Nil(t, c.AccountUUID)
			}
		}
		refs, err := repo.SourceAccount.Identifiers(ctx, mirror.AccountUUID, "", 100)
		require.NoError(t, err)
		kinds := map[string]string{}
		for _, ref := range refs {
			kinds[ref.Reference.Kind] = ref.Reference.Value
		}
		require.Equal(t, map[string]string{"user": "42", "public_id": "public-name"}, kinds)
		owner, err := repo.SourceAccount.Ownership(ctx, twitter.AccountUUID)
		require.NoError(t, err)
		require.Equal(t, catalogSurvivor, *owner.PerformerUUID)
		owner, err = repo.SourceAccount.Ownership(ctx, parent.AccountBindings["reddit:handle:deliberately-unlinked"])
		require.NoError(t, err)
		require.Equal(t, models.AccountOwnershipUnlinked, owner.State)
		var rows []models.CatalogRegistryImportRecord
		for after := int64(0); ; {
			page, err := repo.CatalogRegistryImport.Records(ctx, input.UUID, after, 3)
			require.NoError(t, err)
			if len(page) == 0 {
				break
			}
			rows = append(rows, page...)
			after = page[len(page)-1].Sequence
		}
		require.Len(t, rows, 21)
		for _, r := range rows {
			require.NotEmpty(t, r.Evidence)
			if strings.Contains(r.SourceKey, "../outside") {
				require.Equal(t, "review", r.Outcome)
			}
		}
		performer, err := repo.Performer.Find(ctx, 71)
		require.NoError(t, err)
		require.Equal(t, "Shared name", performer.Name)
		return nil
	}))
	// Receipt replay after later native ownership edits never restores old state.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		account, err := repo.SourceAccount.Find(ctx, twitter.AccountUUID)
		if err != nil {
			return err
		}
		_, err = repo.SourceAccount.DecideOwnership(ctx, models.AccountOwnershipInput{AccountUUID: account.UUID, ExpectedAccountRevision: account.Revision, State: models.AccountOwnershipUnlinked, Origin: "review"})
		return err
	}))
	require.Equal(t, first, registryApply(t, repo, input, plan))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		receipt, err := repo.CatalogRegistryImport.Find(ctx, input.UUID)
		require.NoError(t, err)
		require.Equal(t, first, receipt)
		owner, err := repo.SourceAccount.Ownership(ctx, twitter.AccountUUID)
		require.NoError(t, err)
		require.Equal(t, models.AccountOwnershipUnlinked, owner.State)
		return nil
	}))
	input.UUID = uuid.NewString()
	require.ErrorIs(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.CatalogRegistryImport.Preview(ctx, input, catalogImportNow)
		return err
	}), models.ErrCatalogRegistryImportConflict)
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(db, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	raw := openRawDB(t, output)
	defer raw.Close()
	for _, table := range []string{"catalog_registry_imports", "catalog_registry_import_records", "catalog_account_mappings", "catalog_collection_mappings"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
}

func TestCatalogRegistryImportStalePreviewAndSwallowedFailure(t *testing.T) {
	db, repo, input, parent := registryFixture(t, nil)
	plan := registryPreview(t, repo, input)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		account, err := repo.SourceAccount.Find(ctx, parent.AccountBindings["twitter:id:9007199254740993"])
		if err != nil {
			return err
		}
		_, err = repo.SourceAccount.DecideOwnership(ctx, models.AccountOwnershipInput{AccountUUID: account.UUID, ExpectedAccountRevision: account.Revision, State: models.AccountOwnershipUnlinked, Origin: "review"})
		return err
	}))
	require.ErrorIs(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.CatalogRegistryImport.Apply(ctx, input, plan.PlanSHA256, catalogImportNow)
		return err
	}), models.ErrCatalogRegistryImportConflict)
	plan = registryPreview(t, repo, input)
	for _, a := range plan.Ownership {
		if a.AccountKey == "twitter:id:9007199254740993" {
			require.Equal(t, "review", a.Action)
		}
	}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `CREATE TRIGGER fail_registry_import BEFORE INSERT ON catalog_registry_import_records BEGIN SELECT RAISE(ABORT,'late registry failure'); END`, nil)
		return err
	}))
	require.ErrorContains(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.CatalogRegistryImport.Apply(ctx, input, plan.PlanSHA256, catalogImportNow)
		require.ErrorContains(t, err, "late registry failure")
		return nil
	}), "catalog registry import did not finish atomically")
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		receipt, err := repo.CatalogRegistryImport.Find(ctx, input.UUID)
		require.NoError(t, err)
		require.Nil(t, receipt)
		account, err := repo.SourceAccount.Find(ctx, registryKey(t, plan, "onlyfans:id:42").AccountUUID)
		require.NoError(t, err)
		require.Nil(t, account)
		for _, a := range plan.Collections {
			collection, err := repo.SourceCollection.Find(ctx, a.CollectionUUID)
			require.NoError(t, err)
			require.Nil(t, collection)
		}
		return nil
	}))
}

func TestCatalogRegistryImportKeepsReusedHandlesAmbiguous(t *testing.T) {
	_, repo, input, _ := registryFixture(t, func(doc *registryTestDocument) {
		for _, id := range []string{"100", "200"} {
			doc.Tables["account_identifiers"] = append(doc.Tables["account_identifiers"], map[string]any{"catalog_id": "aggregator", "account_key": "twitter:id:" + id, "platform": "twitter", "namespace": "native:twitter", "kind": "id", "value": id, "handle": "Reused", "alias_key": "twitter:handle:reused", "basis": "captured-author", "origin": "gallery-dl", "first_observed": "2025-01-01T00:00:00Z", "last_observed": "2026-09-29T00:00:00Z"})
		}
		doc.Tables["routes"] = append(doc.Tables["routes"], map[string]any{"route": "owner:creator:twitter:handle:reused", "catalog_id": "aggregator"})
	})
	plan := registryPreview(t, repo, input)
	key := registryKey(t, plan, "twitter:handle:reused")
	require.Equal(t, "review", key.Action)
	require.Len(t, key.Candidates, 2)
	first, second := registryKey(t, plan, "twitter:id:100"), registryKey(t, plan, "twitter:id:200")
	require.NotEqual(t, first.AccountUUID, second.AccountUUID)
	registryApply(t, repo, input, plan)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		accounts, err := repo.SourceAccount.Lookup(ctx, models.AccountReference{Namespace: "native:twitter", Kind: "handle", Value: "REUSED"}, "", 100)
		require.NoError(t, err)
		require.Len(t, accounts, 2)
		return nil
	}))
}

func TestCatalogRegistryImportRetainsActualNativeCandidates(t *testing.T) {
	_, repo, input, parent := registryFixture(t, nil)
	var other string
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		account, err := repo.SourceAccount.Create(ctx, "native:twitter", "Competing account")
		if err != nil {
			return err
		}
		other = account.UUID
		_, err = repo.SourceAccount.ObserveIdentifier(ctx, other, models.AccountReference{Namespace: "native:twitter", Kind: "id", Value: "9007199254740993"}, models.AccountIdentifierEvidence{Key: "existing", Basis: "captured-author", Origin: "migration", Details: json.RawMessage(`{}`), FirstObserved: catalogImportNow, LastObserved: catalogImportNow})
		return err
	}))
	plan := registryPreview(t, repo, input)
	key := registryKey(t, plan, "twitter:id:9007199254740993")
	require.Equal(t, "review", key.Action)
	require.Equal(t, "captured_id_conflicts_with_explicit_binding", key.Reason)
	require.Empty(t, key.AccountUUID)
	require.ElementsMatch(t, []string{other, parent.AccountBindings[key.AccountKey]}, key.Candidates)
	require.ElementsMatch(t, key.Candidates, registryKey(t, plan, "twitter:handle:survivor").Candidates)
	registryApply(t, repo, input, plan)
}

func TestCatalogRegistryImportMultipleReferenceKindsRequireNativeEquivalence(t *testing.T) {
	for _, shared := range []bool{false, true} {
		name := "separate accounts"
		if shared {
			name = "same account"
		}
		t.Run(name, func(t *testing.T) {
			refs := []models.AccountReference{{Namespace: "native:tiktok", Kind: "id", Value: "100"}, {Namespace: "native:tiktok", Kind: "secUid", Value: "MS4wAQ-test"}}
			_, repo, input, _ := registryFixture(t, func(doc *registryTestDocument) {
				for _, ref := range refs {
					doc.Tables["account_identifiers"] = append(doc.Tables["account_identifiers"], map[string]any{"catalog_id": "aggregator", "account_key": "tiktok:id:100", "platform": "tiktok", "namespace": ref.Namespace, "kind": ref.Kind, "value": ref.Value, "handle": "survivor", "alias_key": "tiktok:handle:survivor", "basis": "captured-author", "origin": "gallery-dl", "first_observed": "2025-01-01T00:00:00Z", "last_observed": "2026-09-29T00:00:00Z"})
				}
				doc.Tables["routes"] = append(doc.Tables["routes"], map[string]any{"route": "owner:creator:tiktok:handle:survivor", "catalog_id": "aggregator"})
			})
			var accountID string
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				for i, ref := range refs {
					if i == 0 || !shared {
						account, err := repo.SourceAccount.Create(ctx, ref.Namespace, "Selected label")
						if err != nil {
							return err
						}
						accountID = account.UUID
					}
					_, err := repo.SourceAccount.ObserveIdentifier(ctx, accountID, ref, models.AccountIdentifierEvidence{Key: "existing", Basis: "captured-author", Origin: "migration", Details: json.RawMessage(`{}`), FirstObserved: catalogImportNow, LastObserved: catalogImportNow})
					if err != nil {
						return err
					}
				}
				return nil
			}))
			plan := registryPreview(t, repo, input)
			for _, name := range []string{"tiktok:id:100", "tiktok:handle:survivor"} {
				key := registryKey(t, plan, name)
				if shared {
					require.Equal(t, "mapped", key.Action)
					require.Equal(t, accountID, key.AccountUUID)
					require.Equal(t, []string{accountID}, key.Candidates)
				} else {
					require.Equal(t, "review", key.Action)
					require.Len(t, key.Candidates, 2)
				}
			}
			registryApply(t, repo, input, plan)
		})
	}
}

func TestCatalogRegistryRejectsDifferentSnapshotAndBrokenGraphs(t *testing.T) {
	_, repo, input, _ := registryFixture(t, nil)
	bad := input
	bad.IdentityImportUUID = uuid.NewString()
	require.ErrorIs(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.CatalogRegistryImport.Preview(ctx, bad, catalogImportNow)
		return err
	}), models.ErrCatalogRegistryImportInvalid)
	for _, change := range []func(*registryTestDocument){
		func(d *registryTestDocument) { d.Tables["catalogs"][0]["redirect_to"] = "old" },
		func(d *registryTestDocument) { d.Tables["catalogs"][0]["unknown"] = true },
		func(d *registryTestDocument) {
			d.Tables["account_identifier_checkpoints"][0]["captured_at"] = "invalid"
		},
		func(d *registryTestDocument) {
			d.Tables["account_identifiers"] = append(d.Tables["account_identifiers"], d.Tables["account_identifiers"][0])
		},
	} {
		var d registryTestDocument
		require.NoError(t, json.Unmarshal(input.Document, &d))
		change(&d)
		bad := input
		var err error
		bad.Document, err = json.Marshal(d)
		require.NoError(t, err)
		_, err = scrape.PrepareCatalogRegistry(bad)
		require.ErrorIs(t, err, models.ErrCatalogRegistryImportInvalid)
	}
	var d registryTestDocument
	require.NoError(t, json.Unmarshal(input.Document, &d))
	d.External["performer_identities"]++
	bad = input
	var err error
	bad.Document, err = json.Marshal(d)
	require.NoError(t, err)
	require.ErrorIs(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.CatalogRegistryImport.Preview(ctx, bad, catalogImportNow)
		return err
	}), models.ErrCatalogRegistryImportInvalid)
}
