package sqlite

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
)

var providerMetadataObjects = []string{"provider_metadata_imports", "provider_metadata_imports_entity", "provider_metadata_import_scope", "provider_metadata_import_immutable"}

func init() {
	RegisterPreMigration(NativeSchemaBaseline+96, func(ctx context.Context, conn *sqlx.DB) error {
		for _, name := range providerMetadataObjects {
			var exists bool
			if err := conn.GetContext(ctx, &exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
				return err
			}
			if exists {
				return fmt.Errorf("native provider metadata destination %s already exists", name)
			}
		}
		return nil
	})
}

func validateProviderMetadataSchema(conn *sqlx.DB) error {
	for _, name := range providerMetadataObjects {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM provider_metadata_imports p
 LEFT JOIN archive_entities e ON e.uuid=p.entity_uuid
 WHERE e.uuid IS NULL OR e.kind!=p.entity_kind OR e.revision<p.entity_revision)`); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	// Bounded pages allow identity lookups on the same read-only connection.
	for after := 0; ; {
		var rows []models.ProviderMetadataImport
		if err := conn.Select(&rows, "SELECT "+providerMetadataColumns+" FROM provider_metadata_imports WHERE sequence>? ORDER BY sequence LIMIT 100", after); err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			signature, err := providerMetadataSignature(row)
			if err != nil || signature != row.Signature {
				return models.ErrSourcePayloadCorrupt
			}
			seen := map[string]bool{}
			for id := row.OriginalEntityUUID; id != row.EntityUUID; {
				if seen[id] || len(seen) >= 1024 {
					return models.ErrSourcePayloadCorrupt
				}
				seen[id] = true
				if err := conn.Get(&id, "SELECT redirect_to FROM archive_entities WHERE uuid=? AND kind=?", id, row.EntityKind); err != nil {
					return models.ErrSourcePayloadCorrupt
				}
			}
			after = row.Sequence
		}
	}
}
