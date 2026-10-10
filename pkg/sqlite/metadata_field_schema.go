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
			if !metadataCollectionDefinition(def) {
				required = append(required, "metadata_"+string(kind)+"_"+def.Name+"_library")
			}
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

const unfinishedMetadataCollectionsQuery = `SELECT EXISTS(SELECT 1 FROM metadata_field_pending)
OR EXISTS(SELECT 1 FROM metadata_field_decisions INDEXED BY metadata_field_decisions_unsealed WHERE sealed=0)`

func validateMetadataCollectionSchema(conn *sqlx.DB) error {
	var names []string
	if err := conn.Select(&names, "SELECT name FROM sqlite_schema WHERE name LIKE 'metadata_%'"); err != nil {
		return err
	}
	present := make(map[string]bool, len(names))
	for _, name := range names {
		present[name] = true
	}
	required := []string{"metadata_field_references", "metadata_field_pending", "metadata_collection_values",
		"metadata_field_references_target", "metadata_groups_scenes_owner", "metadata_field_decisions_unsealed", "metadata_reference_scope",
		"metadata_reference_immutable", "metadata_reference_seal", "metadata_reference_current", "metadata_reference_retiring",
		"metadata_head_sealed_insert", "metadata_head_sealed_update"}
	for _, kind := range []models.ArchiveEntityKind{models.ArchiveScene, models.ArchiveImage, models.ArchiveGallery} {
		for _, def := range models.MetadataFields(kind) {
			if !metadataCollectionDefinition(def) {
				continue
			}
			actions := []string{"insert", "delete", "update"}
			if def.Name == "studio" {
				actions = []string{"update"}
			}
			for _, action := range actions {
				required = append(required, "metadata_"+string(kind)+"_"+def.Name+"_"+action)
			}
		}
	}
	for _, name := range required {
		if !present[name] {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var unfinished bool
	if err := conn.Get(&unfinished, unfinishedMetadataCollectionsQuery); err != nil {
		return err
	}
	if unfinished {
		return errors.New("native database has unfinished metadata collection decisions")
	}
	return nil
}
