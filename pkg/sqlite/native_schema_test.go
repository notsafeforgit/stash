package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

// Construct actual historical schemas; do not relabel a current database and
// accidentally pre-apply the migration under test.
func buildLegacyDatabase(t *testing.T, path string, version uint, bridge bool) {
	t.Helper()
	initial, err := os.ReadFile("migrations/1_initial.up.sql")
	require.NoError(t, err)
	raw := openRawDB(t, path)
	_, err = raw.Exec(string(initial))
	require.NoError(t, err)
	_, err = raw.Exec("CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, dirty BOOLEAN NOT NULL); INSERT INTO schema_migrations VALUES (1, false)")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	defer m.Close()
	for current := m.CurrentSchemaVersion(); current < version; current = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(context.Background(), m.GetNextMigrationVersion(current)))
	}
	if bridge {
		require.NoError(t, m.RunAllForkMigrations(context.Background()))
		require.NoError(t, m.RunForkReconcilers(context.Background()))
	}
}

func TestNewNativeDatabaseHasSingleMigrationLineage(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "native.sqlite")
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(path))
	require.Equal(t, sqlite.GetRequiredSchemaVersion(), db.Version())
	require.Zero(t, db.ForkSchemaVersion())
	require.Zero(t, db.RequiredForkSchemaVersion())
	require.NoError(t, db.Close())

	raw := openRawDB(t, path)
	var lineage string
	require.NoError(t, raw.QueryRow("SELECT lineage FROM native_schema WHERE singleton=1").Scan(&lineage))
	require.Equal(t, sqlite.NativeSchemaLineage, lineage)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name GLOB 'fork_*'"))
	require.Equal(t, uint(5), queryUint(t, raw, "SELECT count(*) FROM legacy_schema_history WHERE track = 'legacy-fork'"))
	require.NoError(t, raw.Close())

	for range 2 {
		require.NoError(t, db.Open(path))
		require.NoError(t, db.Close())
	}
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	defer m.Close()
	require.NoError(t, m.RunForkReconcilers(context.Background()))
	require.NoError(t, m.RunAllForkMigrations(context.Background()))
	require.ErrorContains(t, m.RunForkMigration(context.Background(), 5), "cannot run against a native database")
	raw = openRawDB(t, path)
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name GLOB 'fork_*'"))
}

func TestNativeLineageRejectsUnsafeInputsBeforeWriting(t *testing.T) {
	for _, tc := range []struct {
		name, change, message string
	}{
		{"missing lineage", "DROP TABLE native_schema", "no native lineage marker"},
		{"foreign lineage", "UPDATE native_schema SET lineage = 'another-application'", "unsupported database lineage"},
		{"newer version", fmt.Sprintf("UPDATE schema_migrations SET version = %d", sqlite.GetRequiredSchemaVersion()+1), "incompatible with required"},
		{"dirty migration", "UPDATE schema_migrations SET dirty = 1", "is incomplete"},
		{"missing authoritative data", "DROP TABLE video_file_metadata", "missing video_file_metadata"},
		{"missing migration evidence", "DROP TABLE saved_filter_import_conflicts", "missing saved_filter_import_conflicts"},
		{"missing canonical filters", "ALTER TABLE saved_filters DROP COLUMN filter_ast", "missing saved_filters.filter_ast"},
		{"missing name model", "DROP TABLE performer_names", "missing performer_names"},
		{"missing native defaults", "DROP TABLE default_filters", "missing default_filters"},
		{"missing catalog identity history", "DROP TABLE catalog_identity_import_records", "missing catalog_identity_import_records"},
		{"missing catalog registry mappings", "DROP TABLE catalog_account_mappings", "missing catalog_account_mappings"},
		{"missing catalog relationships", "DROP TABLE catalog_relation_records", "missing catalog_relation_records"},
		{"missing catalog relationship checkpoint guard", "DROP TRIGGER catalog_relations_import_guard", "missing catalog_relations_import_guard"},
		{"missing catalog relationship evidence guard", "DROP TRIGGER catalog_relation_record_immutable", "missing catalog_relation_record_immutable"},
		{"missing catalog publisher receipts", "DROP TABLE catalog_publisher_records", "missing catalog_publisher_records"},
		{"missing catalog attachment receipts", "DROP TABLE catalog_attachment_records", "missing catalog_attachment_records"},
		{"missing catalog attachment checkpoint guard", "DROP TRIGGER catalog_attachment_import_guard", "missing catalog_attachment_import_guard"},
		{"missing catalog publisher checkpoint guard", "DROP TRIGGER catalog_publisher_import_guard", "missing catalog_publisher_import_guard"},
		{"missing catalog publisher candidate index", "DROP INDEX catalog_evidence_publisher_candidates", "missing catalog_evidence_publisher_candidates"},
		{"missing config checkpoint", "DROP TABLE configuration_migrations", "missing configuration_migrations"},
		{"missing default alternatives", "DROP TABLE default_filter_import_conflicts", "missing default_filter_import_conflicts"},
		{"missing archive identities", "DROP TABLE archive_entities", "missing archive_entities"},
		{"missing identity lifecycle", "DROP TRIGGER archive_scene_deleted", "missing archive_scene_deleted"},
		{"missing publisher decisions", "DROP TABLE capture_publisher_decisions", "missing capture_publisher_decisions"},
		{"missing ingest producers", "DROP TABLE ingest_producers", "missing ingest_producers"},
		{"missing ingest scopes", "DROP TABLE ingest_credential_scopes", "missing ingest_credential_scopes"},
		{"missing backfill history", "DROP TABLE source_backfill_decisions", "missing source_backfill_decisions"},
		{"missing backfill lookup index", "DROP INDEX source_backfill_subject", "missing source_backfill_subject"},
		{"missing ingest receipt integrity", "DROP TRIGGER ingest_receipt_scope", "missing ingest_receipt_scope"},
		{"missing publisher claims", "DROP TABLE capture_publisher_claims", "missing capture_publisher_claims"},
		{"missing publisher publication", "DROP TRIGGER capture_publisher_decision_publish", "missing capture_publisher_decision_publish"},
		{"missing publisher lookup index", "DROP INDEX source_account_identifiers_canonical_kind", "missing source_account_identifiers_canonical_kind"},
		{"missing media roots", "DROP TABLE media_roots", "missing media_roots"},
		{"missing collection revisions", "DROP TABLE source_collection_revisions", "missing source_collection_revisions"},
		{"missing collection publication guard", "DROP TRIGGER source_collection_revision_publish", "missing source_collection_revision_publish"},
		{"missing collection provenance", "DROP TABLE source_collection_media_intake", "missing source_collection_media_intake"},
		{"missing source accounts", "DROP TABLE source_accounts", "missing source_accounts"},
		{"missing post URL evidence", "DROP TABLE source_post_url_evidence", "missing source_post_url_evidence"},
		{"missing post evidence time", "ALTER TABLE source_post_url_evidence RENAME COLUMN observed_at TO undated", "invalid source_post_url_evidence.observed_at"},
		{"missing post evidence retirement guard", "DROP TRIGGER source_post_account_claim_active_post", "missing source_post_account_claim_active_post"},
		{"missing account evidence", "DROP TABLE source_account_identifier_evidence", "missing source_account_identifier_evidence"},
		{"missing ownership choices", "DROP TABLE account_performer_decisions", "missing account_performer_decisions"},
		{"missing ownership guard", "DROP TRIGGER account_performer_decision_kind_insert", "missing account_performer_decision_kind_insert"},
		{"missing source posts", "DROP TABLE source_posts", "missing source_posts"},
		{"missing source payloads", "DROP TABLE source_payloads", "missing source_payloads"},
		{"missing source captures", "DROP TABLE source_captures", "missing source_captures"},
		{"missing profile references", "DROP TABLE source_capture_profiles", "missing source_capture_profiles"},
		{"missing evidence guard", "DROP TRIGGER source_post_revision_immutable", "missing source_post_revision_immutable"},
		{"missing gallery identity guard", "DROP TRIGGER archive_gallery_created", "missing archive_gallery_created"},
		{"missing source attachments", "DROP TABLE source_attachments", "missing source_attachments"},
		{"missing source manifests", "DROP TABLE source_attachment_manifests", "missing source_attachment_manifests"},
		{"missing attachment choices", "DROP TABLE attachment_media_decisions", "missing attachment_media_decisions"},
		{"missing attachment media guard", "DROP TRIGGER source_media_evidence_kind_update", "missing source_media_evidence_kind_update"},
		{"missing post media index", "DROP INDEX source_media_evidence_post", "missing source_media_evidence_post"},
		{"missing post media revision guard", "DROP TRIGGER source_media_evidence_revision", "missing source_media_evidence_revision"},
		{"missing post media retirement guard", "DROP TRIGGER source_media_evidence_active_post", "missing source_media_evidence_active_post"},
		{"missing post media scope", "ALTER TABLE source_media_evidence RENAME COLUMN post_uuid TO missing_post", "invalid source_media_evidence scope columns"},
		{"missing source file observations", "DROP TABLE source_file_observations", "missing source_file_observations"},
		{"missing source file generation guard", "DROP TRIGGER source_file_match_generation", "missing source_file_match_generation"},
		{"missing source file post guard", "DROP TRIGGER source_post_file_evidence_active_post", "missing source_post_file_evidence_active_post"},
		{"missing source file location index", "DROP INDEX source_file_observation_location", "missing source_file_observation_location"},
		{"missing attachment capture scope", "DROP INDEX source_captures_scope", "missing source_captures_scope"},
		{"missing attachment selection", "DROP TABLE post_attachment_selections", "missing post_attachment_selections"},
		{"missing attachment selection guard", "DROP TRIGGER post_attachment_selection_forward", "missing post_attachment_selection_forward"},
		{"missing source gallery links", "DROP TABLE post_gallery_links", "missing post_gallery_links"},
		{"missing gallery membership history", "DROP TABLE gallery_membership_events", "missing gallery_membership_events"},
		{"missing source gallery intent", "DROP TRIGGER source_gallery_galleries_images_delete", "missing source_gallery_galleries_images_delete"},
		{"missing gallery membership revision", "DROP TRIGGER archive_gallery_galleries_images_delete", "missing archive_gallery_galleries_images_delete"},
		{"missing tag identity index", "DROP INDEX archive_entities_tag", "missing archive_entities_tag"},
		{"missing studio identity lifecycle", "DROP TRIGGER archive_studio_deleted", "missing archive_studio_deleted"},
		{"missing group hierarchy revision", "DROP TRIGGER archive_group_groups_relations_update", "missing archive_group_groups_relations_update"},
		{"missing metadata baselines", "DROP TABLE metadata_field_baselines", "missing metadata_field_baselines"},
		{"missing metadata history", "DROP TABLE metadata_field_decisions", "missing metadata_field_decisions"},
		{"missing file receipt index", "DROP INDEX ingest_receipts_job", "missing file ingestion receipt job association"},
		{"missing metadata library intent", "DROP TRIGGER metadata_scene_title_library", "missing metadata_scene_title_library"},
		{"missing canonical name", "INSERT INTO performers(id, created_at, updated_at) VALUES (1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)", "performer has no canonical name"},
		{"foreign primary version", "DROP TABLE native_schema; UPDATE schema_migrations SET version = 87", "unsupported legacy Stash schema"},
		{"newer legacy fork", "DROP TABLE native_schema; UPDATE schema_migrations SET version = 86; CREATE TABLE fork_schema_migrations(version INTEGER); INSERT INTO fork_schema_migrations VALUES (10)", "unsupported legacy fork schema 10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config.InitializeEmpty()
			path := filepath.Join(t.TempDir(), "native.sqlite")
			writeEmptyNativeFixture(t, path)
			db := sqlite.NewDatabase()
			raw := openRawDB(t, path)
			_, err := raw.Exec(tc.change)
			require.NoError(t, err)
			require.NoError(t, raw.Close())
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			require.ErrorContains(t, db.Open(path), tc.message)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after, "identity rejection must not change database bytes")
		})
	}
}

func TestNativePromotionSQLFailureRollsBackSchemaChanges(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	raw := openRawDB(t, path)
	_, err := raw.Exec(`
INSERT INTO saved_filters(name, mode, find_filter, object_filter, ui_options)
VALUES ('kept filter', 'SCENES', '', '{}', '');
CREATE TRIGGER fail_native_promotion BEFORE UPDATE ON saved_filters
WHEN EXISTS (SELECT 1 FROM sqlite_schema WHERE name = 'native_schema')
BEGIN SELECT RAISE(ABORT, 'injected promotion failure'); END;
`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	require.ErrorContains(t, db.RunAllMigrations(), "injected promotion failure")
	raw = openRawDB(t, path)
	defer raw.Close()
	require.False(t, rawTableExists(t, raw, "native_schema"))
	require.False(t, rawTableExists(t, raw, "video_file_metadata"))
	require.True(t, rawTableExists(t, raw, "fork_video_file_metadata"))
	require.True(t, rawTableExists(t, raw, "fork_saved_filter_state"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM saved_filters WHERE name = 'kept filter'"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM schema_migrations WHERE dirty = 1"))
	require.ErrorContains(t, db.Open(path), "is incomplete")
}

func TestNativePromotionRejectsUnknownObjectsAndCollisions(t *testing.T) {
	for _, tc := range []struct{ name, change, message string }{
		{"unknown fork data", "CREATE TABLE fork_unmapped(value TEXT); INSERT INTO fork_unmapped VALUES ('keep me')", "unrecognized legacy fork object fork_unmapped"},
		{"destination collision", "CREATE TABLE shares(value TEXT); INSERT INTO shares VALUES ('keep me')", "destination shares already exists"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config.InitializeEmpty()
			path := filepath.Join(t.TempDir(), "legacy.sqlite")
			buildLegacyDatabase(t, path, 86, true)
			raw := openRawDB(t, path)
			_, err := raw.Exec(tc.change)
			require.NoError(t, err)
			require.NoError(t, raw.Close())
			db := sqlite.NewDatabase()
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(db.Open(path), &needed))
			require.ErrorContains(t, db.RunAllMigrations(), tc.message)
			raw = openRawDB(t, path)
			defer raw.Close()
			require.Equal(t, uint(86), queryUint(t, raw, "SELECT version FROM schema_migrations"))
			require.False(t, rawTableExists(t, raw, "native_schema"))
			require.True(t, rawTableExists(t, raw, "fork_shares"))
		})
	}
}

func TestNativePromotionPreservesSidecarRecordsAndConstraints(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	raw := openRawDB(t, path)
	_, err := raw.Exec(`
INSERT INTO performers(id, name, created_at, updated_at) VALUES (71, 'Canonical', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO performer_aliases(performer_id, alias) VALUES (71, 'Alias');
INSERT INTO fork_performer_autotag_ignored_names VALUES (71, 'Alias');
INSERT INTO folders(id, path, basename, mod_time, created_at, updated_at) VALUES (1, '/media', 'media', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO files(id, parent_folder_id, basename, size, mod_time, created_at, updated_at) VALUES
 (21, 1, 'movie.mp4', 1000, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
 (22, 1, 'image.jpg', 2000, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO video_files(file_id, duration, video_codec, format, audio_codec, width, height, frame_rate, bit_rate)
VALUES (21, 10.0, 'av1', 'mp4', 'aac', 1920, 1080, 30.0, 10000);
INSERT INTO image_files(file_id, format, width, height) VALUES (22, 'jpeg', 3024, 4032);
INSERT INTO fork_video_file_metadata(file_id, source_size, source_mod_time, frame_count, bit_depth, color_transfer)
SELECT id, size, mod_time, 300, 10, 'smpte2084' FROM files WHERE id = 21;
INSERT INTO fork_image_file_metadata(file_id, source_size, source_mod_time, bit_depth)
SELECT id, size, mod_time, 16 FROM files WHERE id = 22;
INSERT INTO blobs(checksum, blob) VALUES ('cover', X'010203');
INSERT INTO scenes(id, title, cover_blob, created_at, updated_at) VALUES (31, 'Kept title', 'cover', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO fork_scene_cover_sources VALUES (31, 'cover', 9999, 12.5, '{"version":1,"size":1000}');
INSERT INTO fork_shares(id, token_hash, label, created_by, created_at, expires_at, snapshot)
VALUES ('kept-share', X'001122', 'Kept share', 'owner', 100, 9999999999, '{"scenes":[31]}');
INSERT INTO fork_share_sessions(token_hash, share_id, version, expires_at) VALUES (X'003344', 'kept-share', 1, 9999999999);
INSERT INTO fork_file_deletions VALUES ('pending-filesystem-operation');
`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	journalPath := db.FileDeletionJournalPath()
	require.NoError(t, db.RunAllMigrations())
	require.Equal(t, journalPath, db.FileDeletionJournalPath())

	// Inspect before runtime recovery consumes an orphan deletion marker.
	raw = openRawDB(t, path)
	defer raw.Close()
	for _, query := range []string{
		"SELECT count(*) FROM performer_names WHERE performer_id = 71 AND name = 'Alias' AND ignore_auto_tag = 1 AND is_primary = 0",
		"SELECT count(*) FROM video_file_metadata WHERE file_id = 21 AND frame_count = 300 AND bit_depth = 10 AND color_transfer = 'smpte2084'",
		"SELECT count(*) FROM image_file_metadata WHERE file_id = 22 AND bit_depth = 16",
		"SELECT count(*) FROM scene_cover_sources WHERE scene_id = 31 AND source_file_id = 9999 AND at = 12.5",
		"SELECT count(*) FROM shares WHERE id = 'kept-share' AND token_hash = X'001122' AND snapshot = '{\"scenes\":[31]}'",
		"SELECT count(*) FROM share_sessions WHERE share_id = 'kept-share' AND token_hash = X'003344'",
		"SELECT count(*) FROM file_deletions WHERE id = 'pending-filesystem-operation'",
	} {
		require.Equal(t, uint(1), queryUint(t, raw, query), query)
	}
	var table string
	var rowID sql.NullInt64
	var parent string
	var fkID int
	err = raw.QueryRow("PRAGMA foreign_key_check").Scan(&table, &rowID, &parent, &fkID)
	require.ErrorIs(t, err, sql.ErrNoRows)
	_, err = raw.Exec("DELETE FROM shares WHERE id = 'kept-share'")
	require.NoError(t, err)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM share_sessions"))
	_, err = raw.Exec("DELETE FROM scenes WHERE id = 31")
	require.NoError(t, err)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM scene_cover_sources"))
}
