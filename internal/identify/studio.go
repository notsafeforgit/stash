package identify

import (
	"context"
	"strconv"

	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/stashbox"
)

func createMissingStudio(ctx context.Context, endpoint string, w models.StudioReaderWriter, s *models.ScrapedStudio, record stashbox.MetadataRecorder) (*int, error) {
	var err error
	// A failed enclosing identification must not leave a created parent's local
	// ID in the reusable remote result after SQLite rolls that creation back.
	local := *s
	s = &local
	if s.Parent != nil {
		parent := *s.Parent
		s.Parent = &parent
	}

	if s.Parent != nil {
		// Create-missing authorizes new studios. A matched parent is only a
		// relationship target; its existing metadata has not been selected for
		// refresh by the scene's field options.
		if s.Parent.StoredID == nil {
			// The parent needs to be created
			newParentStudio := s.Parent.ToStudio(endpoint, nil)
			parentImage, err := s.Parent.GetImage(ctx, nil)
			if err != nil {
				logger.Errorf("Failed to make parent studio from scraped studio %s: %s", s.Parent.Name, err.Error())
				return nil, err
			}

			// Create the studio
			err = w.Create(ctx, newParentStudio)
			if err != nil {
				return nil, err
			}

			// Update image table
			if len(parentImage) > 0 {
				if err := w.UpdateImage(ctx, newParentStudio.ID, parentImage); err != nil {
					return nil, err
				}
			}

			storedId := strconv.Itoa(newParentStudio.ID)
			s.Parent.StoredID = &storedId
			if err := record.Record(ctx, models.ArchiveStudio, newParentStudio.ID, endpoint, s.Parent.RemoteSiteID, stashbox.StudioCreateImportFields(s.Parent, newParentStudio, nil, len(parentImage) > 0)); err != nil {
				return nil, err
			}
		}
	}

	newStudio := s.ToStudio(endpoint, nil)
	studioImage, err := s.GetImage(ctx, nil)
	if err != nil {
		return nil, err
	}

	err = w.Create(ctx, newStudio)
	if err != nil {
		return nil, err
	}

	// Update image table
	if len(studioImage) > 0 {
		if err := w.UpdateImage(ctx, newStudio.ID, studioImage); err != nil {
			return nil, err
		}
	}
	if err := record.Record(ctx, models.ArchiveStudio, newStudio.ID, endpoint, s.RemoteSiteID, stashbox.StudioCreateImportFields(s, newStudio, nil, len(studioImage) > 0)); err != nil {
		return nil, err
	}

	return &newStudio.ID, nil
}
