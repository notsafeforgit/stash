package txn

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type testManager struct {
	commitErr error
	commits   int
	rollbacks int
}

func (*testManager) Begin(ctx context.Context, _ bool) (context.Context, error) { return ctx, nil }
func (m *testManager) Commit(context.Context) error {
	m.commits++
	return m.commitErr
}
func (m *testManager) Rollback(context.Context) error { m.rollbacks++; return nil }
func (m *testManager) IsLocked(err error) bool        { return errors.Is(err, errLocked) }

var errLocked = errors.New("locked")

func TestWithTxnNotificationsRequireSuccessfulCommit(t *testing.T) {
	failure := errors.New("failed")
	for _, tt := range []struct {
		name      string
		bodyErr   error
		preErr    error
		commitErr error
		commits   int
	}{
		{name: "success", commits: 1},
		{name: "write failure", bodyErr: failure},
		{name: "pre-commit failure", preErr: failure},
		{name: "commit failure", commitErr: failure, commits: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := &testManager{commitErr: tt.commitErr}
			var notifications []string
			outer := context.Background()
			err := WithTxn(outer, m, func(ctx context.Context) error {
				AddPreCommitHook(ctx, func(context.Context) error { return tt.preErr })
				AddPostCommitHook(ctx, func(ctx context.Context) {
					require.Equal(t, outer, ctx)
					notifications = append(notifications, "commit")
				})
				AddPostRollbackHook(ctx, func(context.Context) { notifications = append(notifications, "rollback") })
				AddPostCompleteHook(ctx, func(context.Context) { notifications = append(notifications, "complete") })
				return tt.bodyErr
			})
			if tt.bodyErr != nil || tt.preErr != nil || tt.commitErr != nil {
				require.ErrorIs(t, err, failure)
				require.Equal(t, []string{"rollback", "complete"}, notifications)
				require.Equal(t, 1, m.rollbacks)
			} else {
				require.NoError(t, err)
				require.Equal(t, []string{"commit", "complete"}, notifications)
				require.Zero(t, m.rollbacks)
			}
			require.Equal(t, tt.commits, m.commits)
		})
	}
}

func TestRetryerRetriesFailedCommitWithoutNotifyingPlugins(t *testing.T) {
	m := &testManager{commitErr: errLocked}
	r := Retryer{Manager: m, Retries: 2, OnFail: func(context.Context, error, int) error {
		m.commitErr = nil
		return nil
	}}
	var notifications int
	err := r.WithTxn(context.Background(), func(ctx context.Context) error {
		AddPostCommitHook(ctx, func(context.Context) { notifications++ })
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, m.commits)
	require.Equal(t, 1, m.rollbacks)
	require.Equal(t, 1, notifications)
}
