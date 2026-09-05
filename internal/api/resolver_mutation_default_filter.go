package api

import (
	"context"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/pkg/models"
)

func (r *mutationResolver) ConfigureDefaultFilter(ctx context.Context, input ConfigureDefaultFilterInput) (map[string]interface{}, error) {
	var filter *models.SavedFilter
	if f := input.Filter; f != nil {
		filter = &models.SavedFilter{
			Mode: f.Mode, FindFilter: f.FindFilter, FilterAST: f.FilterAst, UIOptions: convertMapJSONNumbers(f.UIOptions),
		}
	}
	return manager.GetInstance().ConfigureDefaultFilter(input.View, input.Action.String(), filter)
}
