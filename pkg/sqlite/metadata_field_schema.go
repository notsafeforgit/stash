package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
)

func validateMetadataFieldSchema(conn *sqlx.DB) error {
	var names []string
	if err := conn.Select(&names, "SELECT name FROM sqlite_schema WHERE name LIKE 'metadata_%'"); err != nil {
		return err
	}
	present := make(map[string]bool, len(names))
	for _, name := range names {
		present[name] = true
	}
	required := []string{"metadata_field_baselines", "metadata_field_decisions", "metadata_field_heads", "metadata_field_write_context",
		"metadata_field_decisions_entity", "metadata_field_decisions_capture", "metadata_field_decision_scope", "metadata_field_decision_immutable",
		"metadata_field_head_forward", "metadata_field_head_scope", "metadata_field_current"}
	for _, kind := range []models.ArchiveEntityKind{models.ArchiveScene, models.ArchiveImage, models.ArchiveGallery} {
		for _, def := range models.MetadataFields(kind) {
			required = append(required, "metadata_"+string(kind)+"_"+def.Name+"_library")
		}
	}
	for _, name := range required {
		if !present[name] {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var unfinished bool
	if err := conn.Get(&unfinished, "SELECT EXISTS(SELECT 1 FROM metadata_field_write_context)"); err != nil {
		return err
	}
	if unfinished {
		return errors.New("native database has an unfinished metadata field write context")
	}
	return nil
}
