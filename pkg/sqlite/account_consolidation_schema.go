package sqlite

import (
	"errors"
	"fmt"
	"github.com/jmoiron/sqlx"
)

func validateAccountConsolidationSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"source_account_consolidations", "source_account_consolidations_destination",
		"source_account_consolidation_context", "source_account_consolidation_scope", "source_account_consolidation_immutable",
		"source_account_namespace_immutable", "source_account_identifier_immutable", "source_account_evidence_immutable",
		"account_performer_active_root", "source_accounts_identity_pair", "source_accounts_canonical",
		"source_account_identifiers_canonical", "source_account_root_initial", "source_account_root_created",
		"source_account_root_update", "source_account_consolidation_publish"} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var unfinished bool
	if err := conn.Get(&unfinished, "SELECT EXISTS(SELECT 1 FROM source_account_consolidation_context)"); err != nil {
		return err
	}
	if unfinished {
		return errors.New("native database has unfinished source account consolidation")
	}
	if !auditData {
		return nil
	}
	if err := conn.Get(&unfinished, "SELECT EXISTS(SELECT 1 FROM source_accounts WHERE canonical_uuid IS NULL)"); err != nil {
		return err
	}
	if unfinished {
		return errors.New("native source account is missing its canonical identity")
	}
	if err := conn.Get(&unfinished, `SELECT EXISTS(SELECT 1 FROM source_accounts a
JOIN source_accounts r ON r.uuid=a.canonical_uuid
LEFT JOIN source_account_consolidations c ON c.source_uuid=a.uuid
LEFT JOIN source_accounts d ON d.uuid=c.destination_uuid
WHERE r.canonical_uuid!=r.uuid OR r.namespace!=a.namespace
 OR (a.canonical_uuid=a.uuid AND c.source_uuid IS NOT NULL)
 OR (a.canonical_uuid!=a.uuid AND (c.source_uuid IS NULL OR d.canonical_uuid!=a.canonical_uuid)))`); err != nil {
		return err
	}
	if unfinished {
		return errors.New("native source account canonical identity differs from consolidation history")
	}
	return nil
}
