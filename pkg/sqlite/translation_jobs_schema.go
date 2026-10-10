package sqlite

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
)

func validateTranslationJobSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"translation_job_targets", "translation_job_targets_job", "translation_targets_request_ready", "archive_jobs_translation_request", "translation_job_target_immutable", "translation_job_target_scope"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var definition string
	if err := conn.Get(&definition, "SELECT sql FROM sqlite_schema WHERE name='archive_jobs' AND type='table'"); err != nil {
		return err
	}
	if !strings.Contains(definition, "'text.translate'") {
		return errors.New("native database schema is incomplete: missing translation archive job kind")
	}
	var invalid bool
	if !auditData {
		return nil
	}
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM translation_job_targets b
 LEFT JOIN archive_jobs j ON j.uuid=b.job_uuid LEFT JOIN translation_targets t ON t.uuid=b.target_uuid
 LEFT JOIN translation_target_history h ON h.target_uuid=b.target_uuid AND h.revision=b.target_revision
 WHERE j.uuid IS NULL OR j.kind!='text.translate' OR h.revision IS NULL OR h.state!='pending'
 OR t.request_uuid IS NOT json_extract(j.arguments,'$.request_uuid')
 OR NOT EXISTS(SELECT 1 FROM json_each(j.arguments,'$.targets') x
 WHERE json_extract(x.value,'$.target_uuid')=b.target_uuid AND json_extract(x.value,'$.revision')=b.target_revision))
 OR EXISTS(SELECT 1 FROM archive_jobs j WHERE j.kind='text.translate'
 AND (json_array_length(j.arguments,'$.targets') IS NOT (SELECT count(*) FROM translation_job_targets b WHERE b.job_uuid=j.uuid)
 OR EXISTS(SELECT 1 FROM json_each(j.arguments,'$.targets') x WHERE NOT EXISTS(
 SELECT 1 FROM translation_job_targets b WHERE b.job_uuid=j.uuid AND b.target_uuid=json_extract(x.value,'$.target_uuid')
 AND b.target_revision=json_extract(x.value,'$.revision')))))`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has invalid translation job target bindings")
	}
	rows, err := conn.Queryx("SELECT * FROM archive_jobs WHERE kind='text.translate'")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row archiveJobRow
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		if _, err := archive.DecodeTranslationJob(row.resolve()); err != nil {
			return err
		}
	}
	return rows.Err()
}
