package txn

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithTxnPanicRunsRollbackAndCompletionHooks(t *testing.T) {
	m := &testManager{}
	var hooks []string
	require.PanicsWithValue(t, "failure", func() {
		_ = WithTxn(context.Background(), m, func(ctx context.Context) error {
			AddPostCommitHook(ctx, func(context.Context) { hooks = append(hooks, "commit") })
			AddPostRollbackHook(ctx, func(context.Context) { hooks = append(hooks, "rollback") })
			AddPostCompleteHook(ctx, func(context.Context) { hooks = append(hooks, "complete") })
			panic("failure")
		})
	})
	require.Equal(t, []string{"rollback", "complete"}, hooks)
	require.Equal(t, 1, m.rollbacks)
	require.Zero(t, m.commits)
}
