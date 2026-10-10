package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateScanActivationSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"scan_journal_activations", "scan_journal_activation_valid", "scan_journal_activation_immutable", "scan_journal_activation_jobs", "scan_journal_activation_job_page", "scan_journal_activation_job_valid", "scan_journal_activation_job_immutable"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	if !auditData {
		return nil
	}
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM scan_journal_activations a
WHERE json_array_length(a.plan,'$.job_uuids')!=(SELECT count(*) FROM scan_journal_activation_jobs j WHERE j.activation_uuid=a.uuid)
OR EXISTS(SELECT 1 FROM json_each(a.plan,'$.job_uuids') p WHERE NOT EXISTS(SELECT 1 FROM scan_journal_activation_jobs j WHERE j.activation_uuid=a.uuid AND j.record_uuid=p.value)))`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has incomplete scan activation bindings")
	}
	return nil
}
