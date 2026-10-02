package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
)

func validateSourceTranslationSchema(conn *sqlx.DB) error {
	for _, name := range []string{"source_translations", "source_translation_immutable", "source_translation_evidence", "source_translation_evidence_post",
		"source_translation_evidence_result", "source_translation_evidence_collection", "source_translation_evidence_immutable", "source_translation_evidence_active", "source_translation_evidence_revision"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var validTime bool
	if err := conn.Get(&validTime, "SELECT EXISTS(SELECT 1 FROM pragma_table_info('source_translation_evidence') WHERE name='recorded_at' AND upper(type)='DATETIME')"); err != nil {
		return err
	}
	if !validTime {
		return errors.New("native database schema is incomplete: invalid translation receipt time")
	}
	var invalid bool
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM source_translation_evidence e
 LEFT JOIN source_translations t ON t.uuid=e.translation_uuid LEFT JOIN source_posts p ON p.uuid=e.post_uuid
 LEFT JOIN source_collection_revisions c ON c.collection_uuid=e.collection_uuid AND c.revision=e.collection_revision
 WHERE t.uuid IS NULL OR p.uuid IS NULL OR (e.collection_uuid IS NOT NULL AND c.collection_uuid IS NULL))`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has invalid translation evidence scope")
	}
	if err := validateSourceTranslationResults(conn); err != nil {
		return err
	}
	rows, err := conn.Queryx("SELECT " + translationEvidenceColumns + " FROM source_translation_evidence e")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var evidence models.SourceTranslationEvidence
		if err := rows.StructScan(&evidence); err != nil {
			return err
		}
		if err := normalizeTranslationEvidence(&evidence); err != nil {
			return err
		}
	}
	return rows.Err()
}

func validateSourceTranslationResults(conn *sqlx.DB) error {
	rows, err := conn.Queryx("SELECT * FROM source_translations")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var result models.SourceTranslation
		if err := rows.StructScan(&result); err != nil {
			return err
		}
		if err := validateTranslation(&result); err != nil {
			return err
		}
	}
	return rows.Err()
}
