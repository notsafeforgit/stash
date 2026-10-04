package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/jmoiron/sqlx"
)

const (
	// NativeSchemaBaseline separates this lineage from upstream migrations and
	// the historical private 998/999 schemas. It also fits GraphQL's Int range.
	NativeSchemaBaseline uint = 1000000
	NativeSchemaLineage       = "org.notsafeforgit.stash.native-archive"
	lastCompatibleSchema uint = 86
)

// validateDatabaseLineage runs before opening a writable migration connection.
// A matching version number alone is never sufficient to identify our schema.
func validateDatabaseLineage(path string) error {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(abs), RawQuery: "mode=ro"}
	conn, err := sqlx.Open(sqlite3Driver, uri.String())
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	var tables []string
	if err := conn.Select(&tables, "SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%'"); err != nil {
		return fmt.Errorf("reading database identity: %w", err)
	}
	if len(tables) == 0 {
		return nil
	}
	present := make(map[string]bool, len(tables))
	for _, name := range tables {
		present[name] = true
	}
	if !present["schema_migrations"] {
		return errors.New("database is not a supported Stash schema (migration ledger missing)")
	}
	var versions []struct {
		Version uint `db:"version"`
		Dirty   bool `db:"dirty"`
	}
	if err := conn.Select(&versions, "SELECT version, dirty FROM schema_migrations"); err != nil {
		return fmt.Errorf("reading database version: %w", err)
	}
	if len(versions) == 0 && len(tables) == 1 {
		return nil // a newly initialized migration driver, before its first step
	}
	if len(versions) != 1 {
		return errors.New("database migration ledger must contain exactly one version")
	}
	version := versions[0].Version
	if versions[0].Dirty {
		return fmt.Errorf("database migration %d is incomplete; recover the migration or restore its backup before opening", version)
	}
	if present["native_schema"] {
		var lineage string
		if err := conn.Get(&lineage, "SELECT lineage FROM native_schema WHERE singleton = 1"); err != nil {
			return fmt.Errorf("reading native database lineage: %w", err)
		}
		if lineage != NativeSchemaLineage || version < NativeSchemaBaseline {
			return fmt.Errorf("unsupported database lineage %q at schema %d", lineage, version)
		}
		if version > GetRequiredSchemaVersion() {
			return &MismatchedSchemaVersionError{CurrentSchemaVersion: version, RequiredSchemaVersion: GetRequiredSchemaVersion()}
		}
		for _, name := range []string{"video_file_metadata", "image_file_metadata", "scene_cover_sources", "shares", "share_sessions", "file_deletions", "legacy_schema_history"} {
			if !present[name] {
				return fmt.Errorf("native database schema is incomplete: missing %s", name)
			}
		}
		if present[forkSchemaMigrationsTable] {
			return errors.New("native database still contains an active fork migration ledger")
		}
		if version >= NativeSchemaBaseline+1 {
			for _, name := range []string{"native_migration_history", "saved_filter_import_conflicts"} {
				if !present[name] {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
			var hasAST bool
			if err := conn.Get(&hasAST, "SELECT EXISTS(SELECT 1 FROM pragma_table_info('saved_filters') WHERE name = 'filter_ast')"); err != nil {
				return err
			}
			if !hasAST {
				return errors.New("native database schema is incomplete: missing saved_filters.filter_ast")
			}
		}
		if version >= NativeSchemaBaseline+2 {
			if !present["performer_names"] {
				return errors.New("native database schema is incomplete: missing performer_names")
			}
			var missingPrimary bool
			if err := conn.Get(&missingPrimary, `SELECT EXISTS(SELECT 1 FROM performers
WHERE NOT EXISTS (SELECT 1 FROM performer_names WHERE performer_id = performers.id AND position = 0))`); err != nil {
				return err
			}
			if missingPrimary {
				return errors.New("native database schema is incomplete: performer has no canonical name")
			}
		}
		if version >= NativeSchemaBaseline+3 {
			for _, name := range []string{"default_filters", "configuration_migrations", "default_filter_import_conflicts"} {
				if !present[name] {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
		}
		if version >= NativeSchemaBaseline+4 {
			if !present["archive_entities"] {
				return errors.New("native database schema is incomplete: missing archive_entities")
			}
			var triggers []string
			if err := conn.Select(&triggers, "SELECT name FROM sqlite_schema WHERE type = 'trigger' AND name LIKE 'archive_%'"); err != nil {
				return err
			}
			existing := make(map[string]bool, len(triggers))
			for _, name := range triggers {
				existing[name] = true
			}
			required := []string{"archive_entity_redirect_insert", "archive_entity_redirect_update", "archive_entity_kind_immutable", "archive_entity_no_resurrection"}
			for _, kind := range []string{"performer", "scene", "image", "file"} {
				for _, action := range []string{"created", "changed", "deleted"} {
					required = append(required, "archive_"+kind+"_"+action)
				}
			}
			for _, action := range []string{"insert", "update", "delete"} {
				required = append(required, "archive_performer_name_"+action)
			}
			for _, name := range required {
				if !existing[name] {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
		}
		if version >= NativeSchemaBaseline+5 {
			for _, name := range []string{"source_accounts", "source_account_identifiers", "source_account_identifier_evidence", "account_performer_decisions", "account_performer_links"} {
				if !present[name] {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
			for _, name := range []string{"account_performer_decision_kind_insert", "account_performer_decision_kind_update", "account_performer_decision_immutable", "account_performer_head_forward"} {
				var exists bool
				if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type = 'trigger' AND name = ?)", name); err != nil {
					return err
				}
				if !exists {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
		}
		if version >= NativeSchemaBaseline+6 {
			for _, name := range []string{"source_posts", "source_post_identifiers", "source_payloads", "source_profile_bodies", "source_post_revisions", "source_captures", "source_capture_profiles"} {
				if !present[name] {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
			for _, name := range []string{"source_payload_immutable", "source_profile_body_immutable", "source_post_revision_immutable", "source_capture_immutable", "source_capture_profile_immutable", "source_post_identifier_immutable", "source_post_no_resurrection", "source_capture_active_post"} {
				var exists bool
				if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type = 'trigger' AND name = ?)", name); err != nil {
					return err
				}
				if !exists {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
		}
		if version >= NativeSchemaBaseline+7 {
			var galleryColumn bool
			if err := conn.Get(&galleryColumn, "SELECT EXISTS(SELECT 1 FROM pragma_table_info('archive_entities') WHERE name = 'gallery_id')"); err != nil {
				return err
			}
			if !galleryColumn {
				return errors.New("native database schema is incomplete: missing archive_entities.gallery_id")
			}
			required := []string{"archive_gallery_created", "archive_gallery_changed", "archive_gallery_deleted"}
			for _, table := range []string{"galleries_images", "scenes_galleries", "galleries_files", "performers_galleries", "galleries_tags", "gallery_urls", "gallery_custom_fields", "galleries_chapters"} {
				for _, action := range []string{"insert", "delete", "update"} {
					required = append(required, "archive_gallery_"+table+"_"+action)
				}
			}
			for _, name := range required {
				var exists bool
				if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type = 'trigger' AND name = ?)", name); err != nil {
					return err
				}
				if !exists {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
		}
		if version >= NativeSchemaBaseline+8 {
			for _, name := range []string{"source_attachments", "source_attachment_manifests", "source_attachment_entries", "source_capture_attachment_manifests", "source_media_evidence", "attachment_media_decisions", "attachment_media_links"} {
				if !present[name] {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
			for _, name := range []string{"source_captures_scope", "source_attachment_manifests_immutable", "source_attachment_entries_immutable", "source_capture_attachment_manifests_immutable", "source_attachment_identity_immutable", "source_attachments_active_post", "source_attachment_manifests_active_post", "source_capture_attachment_manifests_active_post", "source_media_evidence_kind_insert", "source_media_evidence_kind_update", "attachment_media_decisions_kind_insert", "attachment_media_decisions_kind_update", "source_media_evidence_immutable", "attachment_media_decision_immutable", "attachment_media_head_forward"} {
				var exists bool
				if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type IN ('index', 'trigger') AND name = ?)", name); err != nil {
					return err
				}
				if !exists {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
		}
		if version >= NativeSchemaBaseline+9 {
			for _, name := range []string{"post_attachment_decisions", "post_attachment_decision_manifests", "post_attachment_selections"} {
				if !present[name] {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
			for _, name := range []string{"post_attachment_decision_immutable", "post_attachment_decision_manifest_immutable", "post_attachment_selection_forward", "post_attachment_decision_active_post"} {
				var exists bool
				if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type = 'trigger' AND name = ?)", name); err != nil {
					return err
				}
				if !exists {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
		}
		if version >= NativeSchemaBaseline+10 {
			for _, name := range []string{"post_gallery_decisions", "post_gallery_links", "source_gallery_write_context", "gallery_membership_events", "gallery_membership_heads"} {
				if !present[name] {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
			var origin, unfinished bool
			if err := conn.Get(&origin, "SELECT EXISTS(SELECT 1 FROM pragma_table_info('galleries') WHERE name = 'origin')"); err != nil {
				return err
			}
			if !origin {
				return errors.New("native database schema is incomplete: missing galleries.origin")
			}
			if err := conn.Get(&unfinished, "SELECT EXISTS(SELECT 1 FROM source_gallery_write_context)"); err != nil {
				return err
			}
			if unfinished {
				return errors.New("native database has an unfinished source gallery write context")
			}
			required := []string{"gallery_origin_immutable", "source_gallery_folder_insert", "source_gallery_folder_update", "source_gallery_file_insert", "source_gallery_file_update", "post_gallery_decision_kind_insert", "post_gallery_decision_kind_update", "post_gallery_link_scope_insert", "post_gallery_link_scope_update", "gallery_membership_kind_insert", "gallery_membership_kind_update", "post_gallery_decision_immutable", "post_gallery_head_forward", "gallery_membership_event_immutable", "gallery_membership_head_forward", "gallery_membership_current", "post_gallery_active_post"}
			for _, table := range []string{"galleries_images", "scenes_galleries"} {
				for _, action := range []string{"insert", "delete", "update"} {
					required = append(required, "source_gallery_"+table+"_"+action)
				}
			}
			for _, name := range required {
				var exists bool
				if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type = 'trigger' AND name = ?)", name); err != nil {
					return err
				}
				if !exists {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
		}
		if version >= NativeSchemaBaseline+11 {
			var columns, objects []string
			if err := conn.Select(&columns, "SELECT name FROM pragma_table_info('archive_entities')"); err != nil {
				return err
			}
			if err := conn.Select(&objects, "SELECT name FROM sqlite_schema WHERE type IN ('index', 'trigger') AND name LIKE 'archive_%'"); err != nil {
				return err
			}
			existingColumns, existingObjects := make(map[string]bool), make(map[string]bool)
			for _, name := range columns {
				existingColumns[name] = true
			}
			for _, name := range objects {
				existingObjects[name] = true
			}
			for kind, tables := range map[string][]string{
				"tag":    {"tag_aliases", "tag_stash_ids", "tag_custom_fields", "tags_relations"},
				"studio": {"studio_aliases", "studio_urls", "studio_stash_ids", "studio_custom_fields", "studios_tags"},
				"group":  {"group_urls", "group_custom_fields", "groups_tags", "groups_relations"},
			} {
				if !existingColumns[kind+"_id"] {
					return fmt.Errorf("native database schema is incomplete: missing archive_entities.%s_id", kind)
				}
				required := []string{"archive_entities_" + kind}
				for _, action := range []string{"created", "changed", "deleted"} {
					required = append(required, "archive_"+kind+"_"+action)
				}
				for _, table := range tables {
					for _, action := range []string{"insert", "delete", "update"} {
						required = append(required, "archive_"+kind+"_"+table+"_"+action)
					}
				}
				for _, name := range required {
					if !existingObjects[name] {
						return fmt.Errorf("native database schema is incomplete: missing %s", name)
					}
				}
			}
		}
		if version >= NativeSchemaBaseline+12 {
			if err := validateMetadataFieldSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+13 {
			if err := validateMetadataCollectionSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+14 {
			if err := validateAccountConsolidationSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+15 {
			if err := validateSourceCollectionSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+16 {
			if err := validateCapturePublisherSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+17 {
			if err := validateIngestSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+18 {
			if err := validateFileContentSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+19 {
			if err := validateArchiveJobSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+20 {
			if err := validateFilePathSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+21 {
			if err := validateFileIngestSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+22 {
			if err := validateMetadataPolicySchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+23 {
			if err := validateSourceRunSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+24 {
			if err := validateIngestRootSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+25 {
			if err := validateSourceBackfillSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+26 {
			if err := validateScanJournalSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+27 {
			if err := validateScanActivationSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+28 {
			if err := validateCatalogIdentitySchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+29 {
			if err := validateCatalogRegistrySchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+30 {
			if err := validateCatalogSnapshotSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+31 {
			if err := validateCatalogEvidenceSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+32 {
			if err := validateSourcePostLinksSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+33 {
			if err := validateCatalogRelationsSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+34 {
			if err := validateCatalogPublisherSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+35 {
			if err := validateCatalogAttachmentSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+36 {
			if err := validatePostMediaEvidenceSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+37 {
			if err := validateSourceFileSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+39 {
			if err := validateCatalogMembershipSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+38 {
			if err := validateCatalogMediaSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+40 {
			if err := validateAlbumJobSchema(conn, version); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+41 {
			if err := validateSourceDocumentSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+42 {
			if err := validateCatalogDocumentSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+43 {
			if err := validateSourceTranslationSchema(conn); err != nil {
				return err
			}
			if err := validateCatalogTranslationSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+44 {
			if err := validateTranslationWorkSchema(conn, version >= NativeSchemaBaseline+47); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+45 {
			if err := validateTranslationJobSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+46 {
			if err := validateAutomationSnapshotSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+47 {
			if err := validateAutomationTranslationSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+48 {
			if err := validateTranslationActivationSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+49 {
			if err := validateTranslationPolicySchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+50 {
			if err := validateEnrichmentWorkSchema(conn, version >= NativeSchemaBaseline+58, version >= NativeSchemaBaseline+65); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+51 {
			if version >= NativeSchemaBaseline+53 {
				if err := validateEnrichmentReleaseSchema(conn); err != nil {
					return err
				}
			}
			if err := validateEnrichmentJobSchema(conn, version >= NativeSchemaBaseline+53, version >= NativeSchemaBaseline+65); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+52 {
			if err := validateEnrichmentPublicationSchema(conn, version >= NativeSchemaBaseline+53); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+54 {
			if err := validateSourcePacingSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+55 {
			if err := validateSourceRunServicesSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+56 {
			if err := validateSourceFairnessSchema(conn, version >= NativeSchemaBaseline+65); err != nil {
				return err
			}
		}

		if version >= NativeSchemaBaseline+57 {
			if err := validateCatalogEnrichmentSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+58 {
			if err := validateAutomationEnrichmentSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+59 {
			if err := validateEnrichmentActivationSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+60 {
			if err := validateAutomationCheckpointSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+61 {
			if err := validateSourceCaptureTimeSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+62 {
			if err := validateCheckpointEvidenceSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+63 {
			if err := validateSourceCaptureContextSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+64 {
			if err := validateCheckpointHandoffSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+65 {
			if err := validateEnrichmentHandoffSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+66 {
			if err := validateSourceFileHistorySchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+67 {
			if err := validateMetadataFileReviewSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+68 {
			if err := validateAccountReviewSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+69 {
			if err := validateAutomationDiscoverySchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+70 {
			if err := validateEnrichmentDiscoverySchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+71 {
			if err := validateDiscoveryJobSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+72 {
			if err := validateDiscoveryMatchSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+73 {
			if err := validateDiscoveryActivationSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+74 {
			if err := validateDiscoveryPublicationSchema(conn); err != nil {
				return err
			}
		}

		return nil
	}
	if version >= NativeSchemaBaseline {
		return fmt.Errorf("schema %d has no native lineage marker; refusing to open an unrelated database", version)
	}
	if version > lastCompatibleSchema && version != legacyForkFirstSchemaVersion && version != legacyForkLastSchemaVersion {
		return fmt.Errorf("unsupported legacy Stash schema %d; last compatible input is %d", version, lastCompatibleSchema)
	}
	if present[forkSchemaMigrationsTable] {
		var forkVersion sql.NullInt64
		if err := conn.Get(&forkVersion, "SELECT MAX(version) FROM fork_schema_migrations"); err != nil {
			return err
		}
		if forkVersion.Valid && (forkVersion.Int64 < 0 || uint(forkVersion.Int64) > GetRequiredForkSchemaVersion()) {
			return fmt.Errorf("unsupported legacy fork schema %d", forkVersion.Int64)
		}
	}
	return nil
}

var promotedTables = map[string]string{
	"fork_performer_autotag_ignored_names": "performer_autotag_ignored_names",
	"fork_saved_filter_state":              "saved_filter_state",
	"fork_video_file_metadata":             "video_file_metadata",
	"fork_image_file_metadata":             "image_file_metadata",
	"fork_scene_cover_sources":             "scene_cover_sources",
	"fork_shares":                          "shares",
	"fork_share_sessions":                  "share_sessions",
	"fork_file_deletions":                  "file_deletions",
}

func (m *Migrator) prepareNativePromotion(ctx context.Context) error {
	known := map[string]string{
		"fork_schema_migrations":     "table",
		"fork_scenes_created_at":     "index",
		"fork_images_created_at":     "index",
		"fork_shares_created":        "index",
		"fork_share_sessions_share":  "index",
		"fork_share_sessions_expiry": "index",
	}
	for old := range promotedTables {
		known[old] = "table"
	}
	var objects []struct {
		Name string `db:"name"`
		Type string `db:"type"`
	}
	if err := m.conn.SelectContext(ctx, &objects, "SELECT name, type FROM sqlite_schema WHERE name GLOB 'fork_*' ORDER BY name"); err != nil {
		return err
	}
	for _, object := range objects {
		if known[object.Name] != object.Type {
			return fmt.Errorf("unrecognized legacy fork object %s (%s); preserve and map it before promotion", object.Name, object.Type)
		}
	}
	for _, name := range promotedTables {
		var exists bool
		if err := m.conn.GetContext(ctx, &exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name = ?)", name); err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("native promotion destination %s already exists", name)
		}
	}
	if err := m.snapshotLegacyHistory(ctx); err != nil {
		return err
	}
	// Historical conversions run once, before the independent schema. Their
	// final reconciliation preserves pending filter conflicts for explicit review.
	if err := m.RunAllForkMigrations(ctx); err != nil {
		return err
	}
	if err := m.RunForkReconcilers(ctx); err != nil {
		return err
	}
	var table, parent string
	var rowID sql.NullInt64
	var fkID int
	err := m.conn.QueryRowContext(ctx, "PRAGMA foreign_key_check").Scan(&table, &rowID, &parent, &fkID)
	if err == nil {
		return fmt.Errorf("legacy database has a foreign-key violation in %s referencing %s (row %v, constraint %d)", table, parent, rowID, fkID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("checking legacy database foreign keys: %w", err)
	}
	return m.snapshotLegacyHistory(ctx)
}

func (m *Migrator) snapshotLegacyHistory(ctx context.Context) error {
	tx, err := m.conn.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS native_promotion_input_history (
  track TEXT NOT NULL,
  version INTEGER NOT NULL,
  name TEXT NOT NULL,
  applied_at DATETIME NOT NULL,
  PRIMARY KEY (track, version)
);
INSERT OR IGNORE INTO native_promotion_input_history
SELECT 'legacy-primary', version, 'legacy Stash schema', CURRENT_TIMESTAMP
FROM schema_migrations WHERE dirty = 0`); err != nil {
		return err
	}
	var exists bool
	if err := tx.GetContext(ctx, &exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name = 'fork_schema_migrations')"); err != nil {
		return err
	}
	if exists {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO native_promotion_input_history
SELECT 'legacy-fork', version, name, applied_at FROM fork_schema_migrations`); err != nil {
			return err
		}
	}
	return tx.Commit()
}
