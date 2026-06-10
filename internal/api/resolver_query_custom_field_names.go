package api

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

func (r *queryResolver) CustomFieldNames(ctx context.Context, mode models.FilterMode) ([]string, error) {
	var ret []string

	if err := r.withReadTxn(ctx, func(ctx context.Context) error {
		reader, err := r.customFieldsReaderForMode(mode)
		if err != nil {
			return err
		}
		ret, err = reader.DistinctCustomFieldNames(ctx)
		return err
	}); err != nil {
		return nil, err
	}

	if ret == nil {
		ret = []string{}
	}
	return ret, nil
}

// customFieldsReaderForMode dispatches to the per-entity store. Modes that
// don't carry custom_fields (SCENE_MARKERS, MOVIES) return an error so the
// caller can surface a clear failure rather than silently returning empty.
func (r *queryResolver) customFieldsReaderForMode(mode models.FilterMode) (models.CustomFieldsReader, error) {
	switch mode {
	case models.FilterModeScenes:
		return r.repository.Scene, nil
	case models.FilterModePerformers:
		return r.repository.Performer, nil
	case models.FilterModeStudios:
		return r.repository.Studio, nil
	case models.FilterModeGalleries:
		return r.repository.Gallery, nil
	case models.FilterModeGroups:
		return r.repository.Group, nil
	case models.FilterModeTags:
		return r.repository.Tag, nil
	case models.FilterModeImages:
		return r.repository.Image, nil
	default:
		return nil, fmt.Errorf("custom fields are not supported for filter mode %s", mode)
	}
}
