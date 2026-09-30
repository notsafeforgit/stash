package api

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stretchr/testify/require"
)

type failingPerformerMerge struct{ models.PerformerReaderWriter }

func (f failingPerformerMerge) Merge(context.Context, []int, int) error {
	return errors.New("injected merge failure")
}

func TestPerformerMergeNotificationPreservesDeletedIdentitiesAfterCommit(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(strconv.FormatBool(fail), func(t *testing.T) {
			repo := pluginNotificationRepository(t)
			dest, source := models.NewPerformer(), models.NewPerformer()
			dest.Name, source.Name = "Canonical", "Other account"
			dest.URLs = models.NewRelatedStrings([]string{"https://x.com/canonical"})
			source.URLs = models.NewRelatedStrings([]string{"https://reddit.com/user/other"})
			source.Aliases = models.NewRelatedPerformerAliases([]models.PerformerAlias{{Alias: "Old account"}})
			require.NoError(t, repo.WithTxn(testCtx, func(ctx context.Context) error {
				if err := repo.Performer.Create(ctx, &models.CreatePerformerInput{Performer: &dest}); err != nil {
					return err
				}
				return repo.Performer.Create(ctx, &models.CreatePerformerInput{Performer: &source})
			}))
			hooks := &notificationRecorder{}
			hooks.check = func(ctx context.Context, call notificationCall) {
				require.Equal(t, hook.PerformerMergePost, call.kind)
				require.Equal(t, dest.ID, call.id)
				require.NoError(t, repo.WithReadTxn(ctx, func(ctx context.Context) error {
					deleted, err := repo.Performer.Find(ctx, source.ID)
					require.NoError(t, err)
					require.Nil(t, deleted)
					return nil
				}))
			}
			if fail {
				repo.Performer = failingPerformerMerge{repo.Performer}
			}
			r := &mutationResolver{&Resolver{repository: repo, hookExecutor: hooks}}
			_, err := r.PerformerMerge(testCtx, PerformerMergeInput{
				Source: []string{strconv.Itoa(source.ID)}, Destination: strconv.Itoa(dest.ID),
			})
			if fail {
				require.ErrorContains(t, err, "injected merge failure")
				require.Empty(t, hooks.calls)
				require.NoError(t, repo.WithReadTxn(testCtx, func(ctx context.Context) error {
					unchanged, err := repo.Performer.Find(ctx, source.ID)
					require.NoError(t, err)
					require.Equal(t, "Other account", unchanged.Name)
					return nil
				}))
				return
			}
			require.NoError(t, err)
			require.Len(t, hooks.calls, 1)
			notification := hooks.calls[0].input.(PerformerMergeNotification)
			require.Equal(t, "Canonical", notification.Destination.Name)
			require.Contains(t, notification.Destination.AliasList, "Other account")
			require.Empty(t, notification.PreviousDestination.AliasList)
			require.Equal(t, []string{"https://x.com/canonical"}, notification.PreviousDestination.URLs)
			require.Len(t, notification.Sources, 1)
			require.Equal(t, strconv.Itoa(source.ID), notification.Sources[0].ID)
			require.Equal(t, []string{"Old account"}, notification.Sources[0].AliasList)
			require.Equal(t, []string{"https://reddit.com/user/other"}, notification.Sources[0].URLs)
		})
	}
}
