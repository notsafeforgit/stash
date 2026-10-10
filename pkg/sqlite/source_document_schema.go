package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func validateSourceDocumentSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{
		"source_document_contents", "source_document_content_immutable", "source_documents", "source_documents_content", "source_document_immutable",
		"source_document_sources", "source_document_sources_post", "source_document_sources_location", "source_document_sources_document",
		"source_document_source_immutable", "source_document_source_active", "source_document_source_revision",
		"source_document_head_claims", "source_document_head_claims_location", "source_document_head_claims_source",
		"source_document_head_claim_scope", "source_document_head_claim_immutable", "source_document_head_claim_revision",
		"source_document_head_decisions", "source_document_head_decisions_source", "source_document_head_decisions_claim", "source_document_heads",
		"source_document_head_decision_valid", "source_document_head_decision_immutable", "source_document_head_insert_valid", "source_document_head_update_valid", "source_document_head_publish",
	} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	for _, item := range []struct{ table, column string }{
		{"source_document_sources", "recorded_at"}, {"source_document_head_claims", "recorded_at"}, {"source_document_head_decisions", "created_at"},
	} {
		var typed bool
		if err := conn.Get(&typed, "SELECT EXISTS(SELECT 1 FROM pragma_table_info(?) WHERE name=? AND upper(type)='DATETIME')", item.table, item.column); err != nil {
			return err
		}
		if !typed {
			return fmt.Errorf("native database schema is incomplete: invalid %s.%s", item.table, item.column)
		}
	}
	var invalid bool
	if !auditData {
		return nil
	}
	err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM source_documents d LEFT JOIN source_document_contents c ON c.content_sha256=d.content_sha256 WHERE c.content_sha256 IS NULL)
OR EXISTS(SELECT 1 FROM source_document_sources s
 LEFT JOIN source_documents d ON d.uuid=s.document_uuid
 LEFT JOIN source_collection_revisions c ON c.collection_uuid=s.collection_uuid AND c.revision=s.collection_revision
 LEFT JOIN source_posts p ON p.uuid=s.post_uuid
 WHERE d.uuid IS NULL OR c.collection_uuid IS NULL OR (s.post_uuid IS NOT NULL AND p.uuid IS NULL))
OR EXISTS(SELECT 1 FROM source_document_head_claims c LEFT JOIN source_document_sources s ON s.uuid=c.source_uuid
 WHERE s.uuid IS NULL OR s.collection_uuid IS NOT c.collection_uuid OR s.relative_path IS NOT c.relative_path)
OR EXISTS(SELECT 1 FROM source_document_head_decisions d
 LEFT JOIN source_collections collection ON collection.uuid=d.collection_uuid
 LEFT JOIN source_document_sources s ON s.uuid=d.source_uuid
 LEFT JOIN source_document_head_claims c ON c.uuid=d.claim_uuid
 WHERE collection.uuid IS NULL OR (d.source_uuid IS NOT NULL AND (s.uuid IS NULL OR s.collection_uuid IS NOT d.collection_uuid OR s.relative_path IS NOT d.relative_path))
 OR (d.claim_uuid IS NOT NULL AND (c.uuid IS NULL OR c.source_uuid IS NOT d.source_uuid)))
OR EXISTS(SELECT 1 FROM source_document_heads h LEFT JOIN source_document_head_decisions d ON d.uuid=h.decision_uuid
 WHERE d.uuid IS NULL OR d.collection_uuid IS NOT h.collection_uuid OR d.relative_path IS NOT h.relative_path
 OR d.revision!=(SELECT max(x.revision) FROM source_document_head_decisions x WHERE x.collection_uuid=h.collection_uuid AND x.relative_path=h.relative_path))
OR EXISTS(SELECT 1 FROM source_document_head_decisions d GROUP BY collection_uuid,relative_path
 HAVING min(revision)!=1 OR max(revision)!=count(*) OR NOT EXISTS(SELECT 1 FROM source_document_heads h WHERE h.collection_uuid=d.collection_uuid AND h.relative_path=d.relative_path))`)
	if err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has invalid source document scope or selection history")
	}
	if err := validateDocumentContents(conn); err != nil {
		return err
	}
	if err := validateDocumentInterpretations(conn); err != nil {
		return err
	}
	rows, err := conn.Query(`SELECT relative_path,captured_at FROM source_document_sources UNION ALL SELECT relative_path,observed_at FROM source_document_head_claims`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var path, stamp string
		if err := rows.Scan(&path, &stamp); err != nil {
			return err
		}
		if !archive.ValidDocumentPath(path) || !archive.ValidDocumentSourceTime(stamp) {
			return models.ErrSourceDocumentInvalid
		}
	}
	return rows.Err()
}

func validateDocumentContents(conn *sqlx.DB) error {
	rows, err := conn.Queryx("SELECT * FROM source_document_contents")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row documentContentRow
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		if _, err := decodeDocumentContent(row); err != nil {
			return err
		}
	}
	return rows.Err()
}

func validateDocumentInterpretations(conn *sqlx.DB) error {
	rows, err := conn.Queryx("SELECT " + documentColumns + " FROM source_documents d JOIN source_document_contents c ON c.content_sha256=d.content_sha256")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var doc models.SourceDocument
		if err := rows.StructScan(&doc); err != nil {
			return err
		}
		id, err := archive.DocumentIdentity(doc)
		if err != nil || id != doc.UUID {
			return models.ErrSourcePayloadCorrupt
		}
	}
	return rows.Err()
}
