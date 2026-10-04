package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeAutomationDiscoverySchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeEnrichmentDiscoverySchema(t, raw)
	_, err := raw.Exec(`DROP TABLE automation_discovery_records; DROP TABLE automation_discovery_imports;
 DROP INDEX automation_discovery_input; DELETE FROM native_migration_history WHERE version=1000069`)
	require.NoError(t, err)
}

func legacyDiscoveryAccount(t *testing.T) map[string]any {
	t.Helper()
	account, profile := "reddit:handle:deliberately-unlinked", "https://www.reddit.com/user/deliberately-unlinked/submitted/?sort=new"
	key, err := scrape.LegacyCatalogJSON([]any{"reddit", account, profile}, 65536)
	require.NoError(t, err)
	return map[string]any{"job_key": scrape.CatalogSnapshotSHA(key), "platform": "reddit", "account_key": account, "profile_url": profile,
		"status": "retry", "cursor_json": `{"after":"t3_prior"}`, "staged_json": nil, "pages": 67, "attempts": 5,
		"next_attempt": 0, "last_error": "network unavailable", "created_at": "2026-09-29T00:00:00Z", "updated_at": "2026-10-01T12:00:00Z"}
}

func legacyDiscoveryTarget(t *testing.T, job map[string]any, post, status string) map[string]any {
	t.Helper()
	var key any
	evidence := "{}"
	if status == "pending" || status == "lookup" {
		var candidate any
		if status == "pending" {
			key = job["job_key"]
		} else {
			candidate = "https://www.reddit.com/comments/" + strings.TrimPrefix(post, "reddit:post:")
		}
		encoded, err := json.Marshal(map[string]any{"titles": []string{"Original distinctive title"}, "texts": []string{}, "dates": []string{"2025-01-01"},
			"paths": []string{"Historical album/photo.jpg"}, "urls": []string{}, "platform": "reddit", "account_key": job["account_key"], "candidate_url": candidate, "strict_filename_id": false})
		require.NoError(t, err)
		evidence = string(encoded)
	}
	return map[string]any{"catalog_id": automationCatalog, "post_key": post, "job_key": key, "evidence_json": evidence, "status": status}
}

func discoveryFixture(t *testing.T, alter func(map[string][]map[string]any)) *automationSnapshotFixture {
	t.Helper()
	catalog := enrichmentImportFixture(t, 4, nil)
	advanceCatalogEnrichment(t, catalog, 0)
	account := legacyDiscoveryAccount(t)
	tables := map[string][]map[string]any{
		"discovery_accounts": {account},
		"discovery_targets": {
			legacyDiscoveryTarget(t, account, "reddit:post:receipt000", "pending"),
			legacyDiscoveryTarget(t, account, "reddit:post:receipt001", "lookup"),
			legacyDiscoveryTarget(t, account, "reddit:post:receipt002", "done"),
			legacyDiscoveryTarget(t, account, "reddit:post:receipt003", "no_known_profile"),
		},
		"discovery_candidates": {
			{"catalog_id": automationCatalog, "post_key": "reddit:post:receipt000", "url": "https://www.reddit.com/comments/first1", "basis": "title-needs-verification", "payload_json": `{"records":[],"extractor_version":"legacy"}`},
			{"catalog_id": automationCatalog, "post_key": "reddit:post:receipt000", "url": "https://www.reddit.com/comments/second2", "basis": "exact-title-and-date", "payload_json": `{"records":[]}`},
		},
		"enrichment_jobs":      {legacyEnrichmentJob("reddit:post:receipt001", "pending"), legacyEnrichmentJob("reddit:post:receipt002", "done")},
		"enrichment_cooldowns": {{"scope": "account:" + account["account_key"].(string), "until_time": automationImportNow.Add(24 * time.Hour).Unix(), "reason": "authentication"}},
		"maintenance":          {{"key": "inventory-watermark-ns", "value": "1790936092595039483"}, {"key": "future-key", "value": "Retain this unknown meaning"}},
	}
	if alter != nil {
		alter(tables)
	}
	return enrichmentAutomationFixture(t, catalog, tables)
}

func advanceDiscovery(t *testing.T, f *automationSnapshotFixture, after int64) *models.AutomationDiscoveryImport {
	t.Helper()
	var result *models.AutomationDiscoveryImport
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.AutomationDiscoveryImport.Advance(ctx, f.manifest.UUID, f.sha, after, automationImportNow.Add(2*time.Hour))
		return err
	}))
	return result
}

func TestAutomationDiscoveryImportsAssociationsWithoutMatchingOrActivation(t *testing.T) {
	f := discoveryFixture(t, nil)
	advanceAutomationEnrichment(t, f, 0)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := map[string][][]any{}
	for _, table := range []string{"scenes", "images", "performers", "galleries", "source_posts", "source_captures", "source_post_urls", "source_post_identifiers", "account_performer_decisions", "archive_jobs", "enrichment_targets", "enrichment_completions"} {
		before[table] = albumJobRows(t, raw, table)
	}
	result := advanceDiscovery(t, f, 0)
	require.Equal(t, "review", result.State)
	require.EqualValues(t, 9, result.ProcessedRecords)
	require.EqualValues(t, 7, result.MappedRecords)
	require.EqualValues(t, 2, result.ReviewRecords)
	require.False(t, result.Imported)
	require.Equal(t, result, advanceDiscovery(t, f, result.LastOrdinal))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.AutomationDiscoveryImport.Records(ctx, f.manifest.UUID, 0, 100)
		require.NoError(t, err)
		require.Len(t, rows, 9)
		for _, row := range rows {
			detail, err := f.repo.AutomationDiscoveryImport.Record(ctx, f.manifest.UUID, row.Ordinal)
			require.NoError(t, err)
			require.Equal(t, row, detail.AutomationDiscoveryRecord)
			var values map[string]any
			require.NoError(t, json.Unmarshal(detail.SourceValues, &values))
			switch row.Table {
			case "discovery_accounts":
				require.Equal(t, "listing", row.Phase)
				require.Equal(t, "held", row.Disposition)
				require.Equal(t, automationImportNow.Add(24*time.Hour), *row.NotBefore)
				require.NotNil(t, row.AccountUUID)
				require.EqualValues(t, 67, *row.HistoricalPages)
			case "discovery_candidates":
				require.Equal(t, "candidate", row.Disposition)
				require.NotNil(t, row.TargetOrdinal)
				require.NotNil(t, row.AccountOrdinal)
				require.NotNil(t, row.PostUUID)
				require.Equal(t, scrape.CatalogSnapshotSHA([]byte(values["payload_json"].(string))), row.PayloadSHA256)
			case "discovery_targets":
				require.NotNil(t, row.PostUUID)
				require.NotNil(t, row.CollectionUUID)
				if values["status"] == "done" {
					require.Equal(t, "historical_completion", row.Disposition)
					require.NotNil(t, row.EnrichmentOrdinal)
				}
				if values["status"] == "lookup" {
					require.Equal(t, "lookup", row.Disposition)
					require.NotNil(t, row.EnrichmentOrdinal)
				}
			case "maintenance":
				if values["key"] == "inventory-watermark-ns" {
					require.Equal(t, "1790936092595039483", row.WatermarkNS)
				}
			}
		}
		return nil
	}))
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.Equal(t, result, advanceDiscovery(t, f, result.LastOrdinal))
	path := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(f.db, path)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	anon := openRawDB(t, path)
	defer anon.Close()
	require.Zero(t, queryUint(t, anon, "SELECT count(*) FROM automation_discovery_records"))
	require.Zero(t, queryUint(t, anon, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestAutomationDiscoveryRequiresEvidenceAndPreservesStaging(t *testing.T) {
	f := discoveryFixture(t, func(tables map[string][]map[string]any) {
		account := tables["discovery_accounts"][0]
		account["status"], account["staged_json"] = "matching", `{"target":["c_11111111111111111111111111111111","reddit:post:receipt000","https://www.reddit.com/comments/first1"],"payload":{"records":[]}}`
		tables["enrichment_jobs"][1]["status"] = "pending"
	})
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.AutomationDiscoveryImport.Advance(ctx, f.manifest.UUID, f.sha, 0, automationImportNow.Add(2*time.Hour))
		return err
	})
	require.ErrorIs(t, err, models.ErrAutomationSnapshotConflict)
	advanceAutomationEnrichment(t, f, 0)
	result := advanceDiscovery(t, f, 0)
	require.EqualValues(t, 4, result.ReviewRecords)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.AutomationDiscoveryImport.Records(ctx, f.manifest.UUID, 0, 100)
		require.NoError(t, err)
		reasons := map[string]bool{}
		for _, row := range rows {
			reasons[row.Reason] = true
			if row.StagedKind == "detail" {
				require.NotNil(t, row.TargetOrdinal)
				require.Equal(t, "matching", row.Phase)
			}
		}
		require.True(t, reasons["legacy_discovery_completion_requires_evidence"])
		require.True(t, reasons["legacy_discovery_detail_conversion"])
		return nil
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestAutomationDiscoveryResumeRollbackAndSparsePages(t *testing.T) {
	f := discoveryFixture(t, func(tables map[string][]map[string]any) {
		for i := range 205 {
			tables["maintenance"] = append(tables["maintenance"], map[string]any{"key": fmt.Sprintf("unknown-%03d", i), "value": "original"})
		}
	})
	advanceAutomationEnrichment(t, f, 0)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec(`CREATE TRIGGER fail_discovery BEFORE INSERT ON automation_discovery_records WHEN NEW.ordinal=2 BEGIN SELECT RAISE(ABORT,'fixture failure'); END`)
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.AutomationDiscoveryImport.Advance(ctx, f.manifest.UUID, f.sha, 0, automationImportNow.Add(2*time.Hour))
		require.Error(t, err)
		return nil
	})
	require.Error(t, err, "a swallowed error still cannot commit partial import state")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM automation_discovery_imports"))
	_, err = raw.Exec("DROP TRIGGER fail_discovery")
	require.NoError(t, err)
	first := advanceDiscovery(t, f, 0)
	require.Equal(t, "running", first.State)
	require.EqualValues(t, 200, first.ProcessedRecords)
	require.Greater(t, first.LastOrdinal, first.ProcessedRecords, "ordinals include other record families")
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.AutomationDiscoveryImport.Advance(ctx, f.manifest.UUID, f.sha, 0, automationImportNow.Add(2*time.Hour))
		return err
	})
	require.ErrorIs(t, err, models.ErrAutomationSnapshotConflict)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	result := advanceDiscovery(t, f, first.LastOrdinal)
	require.EqualValues(t, 214, result.ProcessedRecords)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var last, count int64
		for {
			rows, err := f.repo.AutomationDiscoveryImport.Records(ctx, f.manifest.UUID, last, 7)
			require.NoError(t, err)
			if len(rows) == 0 {
				break
			}
			for _, row := range rows {
				require.Greater(t, row.Ordinal, last)
				last = row.Ordinal
				count++
			}
		}
		require.Equal(t, result.ProcessedRecords, count)
		return nil
	}))
}

func TestAutomationDiscoveryMigrationPreservesNativeStateAndRefusesCollision(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			db, _ := archiveTestDatabase(t)
			require.NoError(t, db.Close())
			raw := openRawDB(t, db.DatabasePath())
			defer raw.Close()
			before := map[string][][]any{}
			for _, table := range []string{"scenes", "images", "performers", "archive_entities", "source_accounts", "account_performer_decisions", "source_posts", "archive_jobs", "automation_snapshots", "automation_enrichment_imports"} {
				before[table] = albumJobRows(t, raw, table)
			}
			removeAutomationDiscoverySchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000068,dirty=0")
			require.NoError(t, err)
			if collision {
				_, err = raw.Exec("CREATE TABLE automation_discovery_records(original TEXT); INSERT INTO automation_discovery_records VALUES('retained')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(db.Open(db.DatabasePath()), &needed))
			if collision {
				require.Error(t, db.RunAllMigrations())
				var value string
				require.NoError(t, raw.QueryRow("SELECT original FROM automation_discovery_records").Scan(&value))
				require.Equal(t, "retained", value)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='automation_discovery_input'"))
			} else {
				require.NoError(t, db.RunAllMigrations())
				require.NoError(t, db.ReInitialise())
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
		})
	}
}

func TestAutomationDiscoveryCorruptProjectionRefusedBeforeWrites(t *testing.T) {
	f := discoveryFixture(t, nil)
	advanceAutomationEnrichment(t, f, 0)
	advanceDiscovery(t, f, 0)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	var guard string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='automation_discovery_record_immutable'").Scan(&guard))
	_, err := raw.Exec("DROP TRIGGER automation_discovery_record_immutable; UPDATE automation_discovery_records SET phase='matching' WHERE phase='listing';" + guard)
	require.NoError(t, err)
	require.Error(t, f.db.Open(f.db.DatabasePath()))
}
