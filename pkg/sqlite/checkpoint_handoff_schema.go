package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateCheckpointHandoffSchema(conn *sqlx.DB) error {
	for _, object := range []struct{ name, kind string }{
		{"checkpoint_handoffs", "table"}, {"checkpoint_handoffs_evidence", "index"},
		{"checkpoint_handoff_immutable", "trigger"}, {"checkpoint_handoff_scope", "trigger"},
	} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=? AND type=?)", object.name, object.kind); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", object.name)
		}
	}
	for after := ""; ; {
		var id string
		if err := conn.Get(&id, "SELECT coalesce(min(uuid),'') FROM checkpoint_handoffs WHERE uuid>?", after); err != nil || id == "" {
			return err
		}
		if _, _, err := readCheckpointHandoff(conn.Get, conn.Select, id); err != nil {
			return fmt.Errorf("checkpoint handoff %s: %w", id, err)
		}
		after = id
	}
}
