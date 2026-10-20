package bulkupdate

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestBulkSelectionPagesInStableOrderWithoutMutatingInput(t *testing.T) {
	page, size, sortBy := 99, 1, "random"
	find := &models.FindFilterType{Page: &page, PerPage: &size, Sort: &sortBy}
	calls := 0
	query := func(_ context.Context, _ *models.FilterAST, f *models.FindFilterType) ([]*models.Scene, int, error) {
		calls++
		require.Equal(t, "id", *f.Sort)
		require.Equal(t, models.SortDirectionEnumAsc, *f.Direction)
		var ret []*models.Scene
		for i := (*f.Page-1)*(*f.PerPage) + 1; i <= min(*f.Page*(*f.PerPage), 1001); i++ {
			ret = append(ret, &models.Scene{ID: i})
		}
		return ret, 1001, nil
	}
	selector := SelectAll(query, nil, find, func(s *models.Scene) int { return s.ID })
	ids, err := selector(t.Context())
	require.NoError(t, err)
	require.Len(t, ids, 1001)
	require.Equal(t, 1, ids[0])
	require.Equal(t, 1001, ids[1000])
	require.Equal(t, 3, calls)
	require.Equal(t, 99, *find.Page)
	require.Equal(t, 1, *find.PerPage)
	require.Equal(t, "random", *find.Sort)
}

func TestBulkSelectionRejectsUnboundedOrInconsistentSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name  string
		items []*models.Scene
		total int
	}{
		{"limit", nil, MaxTargets + 1},
		{"missing", nil, 1},
		{"nil", []*models.Scene{nil}, 1},
		{"duplicate", []*models.Scene{{ID: 1}, {ID: 1}}, 2},
		{"extra", []*models.Scene{{ID: 1}, {ID: 2}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := func(context.Context, *models.FilterAST, *models.FindFilterType) ([]*models.Scene, int, error) {
				return tc.items, tc.total, nil
			}
			_, err := SelectAll(query, nil, nil, func(s *models.Scene) int { return s.ID })(t.Context())
			require.Error(t, err)
		})
	}
}
