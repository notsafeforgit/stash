package sqlite

import (
	"context"
	"github.com/stashapp/stash/pkg/entityid"
	"github.com/stashapp/stash/pkg/models"
)

// Capture before deletion, in the same transaction, instead of resolving an
// already-deleted row by a potentially reused original_id after commit.
func retainDeletedLibraryIDs(ctx context.Context, table string, ids []int) error {
	if _, err := entityid.From(ctx); err != nil {
		return nil
	}
	kind := map[string]models.ArchiveEntityKind{
		"scenes": models.ArchiveScene, "images": models.ArchiveImage, "galleries": models.ArchiveGallery,
		"performers": models.ArchivePerformer, "studios": models.ArchiveStudio, "tags": models.ArchiveTag,
		"groups": models.ArchiveGroup, "files": models.ArchiveFile, "folders": models.ArchiveFolder,
		"scene_markers": models.ArchiveSceneMarker, "galleries_chapters": models.ArchiveGalleryChapter, "saved_filters": models.ArchiveSavedFilter,
	}[table]
	if kind == "" {
		return nil
	}
	for _, id := range ids {
		entity, err := (&ArchiveEntityStore{}).FindByLocalID(ctx, kind, id)
		if err != nil {
			return err
		}
		if entity != nil {
			entityid.Remember(ctx, kind, id, entity.UUID)
		}
	}
	return nil
}
