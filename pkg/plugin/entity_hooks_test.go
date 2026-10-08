package plugin

import (
	"context"
	"errors"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/mocks"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stretchr/testify/require"
)

func TestEntityUpdateHooksOnlyAfterCommit(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[rollback], func(t *testing.T) {
			hooks := &fileHooksRecorder{enabled: true}
			repo := WithEntityUpdateHooks(models.Repository{TxnManager: mocks.NewDatabase()}, hooks)
			err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
				fields := []string{"urls"}
				require.NoError(t, models.NotifyEntityUpdate(ctx, models.ArchivePerformer, 71, fields))
				fields[0] = "name"
				require.Empty(t, hooks.calls)
				if rollback {
					return errors.New("later capture write failed")
				}
				return nil
			})
			if rollback {
				require.Error(t, err)
				require.Empty(t, hooks.calls)
				return
			}
			require.NoError(t, err)
			require.Equal(t, []capturedFileHook{{id: 71, kind: hook.PerformerUpdatePost, fields: []string{"urls"}}}, hooks.calls)
		})
	}
}

func TestEntityUpdateHooksWithoutListenersAndUnmanagedTransactions(t *testing.T) {
	hooks := &fileHooksRecorder{}
	repo := WithEntityUpdateHooks(models.Repository{TxnManager: mocks.NewDatabase()}, hooks)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return models.NotifyEntityUpdate(ctx, models.ArchivePerformer, 71, []string{"urls"})
	}))
	require.Empty(t, hooks.calls)

	ctx, err := repo.TxnManager.Begin(t.Context(), true)
	require.NoError(t, err)
	require.ErrorContains(t, models.NotifyEntityUpdate(ctx, models.ArchivePerformer, 71, []string{"urls"}), "managed transaction")
	require.NoError(t, repo.TxnManager.Rollback(ctx))
	require.Empty(t, hooks.calls)
	// Offline tools without a plugin observer keep the domain behavior.
	require.NoError(t, models.NotifyEntityUpdate(t.Context(), models.ArchivePerformer, 71, []string{"urls"}))
}
