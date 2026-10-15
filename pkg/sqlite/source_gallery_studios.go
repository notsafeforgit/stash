package sqlite

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/stashapp/stash/pkg/models"
)

const sourceGalleryStudioReason = "Inherited from the source posts when they identify one studio"

func (s *SourceGalleryStore) ReconcileStudios(ctx context.Context, ids []string) ([]models.SourceGalleryAttributionResult, error) {
	return reconcileSourceGalleryAttribution(ctx, ids, syncSourceGalleryStudios)
}

func flushSourceGalleryStudios(ctx context.Context) error {
	return flushSourceGalleryAttribution(ctx, "source_gallery_studio_pending", syncSourceGalleryStudios)
}

func syncSourceGalleryStudios(ctx context.Context, id int, entity string) (string, error) {
	var active bool
	if err := dbWrapper.Get(ctx, &active, `SELECT EXISTS(SELECT 1 FROM galleries g
JOIN archive_entities a ON a.gallery_id=g.id WHERE g.id=? AND g.origin='source' AND a.state='active')`, id); err != nil || !active {
		return "not_source", err
	}
	state, err := (&MetadataFieldStore{}).State(ctx, entity, "studio")
	if err != nil || state.Protected {
		return "protected", err
	}
	studios, err := (&SourceStudioStore{}).ForGallery(ctx, entity)
	if err != nil {
		return "", err
	}
	value := json.RawMessage("null")
	revisions := make(map[string]int)
	if len(studios) == 1 {
		studio, err := (&ArchiveEntityStore{}).Find(ctx, studios[0].UUID)
		if err != nil {
			return "", err
		}
		value, err = json.Marshal(studio.UUID)
		if err != nil {
			return "", err
		}
		revisions[studio.UUID] = studio.Revision
	}
	if bytes.Equal(value, state.Value) {
		return "unchanged", nil
	}
	def, err := metadataFieldDefinition(models.ArchiveGallery, "studio")
	if err != nil {
		return "", err
	}
	value, native, err := normalizeMetadataCollection(ctx, def, value, revisions)
	if err != nil {
		return "", err
	}
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO metadata_field_write_context(entity_uuid,field) VALUES(?,'studio')", entity); err != nil {
		return "", err
	}
	if err := writeMetadataCollection(ctx, state.Entity, "studio", native.(*metadataCollectionValue)); err != nil {
		return "", err
	}
	if err := insertMetadataDecision(ctx, entity, "studio", "inherit", "policy", value, nil, sourceGalleryStudioReason); err != nil {
		return "", err
	}
	_, err = dbWrapper.Exec(ctx, "DELETE FROM metadata_field_write_context WHERE entity_uuid=? AND field='studio'", entity)
	return "updated", err
}

// Studio deletion can clear ordinary relationships. It must not turn an
// inherited gallery into an explicit empty choice and prevent later inheritance.
func preserveSourceGalleryStudio(ctx context.Context, studio int, fn func() error) error {
	var galleries []string
	if err := dbWrapper.Select(ctx, &galleries, `SELECT a.uuid FROM galleries g
JOIN archive_entities a ON a.gallery_id=g.id
WHERE g.studio_id=? AND g.origin='source' AND a.state='active'
AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='studio')`, studio); err != nil {
		return err
	}
	var inherited []string
	for _, entity := range galleries {
		state, err := (&MetadataFieldStore{}).State(ctx, entity, "studio")
		if err != nil {
			return err
		}
		if state.Protected {
			continue
		}
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO metadata_field_write_context(entity_uuid,field) VALUES(?,'studio')", entity); err != nil {
			return err
		}
		inherited = append(inherited, entity)
	}
	if err := fn(); err != nil {
		return err
	}
	for _, entity := range inherited {
		if err := insertMetadataDecision(ctx, entity, "studio", "inherit", "policy", json.RawMessage("null"), nil, sourceGalleryStudioReason); err != nil {
			return err
		}
		if _, err := dbWrapper.Exec(ctx, "DELETE FROM metadata_field_write_context WHERE entity_uuid=? AND field='studio'", entity); err != nil {
			return err
		}
	}
	return nil
}
