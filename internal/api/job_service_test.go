package api

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/job"
	"github.com/stretchr/testify/require"
)

func TestJobResolversUseTheirOwnService(t *testing.T) {
	first, second := job.NewManager(), job.NewManager()
	t.Cleanup(func() {
		require.NoError(t, first.Shutdown(context.Background()))
		require.NoError(t, second.Shutdown(context.Background()))
	})
	first.SetSync(true)
	id, err := first.Add(t.Context(), "only the first library", job.MakeJobExec(func(context.Context, *job.Progress) error { return nil }))
	require.NoError(t, err)
	query := queryResolver{&Resolver{jobs: first}}
	result, err := query.FindJob(t.Context(), FindJobInput{ID: id})
	require.NoError(t, err)
	require.Equal(t, id, result.ID)
	require.Equal(t, JobStatusFinished, result.Status)
	other := queryResolver{&Resolver{jobs: second}}
	result, err = other.FindJob(t.Context(), FindJobInput{ID: id})
	require.NoError(t, err)
	require.Nil(t, result, "a resolver must not find another library's job through a singleton")
}
