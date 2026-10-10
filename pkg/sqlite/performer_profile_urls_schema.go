package sqlite

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

var performerProfileURLObjects = []string{"account_profile_urls", "account_profile_urls_capture", "performer_profile_url_suppressions", "performer_profile_url_suppression_scope", "performer_profile_url_suppression_update"}

func init() {
	RegisterPreMigration(NativeSchemaBaseline+97, func(ctx context.Context, conn *sqlx.DB) error {
		for _, name := range performerProfileURLObjects {
			var exists bool
			if err := conn.GetContext(ctx, &exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
				return err
			}
			if exists {
				return fmt.Errorf("native profile URL destination %s already exists", name)
			}
		}
		return nil
	})
}

func validatePerformerProfileURLSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range performerProfileURLObjects {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	if !auditData {
		return nil
	}
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM performer_profile_url_suppressions s
LEFT JOIN archive_entities e ON e.uuid=s.performer_uuid WHERE e.uuid IS NULL OR e.kind!='performer')
OR EXISTS(SELECT 1 FROM account_profile_urls u LEFT JOIN source_accounts a ON a.uuid=u.account_uuid
LEFT JOIN source_captures c ON c.uuid=u.capture_uuid WHERE a.uuid IS NULL OR c.uuid IS NULL)`); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	rows, err := conn.Queryx(`SELECT url_key,url FROM account_profile_urls
UNION ALL SELECT url_key,url FROM performer_profile_url_suppressions`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return err
		}
		if key == "" || archive.ProfileURLKey(value) != key {
			return models.ErrSourcePayloadCorrupt
		}
	}
	return rows.Err()
}
