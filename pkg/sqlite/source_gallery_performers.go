package sqlite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

const sourceGalleryPerformerReason = "Inherited from the gallery's images and scenes"

// Older source galleries may predate inheritance and never receive another
// membership edit. Repair them explicitly in bounded batches, without changing
// their members, treating publisher ownership as attribution, or pinning the
// derived performers as a manual selection.
func (s *SourceGalleryStore) ReconcilePerformers(ctx context.Context, ids []string) ([]models.SourceGalleryAttributionResult, error) {
	return reconcileSourceGalleryAttribution(ctx, ids, syncSourceGalleryPerformers)
}

func reconcileSourceGalleryAttribution(ctx context.Context, ids []string, sync func(context.Context, int, string) (string, error)) ([]models.SourceGalleryAttributionResult, error) {
	if len(ids) == 0 || len(ids) > 100 {
		return nil, errors.New("attribution reconciliation requires 1 to 100 galleries")
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if _, err := archiveUUID(id); err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, errors.New("duplicate gallery in attribution reconciliation")
		}
		seen[id] = true
	}
	ret := make([]models.SourceGalleryAttributionResult, 0, len(ids))
	for _, id := range ids {
		result := models.SourceGalleryAttributionResult{GalleryUUID: id, Status: "not_source"}
		entity, err := (&ArchiveEntityStore{}).Find(ctx, id)
		if err != nil {
			return nil, err
		}
		if entity != nil && entity.State == models.ArchiveEntityActive && entity.Kind == models.ArchiveGallery {
			result.Status, err = sync(ctx, *entity.LocalID, id)
			if err != nil {
				return nil, err
			}
		}
		ret = append(ret, result)
	}
	return ret, nil
}

// SQL triggers collect membership and performer edits, including cascades and
// bulk operations. Commit processes each affected gallery once; it never scans
// unrelated galleries. Publisher ownership alone is not performer attribution.
func flushSourceGalleryPerformers(ctx context.Context) error {
	return flushSourceGalleryAttribution(ctx, "source_gallery_performer_pending", syncSourceGalleryPerformers)
}

func flushSourceGalleryAttribution(ctx context.Context, table string, sync func(context.Context, int, string) (string, error)) error {
	for {
		var rows []struct {
			ID   int    `db:"gallery_id"`
			UUID string `db:"uuid"`
		}
		if err := dbWrapper.Select(ctx, &rows, `SELECT p.gallery_id,a.uuid
FROM `+table+` p JOIN archive_entities a ON a.gallery_id=p.gallery_id
ORDER BY p.gallery_id LIMIT 100`); err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			if _, err := sync(ctx, row.ID, row.UUID); err != nil {
				return fmt.Errorf("inheriting source gallery attribution: %w", err)
			}
			if _, err := dbWrapper.Exec(ctx, "DELETE FROM "+table+" WHERE gallery_id=?", row.ID); err != nil {
				return err
			}
		}
	}
}

func syncSourceGalleryPerformers(ctx context.Context, id int, entity string) (string, error) {
	var active bool
	if err := dbWrapper.Get(ctx, &active, `SELECT EXISTS(SELECT 1 FROM galleries g
JOIN archive_entities a ON a.gallery_id=g.id WHERE g.id=? AND g.origin='source' AND a.state='active')`, id); err != nil || !active {
		return "not_source", err
	}
	state, err := (&MetadataFieldStore{}).State(ctx, entity, "performers")
	if err != nil || state.Protected {
		return "protected", err
	}
	var targets []struct {
		UUID     string `db:"uuid"`
		Revision int    `db:"revision"`
	}
	if err := dbWrapper.Select(ctx, &targets, `SELECT a.uuid,a.revision FROM (
SELECT p.performer_id FROM galleries_images m JOIN performers_images p ON p.image_id=m.image_id WHERE m.gallery_id=?
UNION
SELECT p.performer_id FROM scenes_galleries m JOIN performers_scenes p ON p.scene_id=m.scene_id WHERE m.gallery_id=?
) p JOIN archive_entities a ON a.performer_id=p.performer_id AND a.state='active' ORDER BY a.uuid`, id, id); err != nil {
		return "", err
	}
	performers := make([]string, 0, len(targets))
	revisions := make(map[string]int, len(targets))
	for _, target := range targets {
		performers = append(performers, target.UUID)
		revisions[target.UUID] = target.Revision
	}
	value, err := json.Marshal(performers)
	if err != nil || bytes.Equal(value, state.Value) {
		return "unchanged", err
	}
	def, err := metadataFieldDefinition(models.ArchiveGallery, "performers")
	if err != nil {
		return "", err
	}
	value, native, err := normalizeMetadataCollection(ctx, def, value, revisions)
	if err != nil {
		return "", err
	}
	// This also runs at Database.Commit, after ordinary transaction hooks. Use
	// the metadata write context directly and finish it before returning.
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO metadata_field_write_context(entity_uuid,field) VALUES(?,'performers')", entity); err != nil {
		return "", err
	}
	if err := writeMetadataCollection(ctx, state.Entity, "performers", native.(*metadataCollectionValue)); err != nil {
		return "", err
	}
	if err := insertMetadataDecision(ctx, entity, "performers", "inherit", "policy", value, nil, sourceGalleryPerformerReason); err != nil {
		return "", err
	}
	_, err = dbWrapper.Exec(ctx, "DELETE FROM metadata_field_write_context WHERE entity_uuid=? AND field='performers'", entity)
	return "updated", err
}

// Removing or merging a performer updates relationship rows without making a
// new manual choice for every inherited gallery. Preserve inheritance through
// that maintenance; explicit gallery choices still follow the normal path.
func preserveSourceGalleryPerformers(ctx context.Context, performers []int, fn func() error) error {
	if len(performers) == 0 {
		return fn()
	}
	args := make([]interface{}, len(performers))
	for i, id := range performers {
		args[i] = id
	}
	var galleries []string
	if err := dbWrapper.Select(ctx, &galleries, `SELECT DISTINCT a.uuid
FROM performers_galleries p JOIN galleries g ON g.id=p.gallery_id AND g.origin='source'
JOIN archive_entities a ON a.gallery_id=g.id AND a.state='active'
WHERE p.performer_id IN `+getInBinding(len(args))+`
AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field='performers')`, args...); err != nil {
		return err
	}
	var inherited []string
	for _, entity := range galleries {
		state, err := (&MetadataFieldStore{}).State(ctx, entity, "performers")
		if err != nil {
			return err
		}
		if state.Protected {
			continue
		}
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO metadata_field_write_context(entity_uuid,field) VALUES(?,'performers')", entity); err != nil {
			return err
		}
		inherited = append(inherited, entity)
	}
	if err := fn(); err != nil {
		return err
	}
	for _, entity := range inherited {
		value, err := readMetadataCollection(ctx, entity, "performers")
		if err != nil {
			return err
		}
		if err := insertMetadataDecision(ctx, entity, "performers", "inherit", "policy", value, nil, sourceGalleryPerformerReason); err != nil {
			return err
		}
		if _, err := dbWrapper.Exec(ctx, "DELETE FROM metadata_field_write_context WHERE entity_uuid=? AND field='performers'", entity); err != nil {
			return err
		}
	}
	return nil
}
