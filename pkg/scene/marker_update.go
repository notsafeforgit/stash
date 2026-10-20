package scene

import (
	"context"
	"fmt"
	"slices"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sliceutil"
)

// AdjustMarkerTags adjusts the SceneMarkerPartial to exclude the primary tag from tag updates.
func AdjustMarkerTags(ctx context.Context, r models.SceneMarkerReader, id int, partial *models.SceneMarkerPartial) error {
	if partial.TagIDs == nil && !partial.PrimaryTagID.Set {
		return nil
	}

	// exclude primary tag from tag updates
	var primaryTagID int
	if partial.PrimaryTagID.Set {
		primaryTagID = partial.PrimaryTagID.Value
	} else {
		existing, err := r.Find(ctx, id)
		if err != nil {
			return fmt.Errorf("finding existing primary tag id: %w", err)
		}

		if existing == nil {
			return fmt.Errorf("scene marker %d not found", id)
		}
		primaryTagID = existing.PrimaryTagID
	}

	existingTagIDs, err := r.GetTagIDs(ctx, id)
	if err != nil {
		return fmt.Errorf("getting existing tag ids: %w", err)
	}

	tagIDAttr := partial.TagIDs

	if tagIDAttr == nil {
		tagIDAttr = &models.UpdateIDs{
			IDs:  existingTagIDs,
			Mode: models.RelationshipUpdateModeSet,
		}
	}

	newTagIDs := tagIDAttr.Apply(existingTagIDs)
	// Remove primary tag from newTagIDs if present
	newTagIDs = sliceutil.Exclude(newTagIDs, []int{primaryTagID})

	if !slices.Equal(existingTagIDs, newTagIDs) {
		partial.TagIDs = &models.UpdateIDs{
			IDs:  newTagIDs,
			Mode: models.RelationshipUpdateModeSet,
		}
	} else {
		// no change to tags required
		partial.TagIDs = nil
	}

	return nil
}
