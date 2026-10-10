package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

var fileDeduplicationObjects = []string{"file_deduplications", "file_deduplications_kept", "file_deduplications_media", "file_deduplication_verified", "file_deduplication_immutable"}

func init() {
	RegisterPreMigration(NativeSchemaBaseline+95, func(ctx context.Context, conn *sqlx.DB) error {
		for _, name := range fileDeduplicationObjects {
			var exists bool
			if err := conn.GetContext(ctx, &exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
				return err
			}
			if exists {
				return fmt.Errorf("native deduplication destination %s already exists", name)
			}
		}
		return nil
	})
}

func validateFileDeduplicationSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range fileDeduplicationObjects {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	if !auditData {
		return nil
	}
	for after := ""; ; {
		var id string
		if err := conn.Get(&id, "SELECT coalesce(min(uuid),'') FROM file_deduplications WHERE uuid>?", after); err != nil || id == "" {
			return err
		}
		var row models.FileDeduplicationReceipt
		if err := conn.Get(&row, fileDeduplicationSelect+" WHERE uuid=?", id); err != nil {
			return err
		}
		if err := validateFileDeduplicationReceipt(conn, row); err != nil {
			return fmt.Errorf("file deduplication %s: %w", id, err)
		}
		after = id
	}
}

func validateFileDeduplicationReceipt(conn *sqlx.DB, row models.FileDeduplicationReceipt) error {
	var p archive.DeduplicationProof
	if err := json.Unmarshal(row.Proof, &p); err != nil {
		return models.ErrSourcePayloadCorrupt
	}
	signature, err := archive.DeduplicationSignature(row.Proof)
	state := p.State
	if err != nil || signature != row.Signature || p.Version != 1 || state.Kept == nil || state.Removed == nil || state.Owner == nil ||
		p.Keep == nil || p.Remove == nil || state.BlockedReason != "" ||
		state.Input.RootUUID != row.RootUUID || state.Root.UUID != row.RootUUID || state.Input.KeepPath != row.KeepPath || state.Input.RemovePath != row.RemovePath ||
		!archive.ValidRootRelativePath(row.KeepPath, false) || !archive.ValidRootRelativePath(row.RemovePath, false) ||
		state.Kept.Generation != row.KeptGeneration || state.Removed.Generation != row.RemovedGeneration ||
		state.Kept.Size <= 0 || state.Kept.Size != state.Removed.Size ||
		p.Keep.Snapshot.Size != state.Kept.Size || p.Remove.Snapshot.Size != state.Removed.Size {
		return models.ErrSourcePayloadCorrupt
	}
	// UUID adoption cascades references, while the signed original proof stays
	// intact. Later merges or deletions must not invalidate a completed receipt.
	for _, pair := range [][2]string{{state.Kept.UUID, row.KeptUUID}, {state.Removed.UUID, row.RemovedUUID}, {state.Owner.UUID, row.MediaUUID}} {
		seen := map[string]bool{}
		for expected := pair[0]; expected != pair[1]; {
			if seen[expected] || len(seen) >= 1024 {
				return models.ErrSourcePayloadCorrupt
			}
			seen[expected] = true
			var next sql.NullString
			if err := conn.Get(&next, "SELECT redirect_to FROM archive_entities WHERE uuid=?", expected); err != nil || !next.Valid {
				return models.ErrSourcePayloadCorrupt
			}
			expected = next.String
		}
	}
	var valid bool
	err = conn.Get(&valid, `SELECT EXISTS(SELECT 1 FROM file_content_versions k
 JOIN file_content_versions d ON d.content_uuid=k.content_uuid
 JOIN media_contents c ON c.uuid=k.content_uuid AND c.sha256=? AND c.size=?
 JOIN archive_entities m ON m.uuid=? AND m.kind IN ('scene','image')
 JOIN archive_entities e ON e.uuid=d.file_uuid AND e.kind='file' AND e.state='redirected'
 WHERE k.file_uuid=? AND k.generation=? AND d.file_uuid=? AND d.generation=?)`,
		row.SHA256, state.Kept.Size, row.MediaUUID, row.KeptUUID, row.KeptGeneration, row.RemovedUUID, row.RemovedGeneration)
	if err != nil {
		return err
	}
	if !valid {
		return models.ErrSourcePayloadCorrupt
	}
	return nil
}
