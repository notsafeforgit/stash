package api

import (
	"context"
	"fmt"
	"slices"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sliceutil/stringslice"
)

func (r *queryResolver) BulkCustomFieldSummary(ctx context.Context, input BulkCustomFieldTargetInput) (*BulkCustomFieldSummary, error) {
	if input.Ids != nil {
		if input.FindFilter != nil || input.FilterAst != nil {
			return nil, fmt.Errorf("ids cannot be combined with filters")
		}
	} else if !hasBulkUpdateFilter(input.FindFilter, input.FilterAst) {
		return nil, fmt.Errorf("ids, filter_ast or find_filter.q is required")
	}

	var ret *BulkCustomFieldSummary
	err := r.withReadTxn(ctx, func(ctx context.Context) error {
		var err error
		switch input.Mode {
		case models.FilterModeScenes:
			ret, err = summarizeBulkCustomFields(ctx, input, r.repository.Scene, func(v *models.Scene) int { return v.ID })
		case models.FilterModeImages:
			ret, err = summarizeBulkCustomFields(ctx, input, r.repository.Image, func(v *models.Image) int { return v.ID })
		case models.FilterModeGalleries:
			ret, err = summarizeBulkCustomFields(ctx, input, r.repository.Gallery, func(v *models.Gallery) int { return v.ID })
		case models.FilterModePerformers:
			ret, err = summarizeBulkCustomFields(ctx, input, r.repository.Performer, func(v *models.Performer) int { return v.ID })
		case models.FilterModeStudios:
			ret, err = summarizeBulkCustomFields(ctx, input, r.repository.Studio, func(v *models.Studio) int { return v.ID })
		case models.FilterModeGroups:
			ret, err = summarizeBulkCustomFields(ctx, input, r.repository.Group, func(v *models.Group) int { return v.ID })
		case models.FilterModeTags:
			ret, err = summarizeBulkCustomFields(ctx, input, r.repository.Tag, func(v *models.Tag) int { return v.ID })
		default:
			err = fmt.Errorf("custom fields are not supported for filter mode %s", input.Mode)
		}
		return err
	})
	return ret, err
}

type bulkCustomFieldReader[T any] interface {
	models.CustomFieldsReader
	FindMany(context.Context, []int) ([]*T, error)
	QueryAST(context.Context, *models.FilterAST, *models.FindFilterType) ([]*T, int, error)
}

func summarizeBulkCustomFields[T any](ctx context.Context, input BulkCustomFieldTargetInput, reader bulkCustomFieldReader[T], getID func(*T) int) (*BulkCustomFieldSummary, error) {
	ids, err := stringslice.StringSliceToIntSlice(input.Ids)
	if err != nil {
		return nil, fmt.Errorf("converting ids: %w", err)
	}
	if input.Ids == nil {
		items, _, err := reader.QueryAST(ctx, input.FilterAst, sanitizeBulkUpdateFindFilter(input.FindFilter))
		if err != nil {
			return nil, err
		}
		ids = idsFromItems(items, getID)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)

	ret := &BulkCustomFieldSummary{Count: len(ids), SharedNames: []string{}, PartialNames: []string{}}
	counts := make(map[string]int)
	// Bound custom-field memory and SQL parameters even for large filtered sets.
	for batch := range slices.Chunk(ids, 500) {
		if input.Ids != nil {
			items, err := reader.FindMany(ctx, batch)
			if err != nil {
				return nil, err
			}
			if len(idsFromItems(items, getID)) != len(batch) {
				return nil, fmt.Errorf("some selected items no longer exist; refresh the list")
			}
		}
		fields, err := reader.GetCustomFieldsBulk(ctx, batch)
		if err != nil {
			return nil, err
		}
		for _, item := range fields {
			for name := range item {
				counts[name]++
			}
		}
	}
	for name, count := range counts {
		if count == ret.Count {
			ret.SharedNames = append(ret.SharedNames, name)
		} else {
			ret.PartialNames = append(ret.PartialNames, name)
		}
	}
	slices.Sort(ret.SharedNames)
	slices.Sort(ret.PartialNames)
	return ret, nil
}
