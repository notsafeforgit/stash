package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateScanJournalSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"scan_journals", "scan_journals_source", "scan_journal_immutable", "scan_journal_records", "scan_journal_record_page", "scan_journal_table_page", "scan_journal_target", "scan_journal_record_immutable"} {
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
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM scan_journals j WHERE j.record_count!=(SELECT count(*) FROM scan_journal_records r WHERE r.journal_uuid=j.uuid)
OR j.record_count!=(SELECT coalesce(sum(value),0) FROM json_each(j.inventory,'$.retained_tables'))
OR EXISTS(SELECT 1 FROM json_each(j.inventory,'$.retained_tables') t WHERE t.value!=(SELECT count(*) FROM scan_journal_records r WHERE r.journal_uuid=j.uuid AND r.source_table=t.key)))`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has incomplete retained scan journal evidence")
	}
	return nil
}
