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

// validateDatabaseLineage includes the full domain/history audit used before
// migrations and during explicit snapshot verification.
func validateDatabaseLineage(path string) error {
	return validateNativeDatabase(path, true)
}

// Routine opens check lineage, schema objects and unfinished write contexts.
// Auditing all retained rows is a separate migration/verification operation.
func validateNativeDatabase(path string, auditData bool) error {
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
	return validateNativeConnection(conn, auditData)
}

func validateNativeConnection(conn *sqlx.DB, auditData bool) error {
	var tables []string
	if err := conn.Select(&tables, "SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%'"); err != nil {
		return fmt.Errorf("reading database identity: %w", err)
	}
	if len(tables) == 0 {
		fresh, err := isPristineDatabase(context.Background(), conn)
		if err != nil {
			return err
		}
		if !fresh {
			return errors.New("database is not a supported Stash schema (nonempty schema or database header without migration ledger)")
		}
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
		// A pre-baseline binary could stop after creating just its ledger. Keep
		// that recovery path on the historical chain, never the native baseline.
		var unexpected bool
		if err := conn.Get(&unexpected, `SELECT EXISTS(SELECT 1 FROM sqlite_schema
WHERE NOT (type='table' AND name='schema_migrations')
AND NOT (type='index' AND tbl_name='schema_migrations'))`); err != nil {
			return err
		}
		if !unexpected {
			return nil
		}
	}
	if len(versions) != 1 {
		return errors.New("database migration ledger must contain exactly one version")
	}
	version := versions[0].Version
	if versions[0].Dirty {
		return fmt.Errorf("database migration %d is incomplete; recover the migration or restore its backup before opening", version)
	}
	if version == 0 {
		return errors.New("database migration version 0 is incomplete; recover the migration before opening")
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
		if version >= NativeSchemaBaseline+126 {
			for _, name := range []string{"library_bulk_jobs", "library_bulk_targets", "library_bulk_targets_pending"} {
				var exists bool
				if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
					return err
				}
				if !exists {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
			var generation bool
			if err := conn.Get(&generation, "SELECT EXISTS(SELECT 1 FROM pragma_table_info('library_jobs') WHERE name='generation' AND type='INTEGER' AND \"notnull\"=1)"); err != nil {
				return err
			}
			if !generation {
				return errors.New("native database schema is incomplete: missing library_jobs.generation")
			}
		}
		if version >= NativeSchemaBaseline+125 {
			for _, name := range []string{"scene_cover_upgrade", "scene_cover_claims", "scene_cover_upgrade_uncaptured", "scene_cover_upgrade_artwork", "scene_cover_upgrade_selected", "scene_cover_upgrade_reselected"} {
				var exists bool
				if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
					return err
				}
				if !exists {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
		}
		if version >= NativeSchemaBaseline+124 {
			if !present["file_moves"] {
				return errors.New("native database schema is incomplete: missing file_moves")
			}
			var valid bool
			if err := conn.Get(&valid, `SELECT count(*)=1 AND coalesce(sum(name='id' AND type='TEXT' AND "notnull"=1 AND pk=1),0)=1
FROM pragma_table_info('file_moves')`); err != nil {
				return err
			}
			if !valid {
				return errors.New("native database schema is incomplete: invalid file_moves columns")
			}
		}
		if version >= NativeSchemaBaseline+123 {
			for _, name := range []string{"library_jobs", "library_jobs_recent", "library_jobs_pending"} {
				var exists bool
				if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
					return err
				}
				if !exists {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
		}
		if version >= NativeSchemaBaseline+118 && !present["source_gallery_performer_pending"] {
			return errors.New("native database schema is incomplete: missing source_gallery_performer_pending")
		}
		if version >= NativeSchemaBaseline+119 && (!present["source_gallery_studio_pending"] || !present["source_collection_studios"]) {
			return errors.New("native database schema is incomplete: missing source studio attribution")
		}
		if version >= NativeSchemaBaseline+120 && (!present["source_post_translation_display"] || !present["translation_policy_defaults"]) {
			return errors.New("native database schema is incomplete: missing source_post_translation_display")
		}
		if version >= NativeSchemaBaseline+121 {
			for _, name := range []string{"source_post_import_labels", "source_post_studios", "catalog_membership_compaction_guard"} {
				var exists bool
				if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
					return err
				}
				if !exists {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
		}
		if version >= NativeSchemaBaseline+117 {
			for _, name := range []string{"gallery_alternate_titles", "source_gallery_merge_context"} {
				if !present[name] {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
			var unfinished bool
			if err := conn.Get(&unfinished, "SELECT EXISTS(SELECT 1 FROM source_gallery_merge_context)"); err != nil {
				return err
			}
			if unfinished {
				return errors.New("native database contains an unfinished gallery consolidation")
			}
		}
		if version >= NativeSchemaBaseline+105 {
			for _, name := range []string{"post_gallery_links", "source_post_threads", "source_post_threads_conversation", "source_post_threads_author", "source_post_threads_parent", "source_post_thread_scope", "source_post_thread_immutable", "post_gallery_thread_share_insert", "post_gallery_thread_share_update"} {
				var exists bool
				if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
					return err
				}
				if !exists {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
		}
		if version >= NativeSchemaBaseline+104 {
			if err := validateWorkerPolicySchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+101 {
			for _, name := range []string{"source_capture_sightings", "source_capture_content", "source_capture_content_post"} {
				var exists bool
				if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
					return err
				}
				if !exists {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
		}
		if version >= NativeSchemaBaseline+1 {
			if version >= NativeSchemaBaseline+100 {
				for _, name := range []string{"catalog_document_pending_imports", "catalog_nfo_compaction_guard"} {
					var exists bool
					if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
						return err
					}
					if !exists {
						return fmt.Errorf("native database schema is incomplete: missing %s", name)
					}
				}
			}
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
			if auditData {
				var missingPrimary bool
				if err := conn.Get(&missingPrimary, `SELECT EXISTS(SELECT 1 FROM performers
WHERE NOT EXISTS (SELECT 1 FROM performer_names WHERE performer_id = performers.id AND position = 0))`); err != nil {
					return err
				}
				if missingPrimary {
					return errors.New("native database schema is incomplete: performer has no canonical name")
				}
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
			kinds := map[string][]string{
				"tag":    {"tag_aliases", "tag_stash_ids", "tag_custom_fields", "tags_relations"},
				"studio": {"studio_aliases", "studio_urls", "studio_stash_ids", "studio_custom_fields", "studios_tags"},
				"group":  {"group_urls", "group_custom_fields", "groups_tags", "groups_relations"},
			}
			if version >= NativeSchemaBaseline+115 {
				for _, kind := range []string{"folder", "scene_marker", "gallery_chapter", "saved_filter"} {
					kinds[kind] = nil
				}
				var coverColumn bool
				if err := conn.Get(&coverColumn, "SELECT EXISTS(SELECT 1 FROM pragma_table_info('scene_cover_sources') WHERE name='source_file_uuid')"); err != nil {
					return err
				}
				if !coverColumn {
					return errors.New("native database schema is incomplete: missing scene_cover_sources.source_file_uuid")
				}
			}
			for kind, tables := range kinds {
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
			if err := validateAccountConsolidationSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+15 {
			if err := validateSourceCollectionSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+16 {
			if err := validateCapturePublisherSchema(conn, auditData); err != nil {
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
		if version >= NativeSchemaBaseline+107 {
			if err := validateInterruptedWorkSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+108 {
			if err := validateGalleryCoverSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+109 {
			if err := validateMediaConversionSchema(conn, auditData); err != nil {
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
			if err := validateMetadataPolicySchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+23 {
			if err := validateSourceRunSchema(conn, version, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+24 {
			if err := validateIngestRootSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+25 {
			if err := validateSourceBackfillSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+26 {
			if err := validateScanJournalSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+27 {
			if err := validateScanActivationSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+28 {
			if err := validateCatalogIdentitySchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+29 {
			if err := validateCatalogRegistrySchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+30 {
			if err := validateCatalogSnapshotSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+31 {
			if err := validateCatalogEvidenceSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+32 {
			if err := validateSourcePostLinksSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+33 {
			if err := validateCatalogRelationsSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+34 {
			if err := validateCatalogPublisherSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+35 {
			if err := validateCatalogAttachmentSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+36 {
			if err := validatePostMediaEvidenceSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+37 {
			if err := validateSourceFileSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+39 {
			if err := validateCatalogMembershipSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+38 {
			if err := validateCatalogMediaSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+40 {
			if err := validateAlbumJobSchema(conn, version, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+41 {
			if err := validateSourceDocumentSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+42 {
			if err := validateCatalogDocumentSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+43 {
			if err := validateSourceTranslationSchema(conn, auditData); err != nil {
				return err
			}
			if err := validateCatalogTranslationSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+44 {
			if err := validateTranslationWorkSchema(conn, version >= NativeSchemaBaseline+47, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+45 {
			if err := validateTranslationJobSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+46 {
			if err := validateAutomationSnapshotSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+47 {
			if err := validateAutomationTranslationSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+48 {
			if err := validateTranslationActivationSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+49 {
			if err := validateTranslationPolicySchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+50 {
			if err := validateEnrichmentWorkSchema(conn, version >= NativeSchemaBaseline+58, version >= NativeSchemaBaseline+65, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+51 {
			if version >= NativeSchemaBaseline+53 {
				if err := validateEnrichmentReleaseSchema(conn, auditData); err != nil {
					return err
				}
			}
			if err := validateEnrichmentJobSchema(conn, version >= NativeSchemaBaseline+53, version >= NativeSchemaBaseline+65, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+52 {
			if err := validateEnrichmentPublicationSchema(conn, version >= NativeSchemaBaseline+53, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+54 {
			if err := validateSourcePacingSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+111 {
			for _, name := range []string{"source_request_budgets", "source_request_permits", "source_request_metrics", "source_request_posts", "source_check_health", "source_request_waiters", "source_request_posts_retention"} {
				var found bool
				if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
					return err
				}
				if !found {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
		}
		if version >= NativeSchemaBaseline+127 {
			var columns int
			if err := conn.Get(&columns, `SELECT (SELECT count(*) FROM pragma_table_info('source_request_budgets') WHERE name IN ('backoff_step','backoff_at_ms','blocked_until_ms','probe_uuid','probe_until_ms','probe_owner'))
+ (SELECT count(*) FROM pragma_table_info('source_request_permits') WHERE name='backoff')
+ (SELECT count(*) FROM pragma_table_info('metadata_request_permits') WHERE name='backoff')`); err != nil {
				return err
			}
			if columns != 8 {
				return errors.New("native database schema is incomplete: source request backoff")
			}
		}
		if version >= NativeSchemaBaseline+128 {
			for _, name := range []string{"media_root_identity_upgrades", "media_root_identity_upgrade_scope", "media_root_identity_upgrade_immutable", "media_root_identity_upgrade_retained"} {
				var exists bool
				if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
					return err
				}
				if !exists {
					return fmt.Errorf("native database schema is incomplete: missing %s", name)
				}
			}
		}
		if version >= NativeSchemaBaseline+112 {
			var found bool
			if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='table' AND name='metadata_request_permits')"); err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("native database schema is incomplete: missing metadata_request_permits")
			}
		}
		if version >= NativeSchemaBaseline+113 {
			var columns int
			if err := conn.Get(&columns, `SELECT (SELECT count(*) FROM pragma_table_info('media_roots') WHERE name IN ('scan_revision','scan_enabled','exclude_video','exclude_image'))
+ (SELECT count(*) FROM pragma_table_info('media_root_revisions') WHERE name='unbound_path')`); err != nil {
				return err
			}
			if columns != 5 {
				return errors.New("native database schema is incomplete: library folder settings")
			}
		}
		if version >= NativeSchemaBaseline+55 {
			if err := validateSourceRunServicesSchema(conn, version >= NativeSchemaBaseline+99, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+56 {
			if err := validateSourceFairnessSchema(conn, version >= NativeSchemaBaseline+65, version >= NativeSchemaBaseline+76, auditData); err != nil {
				return err
			}
		}

		if version >= NativeSchemaBaseline+57 {
			if err := validateCatalogEnrichmentSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+58 {
			if err := validateAutomationEnrichmentSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+59 {
			if err := validateEnrichmentActivationSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+60 {
			if err := validateAutomationCheckpointSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+61 {
			if err := validateSourceCaptureTimeSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+62 {
			if err := validateCheckpointEvidenceSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+63 {
			if err := validateSourceCaptureContextSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+64 {
			if err := validateCheckpointHandoffSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+65 {
			if err := validateEnrichmentHandoffSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+66 {
			if err := validateSourceFileHistorySchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+67 {
			if err := validateMetadataFileReviewSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+68 {
			if err := validateAccountReviewSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+88 {
			if err := validatePostIdentitySchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+89 {
			if err := validatePostSelectionProvenanceSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+92 {
			if err := validatePostConsolidationReviewSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+94 {
			if err := validateMetadataFileKeepSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+95 {
			if err := validateFileDeduplicationSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+96 {
			if err := validateProviderMetadataSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+97 {
			if err := validatePerformerProfileURLSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+93 {
			if err := validateArchiveActivitySchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+87 {
			if err := validateAttachmentDownloadSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+86 {
			if err := validateSourceAssociationReviewSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+85 {
			if err := validateAttachmentSelectionReviewSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+69 {
			if err := validateAutomationDiscoverySchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+70 {
			if err := validateEnrichmentDiscoverySchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+71 {
			if version >= NativeSchemaBaseline+84 {
				if err := validateDiscoveryScopeSchema(conn, auditData); err != nil {
					return fmt.Errorf("invalid discovery collection bindings: %w", err)
				}
			}
			if err := validateDiscoveryJobSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+72 {
			if err := validateDiscoveryMatchSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+73 {
			if err := validateDiscoveryActivationSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+74 {
			if err := validateDiscoveryPublicationSchema(conn, version >= NativeSchemaBaseline+77, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+75 {
			if err := validateDiscoveryRecoverySchema(conn, auditData); err != nil {
				return err
			}
		}

		if version >= NativeSchemaBaseline+76 {
			if err := validateDiscoveryDetailSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+79 {
			if err := validateMetadataNameSchema(conn); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+80 {
			if err := validateCatalogCleanupSchema(conn, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+81 {
			if err := validatePostMediaDecisionSchema(conn, version >= NativeSchemaBaseline+90, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+82 {
			if err := validatePostMediaBackfillSchema(conn, version >= NativeSchemaBaseline+91, auditData); err != nil {
				return err
			}
		}
		if version >= NativeSchemaBaseline+83 {
			if err := validateEnrichmentRebindSchema(conn, auditData); err != nil {
				return fmt.Errorf("invalid enrichment collection rebindings: %w", err)
			}
		}
		if version >= NativeSchemaBaseline+78 {
			if err := validateMetadataPolicyImportSchema(conn, auditData); err != nil {
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
