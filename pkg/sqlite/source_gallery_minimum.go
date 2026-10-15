package sqlite

import (
	"context"
	"errors"

	"github.com/stashapp/stash/pkg/models"
)

// PruneSmall removes only the derived gallery. In particular it does not make
// the post's album choice disabled: new local media can form an album later.
// The bounded, revision-checked application operation also repairs older data.
func (s *SourceGalleryStore) PruneSmall(ctx context.Context, inputs []models.SourceGalleryPruneInput) ([]models.SourceGalleryPruneResult, error) {
	if len(inputs) == 0 || len(inputs) > 100 {
		return nil, errors.New("gallery pruning requires 1 to 100 galleries")
	}
	seen := make(map[string]bool, len(inputs))
	for _, input := range inputs {
		if _, err := archiveUUID(input.GalleryUUID); err != nil || input.Revision < 1 || seen[input.GalleryUUID] {
			return nil, errors.New("invalid gallery pruning input")
		}
		seen[input.GalleryUUID] = true
	}
	ret := make([]models.SourceGalleryPruneResult, 0, len(inputs))
	for _, input := range inputs {
		status, err := pruneSmallSourceGallery(ctx, input.GalleryUUID, input.Revision)
		if err != nil {
			return nil, err
		}
		ret = append(ret, models.SourceGalleryPruneResult{GalleryUUID: input.GalleryUUID, Status: status})
	}
	return ret, nil
}

func pruneSmallSourceGallery(ctx context.Context, id string, revision int) (string, error) {
	entity, err := (&ArchiveEntityStore{}).Find(ctx, id)
	if err != nil {
		return "", err
	}
	if entity == nil || entity.State != models.ArchiveEntityActive || entity.Kind != models.ArchiveGallery {
		return "missing", nil
	}
	if revision != 0 && entity.Revision != revision {
		return "changed", nil
	}
	var count int
	if err := dbWrapper.Get(ctx, &count, `SELECT
(SELECT count(*) FROM galleries_images WHERE gallery_id=?)+
(SELECT count(*) FROM scenes_galleries WHERE gallery_id=?)`, *entity.LocalID, *entity.LocalID); err != nil {
		return "", err
	}
	if count >= 2 {
		return "retained", nil
	}
	var eligible bool
	if err := dbWrapper.Get(ctx, &eligible, `SELECT EXISTS(SELECT 1 FROM galleries g
WHERE g.id=? AND g.origin='source' AND g.folder_id IS NULL
AND NOT EXISTS(SELECT 1 FROM galleries_files f WHERE f.gallery_id=g.id)
AND NOT EXISTS(SELECT 1 FROM gallery_covers c WHERE c.gallery_id=g.id)
AND EXISTS(SELECT 1 FROM post_gallery_links l WHERE l.gallery_uuid=?)
AND NOT EXISTS(SELECT 1 FROM post_gallery_links l JOIN post_gallery_decisions d ON d.uuid=l.decision_uuid
 WHERE l.gallery_uuid=? AND d.origin!='source')
AND NOT EXISTS(SELECT 1 FROM gallery_membership_heads h JOIN gallery_membership_events e ON e.uuid=h.event_uuid
 WHERE h.gallery_uuid=? AND e.origin!='source'))`, *entity.LocalID, id, id, id); err != nil {
		return "", err
	}
	if !eligible {
		return "protected", nil
	}
	// Preserve immutable decisions, source posts, attachment choices and media.
	// Release just the automatic heads so a new gallery is eligible on arrival.
	if _, err := dbWrapper.Exec(ctx, `UPDATE source_posts SET revision=revision+1
WHERE uuid IN (SELECT post_uuid FROM post_gallery_links WHERE gallery_uuid=?)`, id); err != nil {
		return "", err
	}
	if _, err := dbWrapper.Exec(ctx, "DELETE FROM post_gallery_links WHERE gallery_uuid=?", id); err != nil {
		return "", err
	}
	if _, err := dbWrapper.Exec(ctx, "DELETE FROM galleries WHERE id=?", *entity.LocalID); err != nil {
		return "", err
	}
	return "removed", nil
}
