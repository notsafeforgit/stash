package api

import (
	"context"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
)

type bulkUpdater interface {
	BulkUpdate(ctx context.Context, description string, ids []int, operation manager.BulkUpdateOperation, hookType hook.TriggerEnum, input interface{}, inputFields []string) int
}

func (r *Resolver) enqueueBulkUpdate(ctx context.Context, description string, ids []int, operation manager.BulkUpdateOperation, hookType hook.TriggerEnum, input interface{}, inputFields []string) int {
	return r.bulkUpdater.BulkUpdate(ctx, description, ids, operation, hookType, input, inputFields)
}

func sanitizeBulkUpdateFindFilter(findFilter *models.FindFilterType) *models.FindFilterType {
	var sanitized models.FindFilterType
	if findFilter != nil {
		sanitized = *findFilter
	}
	sanitized.Page = nil
	perPageAll := models.PerPageAll
	sanitized.PerPage = &perPageAll

	return &sanitized
}

func hasBulkUpdateFilter(findFilter *models.FindFilterType, filterAST *models.FilterAST) bool {
	if filterAST != nil {
		return true
	}

	return findFilter != nil && findFilter.Q != nil && *findFilter.Q != ""
}

func idsFromItems[T any](items []*T, getID func(*T) int) []int {
	ids := make([]int, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}

		ids = append(ids, getID(item))
	}

	return ids
}
