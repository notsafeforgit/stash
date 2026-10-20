package bulkupdate_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/bulkupdate"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/performer"
	"github.com/stashapp/stash/pkg/sqlite"
	_ "github.com/stashapp/stash/pkg/sqlite/migrations"
	"github.com/stretchr/testify/require"
)

func TestPerformerURLUpdateLoadsAndPreservesOtherURLs(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "bulk.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	r := db.Repository()
	p := models.NewPerformer()
	p.Name = "fixture"
	p.URLs = models.NewRelatedStrings([]string{"https://example.test/profile", "https://twitter.com/old", "https://instagram.com/retained"})
	require.NoError(t, r.WithTxn(t.Context(), func(ctx context.Context) error {
		require.NoError(t, r.Performer.Create(ctx, &models.CreatePerformerInput{Performer: &p}))
		op := bulkupdate.Performer{Repository: r.Performer, Changes: models.NewPerformerPartial(),
			URLs: performer.URLUpdate{Twitter: models.NewOptionalString("new")}}
		return op.Update(ctx, p.ID)
	}))
	require.NoError(t, r.WithReadTxn(t.Context(), func(ctx context.Context) error {
		urls, err := r.Performer.GetURLs(ctx, p.ID)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"https://example.test/profile", "https://twitter.com/new", "https://instagram.com/retained"}, urls)
		return nil
	}))
}
