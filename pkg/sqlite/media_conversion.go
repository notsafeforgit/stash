package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type MediaConversionStore struct{}

func (s *MediaConversionStore) Find(ctx context.Context, id string) (*models.MediaConversion, error) {
	if _, err := archiveUUID(id); err != nil {
		return nil, err
	}
	var row struct {
		models.MediaConversion
		Time Timestamp `db:"created_at"`
	}
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM media_conversions WHERE uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	row.CreatedAt = row.Time.Timestamp
	return &row.MediaConversion, nil
}

func (s *MediaConversionStore) ConvertImage(ctx context.Context, input models.ImageConversionInput) (*models.MediaConversion, error) {
	if !txn.HasHooks(ctx) {
		return nil, errors.New("media conversion requires a managed transaction")
	}
	for _, id := range []string{input.UUID, input.ImageUUID, input.SceneUUID, input.OriginalFileUUID, input.FileUUID} {
		if _, err := archiveUUID(id); err != nil {
			return nil, err
		}
	}
	// Publication replay is owned by the durable intake checkpoint. Reusing a
	// conversion UUID in another publication must never repeat or reinterpret it.
	if existing, err := s.Find(ctx, input.UUID); err != nil || existing != nil {
		if err == nil {
			err = models.ErrArchiveIdentityConflict
		}
		return nil, err
	}
	identities := &ArchiveEntityStore{}
	image, err := identities.Find(ctx, input.ImageUUID)
	if err != nil {
		return nil, err
	}
	scene, err := identities.Find(ctx, input.SceneUUID)
	if err != nil {
		return nil, err
	}
	if !currentConversionEntity(image, models.ArchiveImage, input.ExpectedImageRevision) ||
		!currentConversionEntity(scene, models.ArchiveScene, input.ExpectedSceneRevision) {
		return nil, models.ErrArchiveIdentityConflict
	}
	if err := conversionFileOwner(ctx, image, input.OriginalFileUUID, input.OriginalGeneration); err != nil {
		return nil, err
	}
	if err := conversionFileOwner(ctx, scene, input.FileUUID, input.Generation); err != nil {
		return nil, err
	}
	proof, err := (&FileContentStore{}).Current(ctx, input.FileUUID)
	if err != nil {
		return nil, err
	}
	if proof == nil || proof.Generation != input.Generation {
		return nil, models.ErrFileGenerationConflict
	}
	if err := flushMetadataPending(ctx); err != nil {
		return nil, err
	}
	fields := &MetadataFieldStore{}
	// An automatically converted GIF never overwrites another scene's edits.
	// The intake service also requires that it created this destination.
	for _, definition := range models.MetadataFields(models.ArchiveScene) {
		state, err := fields.State(ctx, scene.UUID, definition.Name)
		if err != nil {
			return nil, err
		}
		if state.Decision != nil || state.Protected || !bytes.Equal(state.Value, definition.ClearValue) {
			return nil, models.ErrMetadataFieldConflict
		}
	}
	var occupied bool
	if err := dbWrapper.Get(ctx, &occupied, `SELECT EXISTS(SELECT 1 FROM scenes_galleries WHERE scene_id=?)
 OR EXISTS(SELECT 1 FROM scenes_o_dates WHERE scene_id=?)
 OR EXISTS(SELECT 1 FROM scenes_view_dates WHERE scene_id=?)`, *scene.LocalID, *scene.LocalID, *scene.LocalID); err != nil {
		return nil, err
	}
	if occupied {
		return nil, models.ErrArchiveIdentityConflict
	}
	var galleryCount int
	if err := dbWrapper.Get(ctx, &galleryCount, "SELECT count(*) FROM galleries_images WHERE image_id=?", *image.LocalID); err != nil {
		return nil, err
	}
	if galleryCount > 100 {
		return nil, fmt.Errorf("image belongs to too many galleries for automatic conversion")
	}
	var old struct {
		Photographer string    `db:"photographer"`
		OCounter     int       `db:"o_counter"`
		CreatedAt    Timestamp `db:"created_at"`
	}
	if err := dbWrapper.Get(ctx, &old, "SELECT coalesce(photographer,'') AS photographer,o_counter,created_at FROM images WHERE id=?", *image.LocalID); err != nil {
		return nil, err
	}
	if old.OCounter < 0 || old.OCounter > 100000 {
		return nil, fmt.Errorf("image counter requires explicit conversion review")
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO media_conversions
 (uuid,image_uuid,scene_uuid,original_file_uuid,original_generation,file_uuid,generation,photographer,undated_o_count)
 VALUES(?,?,?,?,?,?,?,?,?)`, input.UUID, image.UUID, scene.UUID, input.OriginalFileUUID,
		input.OriginalGeneration, input.FileUUID, input.Generation, old.Photographer, old.OCounter); err != nil {
		return nil, err
	}
	for _, definition := range models.MetadataFields(models.ArchiveImage) {
		if definition.Name == "photographer" {
			continue // Retained as an image attribution, never relabeled director.
		}
		if err := copyConvertedMetadataField(ctx, image, scene, definition); err != nil {
			return nil, err
		}
	}
	if _, err := dbWrapper.Exec(ctx, "UPDATE scenes SET created_at=? WHERE id=?", old.CreatedAt, *scene.LocalID); err != nil {
		return nil, err
	}
	if old.OCounter > 0 {
		if _, err := dbWrapper.Exec(ctx, `WITH RECURSIVE numbers(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM numbers WHERE n<?)
 INSERT INTO scenes_o_dates(scene_id,o_date) SELECT ?,NULL FROM numbers`, old.OCounter, *scene.LocalID); err != nil {
			return nil, err
		}
	}
	// Insert first so the selected cover's retirement trigger can follow the
	// conversion. The conversion-specific trigger preserves membership intent.
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO scenes_galleries(scene_id,gallery_id)
 SELECT ?,gallery_id FROM galleries_images WHERE image_id=?`, *scene.LocalID, *image.LocalID); err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, `UPDATE archive_entities SET state='redirected',revision=revision+1,
 image_id=NULL,redirect_to=?,retired_at=CURRENT_TIMESTAMP WHERE uuid=? AND state='active'`, scene.UUID, image.UUID); err != nil {
		return nil, err
	}
	// A source choice is scoped to its original media identity. Copy its field
	// provenance only after that identity resolves to the converted scene, while
	// the copied heads still refer to the image's original decisions.
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO metadata_decision_post_media(decision_uuid,post_media_decision_uuid)
 SELECT target.decision_uuid,p.post_media_decision_uuid
 FROM metadata_field_heads source
 JOIN metadata_decision_post_media p ON p.decision_uuid=source.decision_uuid
 JOIN metadata_field_heads target ON target.entity_uuid=? AND target.field=source.field
 WHERE source.entity_uuid=?`, scene.UUID, image.UUID); err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, "DELETE FROM images WHERE id=?", *image.LocalID); err != nil {
		return nil, err
	}
	// The producer already removed the GIF. Deleting only its database entry
	// advances the ordinary persistent path-removal fence; no media is unlinked
	// from the filesystem by this conversion transaction.
	if _, err := dbWrapper.Exec(ctx, "DELETE FROM files WHERE id=(SELECT file_id FROM archive_entities WHERE uuid=?)", input.OriginalFileUUID); err != nil {
		return nil, err
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		return validateMediaConversion(ctx, input.UUID)
	})
	return s.Find(ctx, input.UUID)
}

func currentConversionEntity(entity *models.ArchiveEntity, kind models.ArchiveEntityKind, revision int) bool {
	return entity != nil && entity.Kind == kind && entity.State == models.ArchiveEntityActive && entity.LocalID != nil && entity.Revision == revision
}

func conversionFileOwner(ctx context.Context, media *models.ArchiveEntity, fileUUID string, generation int64) error {
	owners, err := (&FileContentStore{}).Owners(ctx, fileUUID)
	if err != nil {
		return err
	}
	if len(owners) != 1 || owners[0].UUID != media.UUID {
		return models.ErrArchiveIdentityConflict
	}
	table, column := "images_files", "image_id"
	if media.Kind == models.ArchiveScene {
		table, column = "scenes_files", "scene_id"
	}
	var valid bool
	err = dbWrapper.Get(ctx, &valid, `SELECT count(*)=1 AND min(f.generation)=? AND min(a.uuid)=?
 FROM `+table+` j JOIN files f ON f.id=j.file_id JOIN archive_entities a ON a.file_id=f.id
 WHERE j.`+column+`=?`, generation, fileUUID, *media.LocalID)
	if err == nil && !valid {
		err = models.ErrFileGenerationConflict
	}
	return err
}

func copyConvertedMetadataField(ctx context.Context, source, target *models.ArchiveEntity, def models.MetadataFieldDefinition) error {
	fields := &MetadataFieldStore{}
	state, err := fields.State(ctx, source.UUID, def.Name)
	if err != nil {
		return err
	}
	if state.Decision == nil && !state.Protected && bytes.Equal(state.Value, def.ClearValue) {
		return nil
	}
	refs := map[string]int{}
	for _, entity := range state.References {
		refs[entity.UUID] = entity.Revision
	}
	var native any
	if metadataCollectionDefinition(def) {
		_, native, err = normalizeMetadataCollection(ctx, def, state.Value, refs)
	} else {
		_, native, err = normalizeMetadataValue(def, state.Value)
	}
	if err != nil {
		return err
	}
	return withMetadataFieldWrite(ctx, target.UUID, def.Name, func() error {
		if collection, ok := native.(*metadataCollectionValue); ok {
			if err := writeMetadataCollection(ctx, target, def.Name, collection); err != nil {
				return err
			}
		} else {
			column := metadataFieldColumn(def.Name)
			query := "UPDATE scenes SET " + column + "=?,updated_at=CURRENT_TIMESTAMP WHERE id=?"
			args := []any{native, *target.LocalID}
			if def.Type == "date" {
				date, _ := native.(*models.Date)
				query = "UPDATE scenes SET " + column + "=?," + column + "_precision=?,updated_at=CURRENT_TIMESTAMP WHERE id=?"
				args = []any{NullDateFromDatePtr(date), datePrecisionFromDatePtr(date), *target.LocalID}
			}
			if _, err := dbWrapper.Exec(ctx, query, args...); err != nil {
				return err
			}
		}
		var capture *string
		reason := "Retained from converted image " + source.UUID
		if state.Decision != nil {
			capture = state.Decision.CaptureUUID
			reason = state.Decision.Reason
		}
		if err := insertMetadataDecision(ctx, target.UUID, def.Name, state.Mode, state.Origin, state.Value, capture, reason); err != nil {
			return err
		}
		if state.Decision != nil {
			if _, err := dbWrapper.Exec(ctx, `INSERT INTO metadata_decision_policies(decision_uuid,collection_uuid,revision)
 SELECT h.decision_uuid,p.collection_uuid,p.revision
 FROM metadata_decision_policies p JOIN metadata_field_heads h ON h.entity_uuid=? AND h.field=?
 WHERE p.decision_uuid=?`, target.UUID, def.Name, state.Decision.UUID); err != nil {
				return err
			}
		}
		return nil
	})
}

func validateMediaConversion(ctx context.Context, id string) error {
	var valid bool
	if err := dbWrapper.Get(ctx, &valid, `SELECT EXISTS(SELECT 1 FROM media_conversions c
 JOIN archive_entities i ON i.uuid=c.image_uuid JOIN archive_entities s ON s.uuid=c.scene_uuid
 JOIN archive_entities f ON f.uuid=c.original_file_uuid
 WHERE c.uuid=? AND i.kind='image' AND i.state='redirected' AND i.redirect_to=s.uuid AND s.kind='scene'
 AND f.kind='file' AND f.state='deleted'
 AND NOT EXISTS(SELECT 1 FROM images WHERE id=i.original_id))`, id); err != nil {
		return err
	}
	if !valid {
		return models.ErrArchiveIdentityConflict
	}
	return nil
}
