package bulkupdate

import (
	"context"
	"errors"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

const MaxTargets = 100000

// Selector runs only during admission, in the same writer transaction that
// freezes UUIDs and saves the command/targets. It is never serialized or replayed.
type Selector func(context.Context) ([]int, error)

// SelectAll bounds object loading while ignoring UI pagination and sort order.
// Stable ID ordering and a single admission transaction prevent rows from
// moving between pages; execution later uses only the frozen target list.
func SelectAll[T any](query func(context.Context, *models.FilterAST, *models.FindFilterType) ([]*T, int, error),
	filter *models.FilterAST, find *models.FindFilterType, id func(*T) int) Selector {
	saved := models.FindFilterType{}
	if find != nil {
		saved = *find
	}
	return func(ctx context.Context) ([]int, error) {
		const batchSize = 500
		page, perPage, sortBy, direction := 1, batchSize, "id", models.SortDirectionEnumAsc
		input := saved
		input.Page, input.PerPage, input.Sort, input.Direction = &page, &perPage, &sortBy, &direction
		var ret []int
		total := -1
		previousID := 0
		for {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			items, count, err := query(ctx, filter, &input)
			if err != nil {
				return nil, fmt.Errorf("querying bulk selection: %w", err)
			}
			if count > MaxTargets {
				return nil, fmt.Errorf("bulk selection exceeds %d items; narrow the filter", MaxTargets)
			}
			if count < 0 || (total != -1 && total != count) {
				return nil, errors.New("bulk selection changed while being captured")
			}
			total = count
			for _, item := range items {
				if item == nil || id(item) <= previousID {
					return nil, errors.New("bulk selection returned missing or unordered identities")
				}
				previousID = id(item)
				ret = append(ret, previousID)
			}
			if len(ret) == total {
				return ret, nil
			}
			if len(items) == 0 || len(ret) > total {
				return nil, errors.New("bulk selection returned an incomplete snapshot")
			}
			page++
		}
	}
}
