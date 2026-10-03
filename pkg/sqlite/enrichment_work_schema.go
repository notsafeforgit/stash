package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func validateEnrichmentWorkSchema(conn *sqlx.DB) error {
	for _, object := range []struct{ name, kind string }{
		{"enrichment_targets", "table"}, {"enrichment_completions", "table"}, {"enrichment_completion_captures", "table"}, {"enrichment_target_history", "table"},
		{"enrichment_targets_post", "index"}, {"enrichment_targets_post_state", "index"},
		{"enrichment_targets_collection", "index"}, {"enrichment_targets_collection_state", "index"}, {"enrichment_targets_ready", "index"},
		{"enrichment_target_initial", "trigger"}, {"enrichment_target_identity", "trigger"}, {"enrichment_target_scope", "trigger"},
		{"enrichment_completion_scope", "trigger"}, {"enrichment_completion_immutable", "trigger"},
		{"enrichment_completion_capture_scope", "trigger"}, {"enrichment_completion_capture_immutable", "trigger"},
		{"enrichment_target_history_immutable", "trigger"}, {"enrichment_target_history_insert", "trigger"}, {"enrichment_target_history_update", "trigger"},
	} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=? AND type=?)", object.name, object.kind); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", object.name)
		}
	}
	for _, field := range []struct{ table, column string }{
		{"enrichment_targets", "created_at"}, {"enrichment_targets", "updated_at"}, {"enrichment_targets", "not_before"},
		{"enrichment_completions", "created_at"}, {"enrichment_target_history", "not_before"}, {"enrichment_target_history", "recorded_at"},
	} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM pragma_table_info(?) WHERE name=? AND upper(type)='DATETIME')", field.table, field.column); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database has an invalid enrichment timestamp type: %s.%s", field.table, field.column)
		}
	}
	var invalid bool
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM enrichment_targets t
 LEFT JOIN source_posts p ON p.uuid=t.post_uuid LEFT JOIN source_post_urls u ON u.uuid=t.url_uuid
 LEFT JOIN source_collection_revisions c ON c.collection_uuid=t.collection_uuid AND c.revision=t.collection_revision
 LEFT JOIN enrichment_completions e ON e.uuid=t.completion_uuid
 WHERE p.uuid IS NULL OR u.post_uuid IS NOT t.post_uuid OR c.collection_uuid IS NULL
 OR (t.completion_uuid IS NOT NULL AND e.target_uuid IS NOT t.uuid)
 OR t.revision!=(SELECT count(*) FROM enrichment_target_history h WHERE h.target_uuid=t.uuid)
 OR t.revision!=(SELECT max(revision) FROM enrichment_target_history h WHERE h.target_uuid=t.uuid)
 OR NOT EXISTS(SELECT 1 FROM enrichment_target_history h WHERE h.target_uuid=t.uuid AND h.revision=t.revision
  AND h.state=t.state AND h.priority=t.priority AND h.not_before=t.not_before AND h.reason=t.reason
  AND h.completion_uuid IS t.completion_uuid AND h.recorded_at=t.updated_at))
 OR EXISTS(SELECT 1 FROM enrichment_target_history h LEFT JOIN enrichment_targets t ON t.uuid=h.target_uuid
 LEFT JOIN enrichment_target_history previous ON previous.target_uuid=h.target_uuid AND previous.revision=h.revision-1
 WHERE t.uuid IS NULL OR h.revision<1 OR h.revision>t.revision
 OR (h.state='completed')!=(h.completion_uuid IS NOT NULL)
 OR (h.revision=1 AND (h.state='completed' OR h.recorded_at IS NOT t.created_at))
 OR (h.state='completed' AND (h.revision!=t.revision OR h.completion_uuid IS NOT t.completion_uuid
  OR previous.state IS NOT 'pending' OR h.priority IS NOT previous.priority OR h.not_before IS NOT previous.not_before))
 OR (h.revision>1 AND (previous.revision IS NULL OR h.recorded_at<previous.recorded_at)))
 OR EXISTS(SELECT 1 FROM enrichment_completions e LEFT JOIN enrichment_targets t ON t.uuid=e.target_uuid
 LEFT JOIN enrichment_target_history h ON h.target_uuid=e.target_uuid AND h.revision=e.expected_revision
 WHERE t.completion_uuid IS NOT e.uuid OR t.state IS NOT 'completed' OR t.revision!=e.expected_revision+1
 OR e.created_at IS NOT t.updated_at OR h.state IS NOT 'pending' OR h.recorded_at>e.created_at OR h.not_before>e.created_at
 OR e.capture_count!=(SELECT count(*) FROM enrichment_completion_captures c WHERE c.completion_uuid=e.uuid))
 OR EXISTS(SELECT 1 FROM enrichment_completion_captures e LEFT JOIN enrichment_completions r ON r.uuid=e.completion_uuid
 LEFT JOIN enrichment_targets t ON t.uuid=r.target_uuid LEFT JOIN source_captures c ON c.uuid=e.capture_uuid
 LEFT JOIN source_collection_captures b ON b.capture_uuid=e.capture_uuid AND b.collection_uuid=t.collection_uuid AND b.collection_revision=t.collection_revision
 WHERE t.uuid IS NULL OR c.post_uuid IS NOT t.post_uuid OR c.origin NOT IN ('gallery-dl','gallery-dl-enrichment') OR b.capture_uuid IS NULL)`); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	if err := validateEnrichmentTargets(conn); err != nil {
		return err
	}
	if err := validateEnrichmentHistory(conn); err != nil {
		return err
	}
	return validateEnrichmentCompletions(conn)
}

func validateEnrichmentTargets(conn *sqlx.DB) error {
	rows, err := conn.Queryx(enrichmentTargetSelect)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row models.EnrichmentTarget
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		if err := validateEnrichmentTarget(&row); err != nil {
			return err
		}
	}
	return rows.Err()
}

func validateEnrichmentHistory(conn *sqlx.DB) error {
	rows, err := conn.Queryx("SELECT * FROM enrichment_target_history")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row models.EnrichmentTargetHistory
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		if !validEnrichmentSchedule(row.EnrichmentSchedule) || !validJobTime(row.RecordedAt) {
			return models.ErrSourcePayloadCorrupt
		}
	}
	return rows.Err()
}

func validateEnrichmentCompletions(conn *sqlx.DB) error {
	// Stream one bounded completion at a time; do not reconstruct post payloads.
	rows, err := conn.Queryx(`SELECT e.*,c.capture_uuid FROM enrichment_completions e
 LEFT JOIN enrichment_completion_captures c ON c.completion_uuid=e.uuid ORDER BY e.uuid,c.capture_uuid`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var current *enrichmentCompletionRow
	var captures []string
	flush := func() error {
		if current == nil {
			return nil
		}
		_, err := current.resolve(captures)
		return err
	}
	for rows.Next() {
		var row struct {
			enrichmentCompletionRow
			CaptureUUID *string `db:"capture_uuid"`
		}
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		if current == nil || current.UUID != row.UUID {
			if err := flush(); err != nil {
				return err
			}
			current = &row.enrichmentCompletionRow
			captures = nil
		}
		if row.CaptureUUID != nil {
			captures = append(captures, *row.CaptureUUID)
			if len(captures) > archive.MaxEnrichmentCaptures {
				return models.ErrSourcePayloadCorrupt
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return flush()
}
