package sqlite_test

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Historical schema rewinds for retired import migrations. Older migration
// tests remove later schema objects before exercising their own upgrade.

func removeMembershipCleanupSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeSavedShareLinkSchema(t, raw)
	_, err := raw.Exec(`DROP TRIGGER IF EXISTS catalog_membership_compaction_guard;
DROP TABLE IF EXISTS source_post_import_labels; DROP TABLE IF EXISTS source_post_studios;
DROP INDEX IF EXISTS catalog_membership_records_collection; DROP INDEX IF EXISTS catalog_membership_records_membership;
DROP INDEX IF EXISTS translation_targets_collection; DROP INDEX IF EXISTS automation_translation_records_collection;
DROP INDEX IF EXISTS automation_enrichment_records_collection; DROP INDEX IF EXISTS automation_enrichment_records_alias;
DROP INDEX IF EXISTS automation_discovery_records_collection; DROP INDEX IF EXISTS ingest_receipts_collection;
DELETE FROM native_migration_history WHERE version=1000121`)
	require.NoError(t, err)
	if queryUint(t, raw, "SELECT count(*) FROM pragma_table_info('catalog_snapshots') WHERE name='memberships_compacted'") != 0 {
		_, err := raw.Exec("ALTER TABLE catalog_snapshots DROP COLUMN memberships_compacted")
		require.NoError(t, err)
	}
}

func removeImportedNFOSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeSourceThreadSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE metadata_worker_attempt_policies;
DROP TABLE metadata_worker_policy_upgrades;
DROP TABLE source_run_policy_upgrades;
DROP TABLE source_capture_sightings;
DROP TABLE source_capture_content;
DELETE FROM native_migration_history WHERE version>=1000101;
DROP TRIGGER catalog_nfo_compaction_guard;
DROP VIEW catalog_document_pending_imports;
ALTER TABLE catalog_snapshots DROP COLUMN nfo_compacted;
DELETE FROM native_migration_history WHERE version=1000100`)
	require.NoError(t, err)
}

func removeEnrichmentRebindSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeDiscoveryScopeSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE enrichment_rebinding_targets; DROP TABLE enrichment_rebindings;
DROP INDEX enrichment_targets_pending_scope; DELETE FROM native_migration_history WHERE version=1000083`)
	require.NoError(t, err)
}

func removeDiscoveryScopeSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeAttachmentSelectionReviewSchema(t, raw)
	for _, trigger := range []string{"discovery_listing_job_scope", "discovery_detail_job_scope", "discovery_listing_recovery_scope"} {
		var body string
		require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name=?", trigger).Scan(&body))
		_, err := raw.Exec("DROP TRIGGER " + trigger)
		require.NoError(t, err)
		_, err = raw.Exec(strings.ReplaceAll(body, "discovery_effective_listings", "discovery_listings"))
		require.NoError(t, err)
	}
	_, err := raw.Exec(`DROP VIEW discovery_effective_listings; DROP TABLE discovery_scope_reviews;
 DELETE FROM native_migration_history WHERE version=1000084`)
	require.NoError(t, err)
}

func removeCatalogEnrichmentSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeAutomationEnrichmentSchema(t, raw)
	_, err := raw.Exec(`DROP TRIGGER source_enrichment_receipt_immutable; DROP TRIGGER source_enrichment_receipt_scope;
 DROP TRIGGER catalog_enrichment_import_guard; DROP TRIGGER catalog_enrichment_record_immutable;
 DROP TABLE catalog_enrichment_records; DROP TABLE catalog_enrichment_imports; DROP TABLE source_enrichment_receipts;
 DELETE FROM native_migration_history WHERE version=1000057;`)
	require.NoError(t, err)
}

func removeAutomationEnrichmentSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeEnrichmentActivationSchema(t, raw)
	conn, err := raw.Conn(t.Context())
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.ExecContext(t.Context(), "PRAGMA foreign_keys=OFF")
	require.NoError(t, err)
	defer func() {
		_, err := conn.ExecContext(t.Context(), "PRAGMA foreign_keys=ON")
		require.NoError(t, err)
	}()
	var dependents string
	for _, name := range []string{"enrichment_publication_scope", "enrichment_published_record_scope", "enrichment_job_success"} {
		var definition string
		require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT sql FROM sqlite_schema WHERE name=?", name).Scan(&definition))
		_, err := conn.ExecContext(t.Context(), "DROP TRIGGER "+name)
		require.NoError(t, err)
		dependents += definition + ";\n"
	}
	body, err := os.ReadFile("migrations/1000050_enrichment_work.up.sql")
	require.NoError(t, err)
	s := string(body)
	start := strings.Index(s, "CREATE TABLE enrichment_completions (")
	end := strings.Index(s, "CREATE TABLE enrichment_completion_captures (")
	create := s[start:strings.Index(s, "CREATE TRIGGER enrichment_completion_immutable")]
	create = strings.Replace(create, "CREATE TABLE enrichment_completions (", "CREATE TABLE enrichment_completions_prior (", 1)
	triggers := s[strings.Index(s, "CREATE TRIGGER enrichment_completion_immutable"):end]
	triggers += s[strings.Index(s, "CREATE TRIGGER enrichment_completion_capture_scope"):strings.Index(s, "CREATE TABLE enrichment_target_history")]
	_, err = conn.ExecContext(t.Context(), `DROP TRIGGER automation_enrichment_import_guard; DROP TRIGGER automation_enrichment_record_immutable;
 DROP TABLE automation_enrichment_records; DROP TABLE automation_enrichment_imports; DROP INDEX automation_enrichment_input;
 DROP TRIGGER enrichment_completion_immutable; DROP TRIGGER enrichment_completion_scope;
 DROP TRIGGER enrichment_completion_capture_scope; DROP TRIGGER enrichment_target_scope;`+create+`
 INSERT INTO enrichment_completions_prior(uuid,target_uuid,expected_revision,request_digest,capture_count,created_at)
 SELECT uuid,target_uuid,expected_revision,request_digest,capture_count,created_at FROM enrichment_completions;
 DROP TABLE enrichment_completions; ALTER TABLE enrichment_completions_prior RENAME TO enrichment_completions;`+triggers+dependents+`
 DELETE FROM native_migration_history WHERE version=1000058;`)
	require.NoError(t, err)
}

func removeEnrichmentActivationSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeAutomationCheckpointSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE enrichment_activation_targets; DROP TABLE enrichment_activations;
 DROP INDEX automation_enrichment_held; DROP INDEX automation_enrichment_held_targets;
 DELETE FROM native_migration_history WHERE version=1000059`)
	require.NoError(t, err)
}

func removeAutomationCheckpointSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeCaptureRecordingTimeSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE automation_checkpoint_records; DROP TABLE automation_checkpoint_imports;
 DROP TABLE automation_checkpoint_bodies; DROP INDEX automation_enrichment_staged_input;
 DELETE FROM native_migration_history WHERE version=1000060`)
	require.NoError(t, err)
}

func removeCheckpointEvidenceSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeCaptureContextSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE checkpoint_evidence_captures; DROP TABLE checkpoint_evidence_acceptances;
 DELETE FROM native_migration_history WHERE version=1000062`)
	require.NoError(t, err)
}

func removeCheckpointHandoffSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeEnrichmentHandoffSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE checkpoint_handoffs; DELETE FROM native_migration_history WHERE version=1000064`)
	require.NoError(t, err)
}

func removeEnrichmentHandoffSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeSourceFileHistorySchema(t, raw)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM archive_jobs WHERE kind='post.enrich' AND json_extract(arguments,'$.version')!=1"), "historical migration fixtures must create historical jobs")
	_, err := raw.Exec(`DROP TABLE enrichment_job_seed_services; DROP TABLE enrichment_job_retained_records; DROP TABLE enrichment_handoff_jobs;
 DROP TRIGGER enrichment_job_target_scope; DROP TRIGGER enrichment_completion_capture_scope;
 DROP TRIGGER enrichment_checkpoint_published_delete;
 CREATE TEMP TABLE old_release_rows AS SELECT rowid AS saved_rowid,* FROM enrichment_checkpoint_releases;
 DROP TABLE enrichment_checkpoint_releases;`)
	require.NoError(t, err)
	_, err = raw.Exec(migrationObject(t, "1000053_enrichment_checkpoint_release.up.sql", "TABLE", "enrichment_checkpoint_releases"))
	require.NoError(t, err)
	_, err = raw.Exec(`INSERT INTO enrichment_checkpoint_releases(rowid,job_uuid,version,proof_sha256,checkpoint_bytes,unresolved,created_at)
 SELECT saved_rowid,job_uuid,version,proof_sha256,checkpoint_bytes,unresolved,created_at FROM old_release_rows;
 DROP TABLE old_release_rows; DELETE FROM native_migration_history WHERE version=1000065;`)
	require.NoError(t, err)
	for _, trigger := range []string{"enrichment_checkpoint_release_immutable", "enrichment_checkpoint_release_scope", "enrichment_checkpoint_published_delete"} {
		_, err := raw.Exec(migrationObject(t, "1000053_enrichment_checkpoint_release.up.sql", "TRIGGER", trigger))
		require.NoError(t, err)
	}
	_, err = raw.Exec(migrationObject(t, "1000051_enrichment_jobs.up.sql", "TRIGGER", "enrichment_job_target_scope") +
		migrationObject(t, "1000058_automation_enrichment.up.sql", "TRIGGER", "enrichment_completion_capture_scope"))
	require.NoError(t, err)
}

func removeSourceFileHistorySchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeMetadataFileReviewSchema(t, raw)
	for _, table := range fileHistoryTables {
		_, err := raw.Exec("DROP TABLE " + table)
		require.NoError(t, err)
	}
	_, err := raw.Exec("DELETE FROM native_migration_history WHERE version=1000066")
	require.NoError(t, err)
}

func removeCatalogCleanupSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removePostMediaDecisionSchema(t, raw)
	_, err := raw.Exec(`DROP TRIGGER source_cleanup_intent_immutable; DROP TRIGGER source_cleanup_intent_scope;
 DROP TRIGGER catalog_cleanup_import_guard; DROP TRIGGER catalog_cleanup_record_immutable;
 DROP TABLE catalog_cleanup_records; DROP TABLE catalog_cleanup_imports; DROP TABLE source_cleanup_intents;
 DELETE FROM native_migration_history WHERE version=1000080;`)
	require.NoError(t, err)
}

func removeAutomationDiscoverySchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeEnrichmentDiscoverySchema(t, raw)
	_, err := raw.Exec(`DROP TABLE automation_discovery_records; DROP TABLE automation_discovery_imports;
 DROP INDEX automation_discovery_input; DELETE FROM native_migration_history WHERE version=1000069`)
	require.NoError(t, err)
}

func removeEnrichmentDiscoverySchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeDiscoveryListingSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE enrichment_discovery_resolutions; DROP INDEX automation_discovery_lookup;
 DELETE FROM native_migration_history WHERE version=1000070`)
	require.NoError(t, err)
}

func removeDiscoveryListingSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeDiscoveryMatchSchema(t, raw)
	_, err := raw.Exec(`DROP TRIGGER discovery_listing_success; DROP TRIGGER discovery_listing_pacing_bind;
 DROP INDEX archive_jobs_discovery_listing; DROP TABLE discovery_pages; DROP TABLE discovery_job_attempts;
 DROP TABLE discovery_listing_jobs; DROP TABLE discovery_listing_legacy; DROP TABLE discovery_listings;
 DELETE FROM native_migration_history WHERE version=1000071`)
	require.NoError(t, err)
}

func removeDiscoveryMatchSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeDiscoveryActivationSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE discovery_match_evidence; DROP TABLE discovery_match_candidates; DROP TABLE discovery_match_pages;
 DROP TABLE discovery_match_targets; DELETE FROM native_migration_history WHERE version=1000072`)
	require.NoError(t, err)
}

func removeDiscoveryActivationSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeDiscoveryPublicationSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE discovery_activation_targets; DROP TABLE discovery_activations;
 DELETE FROM native_migration_history WHERE version=1000073`)
	require.NoError(t, err)
}

func removeDiscoveryPublicationSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeDiscoveryRecoverySchema(t, raw)
	_, err := raw.Exec(`DROP TABLE discovery_published_records; DROP TABLE discovery_match_publications;
 DELETE FROM native_migration_history WHERE version=1000074`)
	require.NoError(t, err)
}

func removeDiscoveryRecoverySchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeDiscoveryDetailSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE discovery_recovery_targets; DROP TABLE discovery_listing_recoveries;
 DROP TRIGGER discovery_recovery_job_scope; DROP TRIGGER discovery_recovery_page_scope;
 DELETE FROM native_migration_history WHERE version=1000075`)
	require.NoError(t, err)
}

func removeDiscoveryDetailSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeDiscoveryDetailPublicationSchema(t, raw)
	_, err := raw.Exec(`DROP TRIGGER discovery_detail_success; DROP INDEX archive_jobs_discovery_detail;
 DROP TABLE discovery_detail_results; DROP TABLE discovery_detail_checkpoints;
 DROP TABLE discovery_detail_checkpoint_records; DROP TABLE discovery_detail_checkpoint_receipts;
 DROP TABLE discovery_detail_checkpoint_usage; DROP TABLE discovery_detail_attempts; DROP TABLE discovery_detail_jobs;
 DELETE FROM native_migration_history WHERE version=1000076`)
	require.NoError(t, err)
}

// Construct the actual predecessor layout, retaining all original receipt
// columns and guards rather than just changing its version marker.
func removeDiscoveryDetailPublicationSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeMetadataPolicyImportSchema(t, raw)
	legacy, err := os.ReadFile("migrations/1000074_discovery_publications.up.sql")
	require.NoError(t, err)
	definition := strings.Split(string(legacy), "CREATE TABLE discovery_published_records")[0]
	guard := strings.Split(strings.Split(string(legacy), "CREATE TRIGGER discovery_published_record_scope")[1], "INSERT INTO native_migration_history")[0]
	_, err = raw.Exec(`PRAGMA foreign_keys=OFF; BEGIN;
 CREATE TABLE original_publication_rows AS SELECT target_uuid,target_revision,listing_uuid,page_ordinal,page_sha256,post_uuid,post_revision,
 namespace,value,policy,basis,witness_ordinal,evidence_uuid,url_evidence_uuid,record_count,capture_count,created_at FROM discovery_match_publications;
 DROP TABLE discovery_match_publications;
 ` + definition + `
 INSERT INTO discovery_match_publications SELECT * FROM original_publication_rows;
 DROP TABLE original_publication_rows;
 DROP TRIGGER discovery_published_record_scope;
 CREATE TRIGGER discovery_published_record_scope` + guard + `
 DROP INDEX discovery_detail_candidate_history;
 DELETE FROM native_migration_history WHERE version=1000077;
 COMMIT; PRAGMA foreign_keys=ON;`)
	require.NoError(t, err)
}
func migrationObject(t *testing.T, file, kind, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("migrations", file))
	require.NoError(t, err)
	end := `\n\);`
	if kind == "TRIGGER" {
		end = `END;`
	}
	ret := regexp.MustCompile(`(?s)CREATE ` + kind + ` ` + regexp.QuoteMeta(name) + `\b.*?` + end).FindString(string(body))
	require.NotEmpty(t, ret)
	return ret
}

var fileHistoryTables = []string{"catalog_file_history_records", "catalog_file_history_imports", "source_file_history_locations", "source_file_history_edits", "source_file_history_states", "source_file_history_deduplications", "source_file_history"}

type schemaObject struct{ Type, Name, SQL string }

var importRetirementTemplate struct {
	sync.Once
	objects []schemaObject
	err     error
}

func schemaObjects(raw *sql.DB) ([]schemaObject, error) {
	rows, err := raw.Query("SELECT type,name,sql FROM sqlite_schema WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' ORDER BY rowid")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ret []schemaObject
	for rows.Next() {
		var o schemaObject
		if err := rows.Scan(&o.Type, &o.Name, &o.SQL); err != nil {
			return nil, err
		}
		ret = append(ret, o)
	}
	return ret, rows.Err()
}

// buildSchemaTemplate applies the native bootstrap and later scripts to an
// empty database. None of these versions has a Go conversion hook that creates
// schema objects.
func buildSchemaTemplate(version int) ([]schemaObject, error) {
	dir, err := os.MkdirTemp("", "stash-schema-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	raw, err := sql.Open("sqlite3ex", "file:"+filepath.ToSlash(filepath.Join(dir, "template.sqlite")))
	if err != nil {
		return nil, err
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	scripts := []string{"bootstrap/1000121.sql"}
	for v := 1000122; v <= version; v++ {
		names, err := filepath.Glob(fmt.Sprintf("migrations/%d_*.up.sql", v))
		if err != nil || len(names) != 1 {
			return nil, fmt.Errorf("migration %d: %v %v", v, names, err)
		}
		scripts = append(scripts, names[0])
	}
	for _, name := range scripts {
		script, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		if _, err := raw.Exec(string(script)); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	return schemaObjects(raw)
}

// Schema 1000142 retires the import history. Rewind it from a pristine 1000141
// schema: retired objects return empty, replaced triggers regain their earlier
// definitions and the replacement identity lookup tables are removed.
func removeImportRetirementSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeBackgroundMetadataSchema(t, raw)
	var present bool
	require.NoError(t, raw.QueryRow("SELECT EXISTS(SELECT 1 FROM native_migration_history WHERE version=1000142)").Scan(&present))
	if !present {
		return
	}
	importRetirementTemplate.Do(func() {
		importRetirementTemplate.objects, importRetirementTemplate.err = buildSchemaTemplate(1000141)
	})
	require.NoError(t, importRetirementTemplate.err)
	prior := map[string]schemaObject{}
	for _, o := range importRetirementTemplate.objects {
		prior[o.Name] = o
	}
	current, err := schemaObjects(raw)
	require.NoError(t, err)
	now := map[string]schemaObject{}
	var dropTables []string
	for _, o := range current {
		now[o.Name] = o
		if before, ok := prior[o.Name]; ok && before.SQL == o.SQL {
			continue
		}
		switch o.Type {
		case "table":
			_, ok := prior[o.Name]
			require.False(t, ok, "table %s changed after 1000141", o.Name)
			dropTables = append([]string{o.Name}, dropTables...)
		case "index", "view", "trigger":
			if _, err := raw.Exec("DROP " + strings.ToUpper(o.Type) + " IF EXISTS " + o.Name); err != nil {
				require.NoError(t, err, o.Name)
			}
		}
	}
	for _, name := range dropTables {
		_, err := raw.Exec("DROP TABLE " + name)
		require.NoError(t, err, name)
	}
	for _, kind := range []string{"table", "index", "view", "trigger"} {
		for _, o := range importRetirementTemplate.objects {
			if o.Type != kind {
				continue
			}
			if existing, ok := now[o.Name]; ok && existing.SQL == o.SQL {
				continue
			}
			_, err := raw.Exec(o.SQL)
			require.NoError(t, err, o.Name)
		}
	}
	_, err = raw.Exec("DELETE FROM native_migration_history WHERE version=1000142")
	require.NoError(t, err)
}
