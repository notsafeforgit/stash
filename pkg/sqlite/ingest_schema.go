package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateIngestSchema(conn *sqlx.DB) error {
	for _, name := range []string{"ingest_producers", "ingest_producer_immutable", "ingest_credentials", "ingest_credentials_producer", "ingest_credential_immutable", "ingest_credential_scopes", "ingest_scope_immutable", "ingest_receipts", "ingest_receipts_capture", "ingest_receipt_scope", "ingest_receipt_immutable"} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	return nil
}

func validateFileIngestSchema(conn *sqlx.DB) error {
	var column, index bool
	if err := conn.Get(&column, "SELECT EXISTS(SELECT 1 FROM pragma_table_info('ingest_receipts') WHERE name='job_uuid')"); err != nil {
		return err
	}
	if err := conn.Get(&index, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name='ingest_receipts_job' AND type='index')"); err != nil {
		return err
	}
	if !column || !index {
		return fmt.Errorf("native database schema is incomplete: missing file ingestion receipt job association")
	}
	return nil
}
