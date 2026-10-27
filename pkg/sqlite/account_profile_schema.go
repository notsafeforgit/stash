package sqlite

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sourceadapter"
)

var accountProfileObjects = []string{"source_account_profiles", "source_account_profiles_account", "source_account_profile_immutable", "account_profile_receipt", "account_observed_profile_urls", "account_observed_profile_urls_profile", "account_observed_profile_url_scope", "account_observed_profile_url_update"}

func init() {
	RegisterPreMigration(NativeSchemaBaseline+134, func(ctx context.Context, conn *sqlx.DB) error {
		for _, name := range accountProfileObjects {
			var exists bool
			if err := conn.GetContext(ctx, &exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
				return err
			}
			if exists {
				return fmt.Errorf("native account profile destination %s already exists", name)
			}
		}
		return nil
	})
}

func validateAccountProfileSchema(conn *sqlx.DB, audit bool) error {
	for _, name := range accountProfileObjects {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	if !audit {
		return nil
	}
	var invalid bool
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM source_account_profiles p
LEFT JOIN ingest_receipts r ON r.producer_uuid=p.producer_uuid AND r.event_uuid=p.event_uuid
WHERE r.kind IS NOT 'account.profile' OR r.collection_uuid!=p.collection_uuid OR r.collection_revision!=p.collection_revision OR json_extract(r.result,'$.profile_uuid') IS NOT p.uuid
OR coalesce(json_extract(r.result,'$.account_uuid'),'')!=coalesce(p.account_uuid,'') OR coalesce(json_extract(r.result,'$.review'),'')!=p.review
OR json_extract(r.result,'$.status') IS NOT 'recorded' OR json_extract(r.result,'$.media_ingested') IS NOT 0)
OR EXISTS(SELECT 1 FROM account_observed_profile_urls u LEFT JOIN source_account_profiles p ON p.uuid=u.profile_uuid WHERE p.account_uuid IS NOT u.account_uuid)
OR EXISTS(SELECT 1 FROM ingest_receipts r WHERE r.kind='account.profile' AND NOT EXISTS(SELECT 1 FROM source_account_profiles p WHERE p.producer_uuid=r.producer_uuid AND p.event_uuid=r.event_uuid))`); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	if err := validateAccountProfilePayloads(conn); err != nil {
		return err
	}
	rows, err := conn.Queryx("SELECT url_key,url FROM account_observed_profile_urls")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err = rows.Scan(&key, &value); err != nil {
			return err
		}
		if key == "" || sourceadapter.ProfileURLKey(value) != key {
			return models.ErrSourcePayloadCorrupt
		}
	}
	return rows.Err()
}

func validateAccountProfilePayloads(conn *sqlx.DB) error {
	rows, err := conn.Queryx("SELECT source FROM source_account_profiles")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		if _, err = sourceadapter.ExtractAccountProfile(raw); err != nil {
			return models.ErrSourcePayloadCorrupt
		}
	}
	return rows.Err()
}
