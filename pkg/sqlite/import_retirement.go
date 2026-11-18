package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// importRetirementVersion removes the October 2026 catalog/automation import
// staging, its per-row receipts, activation receipts and the discovery work that
// existed only to match imported history. Imported posts, media, captures,
// decisions and completed work remain ordinary native records. Validators for
// the retired families apply only to databases below this version.
const importRetirementVersion = NativeSchemaBaseline + 142

// validateRetainedImportSchema checks the native objects that arrived with the
// import schemas and outlive it: post collection membership, the historical
// enrichment receipts that completions still cite, and the indexes their
// queries rely on. The retired import validators used to check them.
func validateRetainedImportSchema(conn *sqlx.DB, version uint, auditData bool) error {
	var names []string
	if version >= NativeSchemaBaseline+39 {
		names = append(names, "source_collection_post_evidence", "source_collection_post_evidence_post", "source_collection_post_evidence_collection",
			"source_collection_post_evidence_immutable", "source_collection_post_evidence_active_post", "source_collection_post_evidence_revision")
	}
	if version >= NativeSchemaBaseline+57 {
		names = append(names, "source_enrichment_receipts", "source_enrichment_receipts_post", "source_enrichment_receipts_collection",
			"source_enrichment_receipt_immutable", "source_enrichment_receipt_scope")
	}
	if version >= NativeSchemaBaseline+58 {
		names = append(names, "enrichment_completion_legacy_receipt", "enrichment_completion_legacy_capture")
	}
	if version >= NativeSchemaBaseline+83 {
		names = append(names, "enrichment_targets_pending_scope")
	}
	for _, name := range names {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	if version < NativeSchemaBaseline+39 {
		return nil
	}
	var typed bool
	if err := conn.Get(&typed, "SELECT EXISTS(SELECT 1 FROM pragma_table_info('source_collection_post_evidence') WHERE name='observed_at' AND upper(type)='DATETIME')"); err != nil {
		return err
	}
	if !typed {
		return errors.New("native database schema is incomplete: invalid source_collection_post_evidence.observed_at")
	}
	if !auditData {
		return nil
	}
	query := `SELECT EXISTS(SELECT 1 FROM source_collection_post_evidence e
 LEFT JOIN source_posts p ON p.uuid=e.post_uuid
 LEFT JOIN source_collection_revisions c ON c.collection_uuid=e.collection_uuid AND c.revision=e.collection_revision
 WHERE p.uuid IS NULL OR c.collection_uuid IS NULL)`
	if version >= NativeSchemaBaseline+58 {
		query += ` OR EXISTS(SELECT 1 FROM enrichment_completions e LEFT JOIN source_enrichment_receipts r ON r.uuid=e.legacy_receipt_uuid
 WHERE e.legacy_receipt_uuid IS NOT NULL AND r.uuid IS NULL)`
	}
	var invalid bool
	if err := conn.Get(&invalid, query); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has invalid collection membership or enrichment receipt references")
	}
	return nil
}
