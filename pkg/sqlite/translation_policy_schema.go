package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func validateTranslationPolicySchema(conn *sqlx.DB, auditData bool) error {
	for _, object := range []struct{ name, kind string }{
		{"translation_policies", "table"}, {"translation_policy_revisions", "table"},
		{"capture_translation_decisions", "table"}, {"capture_translation_entries", "table"},
		{"translation_policy_revision_scope", "trigger"}, {"translation_policy_revision_publish", "trigger"},
		{"translation_policy_revision_immutable", "trigger"}, {"translation_policy_identity_immutable", "trigger"},
		{"capture_translation_decision_immutable", "trigger"}, {"capture_translation_entry_immutable", "trigger"},
		{"capture_translation_target", "index"},
	} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=? AND type=?)", object.name, object.kind); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", object.name)
		}
	}
	var invalid bool
	if !auditData {
		return nil
	}
	if err := conn.Get(&invalid, `SELECT
 EXISTS(SELECT 1 FROM translation_policies p WHERE p.revision!=(SELECT count(*) FROM translation_policy_revisions r WHERE r.collection_uuid=p.collection_uuid))
 OR EXISTS(SELECT 1 FROM translation_policy_revisions r LEFT JOIN translation_policies p ON p.collection_uuid=r.collection_uuid
 LEFT JOIN source_collection_revisions c ON c.collection_uuid=r.collection_uuid AND c.revision=r.collection_revision
 WHERE p.collection_uuid IS NULL OR c.collection_uuid IS NULL OR r.revision>p.revision)
 OR EXISTS(SELECT 1 FROM capture_translation_decisions d
 LEFT JOIN source_collection_captures c ON c.collection_uuid=d.collection_uuid AND c.capture_uuid=d.capture_uuid AND c.collection_revision=d.collection_revision
 LEFT JOIN translation_policy_revisions p ON p.collection_uuid=d.collection_uuid AND p.revision=d.policy_revision
 WHERE c.capture_uuid IS NULL OR (d.policy_revision IS NOT NULL AND p.collection_uuid IS NULL)
 OR (d.status='no_policy')!=(d.policy_revision IS NULL)
 OR d.entry_count!=(SELECT count(*) FROM capture_translation_entries e WHERE e.collection_uuid=d.collection_uuid AND e.capture_uuid=d.capture_uuid AND e.collection_revision=d.collection_revision)
 OR (d.status='recorded' AND (json_extract(p.definition,'$.enabled') IS NOT 1 OR p.collection_revision!=d.collection_revision
  OR d.entry_count!=json_extract(p.definition,'$.title')+json_extract(p.definition,'$.caption')))
 OR (d.status='disabled' AND json_extract(p.definition,'$.enabled') IS NOT 0))
 OR EXISTS(SELECT 1 FROM capture_translation_entries e
 LEFT JOIN capture_translation_decisions d ON d.collection_uuid=e.collection_uuid AND d.capture_uuid=e.capture_uuid AND d.collection_revision=e.collection_revision
 LEFT JOIN translation_policy_revisions p ON p.collection_uuid=d.collection_uuid AND p.revision=d.policy_revision
 LEFT JOIN source_captures c ON c.uuid=e.capture_uuid
 LEFT JOIN translation_targets t ON t.uuid=e.target_uuid
 LEFT JOIN translation_target_history h ON h.target_uuid=e.target_uuid AND h.revision=e.target_revision
 WHERE d.status IS NOT 'recorded' OR c.uuid IS NULL
 OR (e.field='title' AND json_extract(p.definition,'$.title') IS NOT 1)
 OR (e.field='caption' AND json_extract(p.definition,'$.caption') IS NOT 1)
 OR (e.target_uuid IS NOT NULL AND (h.target_uuid IS NULL OR t.post_uuid IS NOT c.post_uuid OR t.collection_uuid IS NOT e.collection_uuid
  OR t.collection_revision IS NOT e.collection_revision OR t.field IS NOT e.field))
 OR (e.status='created' AND (e.target_revision!=1 OR t.origin IS NOT 'capture' OR h.state IS NOT 'pending'
  OR h.priority IS NOT json_extract(p.definition,'$.priority') OR h.not_before IS NOT d.created_at OR h.recorded_at IS NOT d.created_at)))`); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	if err := validateTranslationPolicyRows(conn); err != nil {
		return err
	}
	return validateCaptureTranslationRows(conn)
}

func validateTranslationPolicyRows(conn *sqlx.DB) error {
	rows, err := conn.Queryx("SELECT * FROM translation_policy_revisions")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row translationPolicyRow
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		if _, err := row.resolve(); err != nil || !validJobTime(row.CreatedAt) {
			return models.ErrSourcePayloadCorrupt
		}
	}
	return rows.Err()
}

func validateCaptureTranslationRows(conn *sqlx.DB) error {
	// All comparisons use the saved policy and target revision. Current policies,
	// target states and post tombstones may legitimately have changed since then.
	rows, err := conn.Queryx(`SELECT d.created_at,p.definition,e.field,e.status,
 CASE WHEN e.field='title' THEN json_extract(r.metadata,'$.title') WHEN e.field='caption' THEN json_extract(r.metadata,'$.original_text') END AS source_text,
 q.original_text,q.target_language,q.policy,h.recorded_at
 FROM capture_translation_decisions d
 LEFT JOIN capture_translation_entries e ON e.collection_uuid=d.collection_uuid AND e.capture_uuid=d.capture_uuid AND e.collection_revision=d.collection_revision
 LEFT JOIN translation_policy_revisions p ON p.collection_uuid=d.collection_uuid AND p.revision=d.policy_revision
 JOIN source_captures c ON c.uuid=d.capture_uuid JOIN source_post_revisions r ON r.uuid=c.revision_uuid
 LEFT JOIN translation_targets t ON t.uuid=e.target_uuid LEFT JOIN translation_requests q ON q.uuid=t.request_uuid
 LEFT JOIN translation_target_history h ON h.target_uuid=e.target_uuid AND h.revision=e.target_revision`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row struct {
			CreatedAt      time.Time    `db:"created_at"`
			Definition     *string      `db:"definition"`
			Field          *string      `db:"field"`
			Status         *string      `db:"status"`
			SourceText     *string      `db:"source_text"`
			OriginalText   *string      `db:"original_text"`
			TargetLanguage *string      `db:"target_language"`
			Policy         *string      `db:"policy"`
			RecordedAt     sql.NullTime `db:"recorded_at"`
		}
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		if !validJobTime(row.CreatedAt) {
			return models.ErrSourcePayloadCorrupt
		}
		if row.Field == nil {
			continue
		}
		if row.Definition == nil || row.Status == nil {
			return models.ErrSourcePayloadCorrupt
		}
		policy, err := archive.DecodeTranslationPolicy([]byte(*row.Definition))
		if err != nil {
			return err
		}
		empty := row.SourceText == nil || strings.TrimSpace(*row.SourceText) == ""
		switch *row.Status {
		case "no_text":
			if !empty {
				return models.ErrSourcePayloadCorrupt
			}
		case "created", "retained":
			if empty || row.OriginalText == nil || *row.OriginalText != *row.SourceText || row.TargetLanguage == nil || *row.TargetLanguage != policy.TargetLanguage ||
				row.Policy == nil || *row.Policy != policy.ProviderPolicy || !row.RecordedAt.Valid || row.RecordedAt.Time.After(row.CreatedAt) {
				return models.ErrSourcePayloadCorrupt
			}
		default:
			return models.ErrSourcePayloadCorrupt
		}
	}
	return rows.Err()
}
