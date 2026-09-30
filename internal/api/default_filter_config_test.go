package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUISettingsCannotWriteNativeDefaultFilterState(t *testing.T) {
	r := &mutationResolver{}
	for _, key := range []string{"defaultFilters", "defaultFilters.scenes", "forkDefaultFilterState.scenes", "defaultFilterConflicts.scenes"} {
		t.Run(key, func(t *testing.T) {
			ctx := context.Background()
			_, err := r.ConfigureUISetting(ctx, key, nil)
			require.ErrorContains(t, err, "use configureDefaultFilter")
			_, err = r.ConfigureUI(ctx, map[string]interface{}{key: nil}, nil)
			require.ErrorContains(t, err, "use configureDefaultFilter")
			_, err = r.ConfigureUI(ctx, nil, map[string]interface{}{key: nil})
			require.ErrorContains(t, err, "use configureDefaultFilter")
		})
	}
}
