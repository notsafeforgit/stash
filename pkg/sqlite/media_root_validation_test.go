package sqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestMediaRootInvalidInputLeavesNoIdentity(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	dir := t.TempDir()
	bound, err := archive.ProbeMediaRoot(dir)
	require.NoError(t, err)
	for _, input := range []models.MediaRootInput{
		{ExpectedRevision: -1, Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Root", State: "active"}},
		{UUID: "bad", Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Root", State: "active"}},
		{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "", State: "active"}},
		{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Root", State: "unknown"}},
		{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Root", State: "active", Binding: &models.MediaRootBinding{Path: "relative", DirectoryIdentity: bound.DirectoryIdentity}}},
		{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Root", State: "active", Binding: &models.MediaRootBinding{Path: dir + string(os.PathSeparator) + ".", DirectoryIdentity: bound.DirectoryIdentity}}},
		{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Root", State: "active", Binding: &models.MediaRootBinding{Path: dir, DirectoryIdentity: ""}}},
	} {
		require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
			_, err := repo.MediaRoot.Put(ctx, input)
			require.Error(t, err)
			// Even a caller that handles a validation error inside its transaction
			// must not accidentally commit a half-created identity.
			return nil
		}))
	}
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		rows, err := repo.MediaRoot.List(ctx, "", 100)
		require.NoError(t, err)
		require.Empty(t, rows)
		return nil
	}))
	input := models.MediaRootInput{UUID: uuid.NewString(), Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Portable root", State: "active"}}
	root := putMediaRoot(t, repo, input)
	require.Nil(t, root.Binding)
	input.ExpectedRevision, input.Binding = 1, bound
	root = putMediaRoot(t, repo, input)
	require.Equal(t, bound, root.Binding)
	input.ExpectedRevision, input.Binding = 2, nil
	root = putMediaRoot(t, repo, input)
	require.Nil(t, root.Binding)
	require.Equal(t, 3, root.Revision)
	input.ExpectedRevision = 3
	input.Binding = &models.MediaRootBinding{Path: filepath.Join(dir, "missing"), DirectoryIdentity: bound.DirectoryIdentity}
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.MediaRoot.Put(ctx, input)
		require.ErrorIs(t, err, models.ErrMediaRootBindingInvalid)
		return nil
	}))
}
