package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validatePostMediaBackfillSchema(conn *sqlx.DB, consolidated bool, auditData bool) error {
	for _, name := range []string{"post_media_backfills", "post_media_backfills_post", "post_media_backfill_immutable", "post_media_backfill_scope",
		"post_media_backfill_decisions", "post_media_backfill_decision_immutable", "post_media_backfill_decision_scope",
		"post_media_decision_evidence", "post_media_decision_evidence_source", "post_media_decision_evidence_post_file", "post_media_decision_evidence_match",
		"post_media_decision_evidence_immutable", "post_media_decision_evidence_scope"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var typed bool
	if err := conn.Get(&typed, `SELECT EXISTS(SELECT 1 FROM pragma_table_info('post_media_backfills') WHERE name='created_at' AND upper(type)='DATETIME')`); err != nil {
		return err
	}
	if !typed {
		return errors.New("native database schema is incomplete: invalid post media backfill timestamp")
	}
	postScope := `d.post_uuid!=e.post_uuid OR d.post_uuid!=p.post_uuid`
	if consolidated {
		postScope = `e.post_uuid!=p.post_uuid OR NOT EXISTS(SELECT 1 FROM source_post_identities chosen
JOIN source_post_identities original ON original.canonical_uuid=chosen.canonical_uuid
WHERE chosen.post_uuid=d.post_uuid AND original.post_uuid=e.post_uuid)`
	}
	var invalid bool
	if !auditData {
		return nil
	}
	if err := conn.Get(&invalid, `SELECT
EXISTS(SELECT 1 FROM post_media_backfills b LEFT JOIN source_posts p ON p.uuid=b.post_uuid
 WHERE p.uuid IS NULL OR b.post_revision>p.revision OR b.selected!=(SELECT count(*) FROM post_media_backfill_decisions d WHERE d.backfill_uuid=b.uuid))
OR EXISTS(SELECT 1 FROM post_media_backfill_decisions r
 LEFT JOIN post_media_backfills b ON b.uuid=r.backfill_uuid LEFT JOIN post_media_decisions d ON d.uuid=r.decision_uuid
 WHERE b.uuid IS NULL OR d.uuid IS NULL OR d.post_uuid!=b.post_uuid OR d.state!='linked' OR d.origin!='migration'
 OR d.post_revision<=b.post_revision OR d.post_revision>b.post_revision+b.selected
 OR NOT EXISTS(SELECT 1 FROM post_media_decision_evidence e WHERE e.decision_uuid=d.uuid))
OR EXISTS(SELECT 1 FROM post_media_decision_evidence l
 LEFT JOIN post_media_decisions d ON d.uuid=l.decision_uuid
 LEFT JOIN post_media_backfill_decisions b ON b.decision_uuid=d.uuid
 LEFT JOIN source_media_evidence e ON e.uuid=l.evidence_uuid
 LEFT JOIN source_post_file_evidence p ON p.uuid=l.post_file_uuid
 LEFT JOIN source_file_matches f ON f.uuid=l.match_uuid
 WHERE d.uuid IS NULL OR b.decision_uuid IS NULL OR e.uuid IS NULL OR p.uuid IS NULL OR f.uuid IS NULL
 OR (`+postScope+`) OR d.origin!='migration' OR d.state!='linked'
 OR e.attachment_uuid IS NOT NULL OR e.basis!='legacy' OR p.origin!='migration' OR p.basis!='catalog-appearance'
 OR p.uuid IS NOT json_extract(e.details,'$.source_post_file_evidence_uuid')
 OR f.uuid IS NOT json_extract(e.details,'$.source_file_match_uuid')
 OR f.observation_uuid!=p.observation_uuid OR f.file_uuid IS NOT e.file_uuid)`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has invalid historical post media backfills or proof references")
	}
	return nil
}
