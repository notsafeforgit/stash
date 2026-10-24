package sqlite_test

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/group"
	"github.com/stashapp/stash/pkg/image"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/performer"
	"github.com/stashapp/stash/pkg/scene"
	"github.com/stretchr/testify/require"
)

func TestEntityCountsMatchIndividualQueries(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `
INSERT INTO studios(id, name, parent_id, created_at, updated_at) VALUES
 (1, 'Root', NULL, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
 (2, 'Child', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
 (3, 'Grandchild', 2, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
 (4, 'Empty', NULL, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
UPDATE scenes SET studio_id = CASE id WHEN 31 THEN 1 ELSE 3 END;
UPDATE images SET studio_id = 2, o_counter = 3;
INSERT INTO galleries(id, title, studio_id, created_at, updated_at) VALUES (51, 'Album', 3, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO groups(id, name, studio_id, created_at, updated_at) VALUES (61, 'Group', 2, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO groups_scenes(group_id, scene_id) VALUES (61, 31), (61, 32);
INSERT INTO performers_scenes(performer_id, scene_id) VALUES (72, 31), (71, 32);
INSERT INTO performers_images(performer_id, image_id) VALUES (71, 41), (72, 41);
INSERT INTO performers_galleries(performer_id, gallery_id) VALUES (71, 51), (72, 51);
INSERT INTO scenes_o_dates(scene_id, o_date) VALUES (31, CURRENT_TIMESTAMP), (32, CURRENT_TIMESTAMP);
INSERT INTO tags(id, name, created_at, updated_at) VALUES (1, 'Marker tag', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO scene_markers(scene_id, title, seconds, primary_tag_id, created_at, updated_at) VALUES (31, 'Marker', 1, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
`, nil)
		return err
	}))
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		var keys []models.EntityCountKey
		var want []int
		for _, id := range []int{71, 72, 999, 71} {
			for field, single := range map[models.EntityCountField]func() (int, error){
				models.EntityCountScenes:     func() (int, error) { return repo.Scene.CountByPerformerID(ctx, id) },
				models.EntityCountImages:     func() (int, error) { return image.CountByPerformerID(ctx, repo.Image, id) },
				models.EntityCountGalleries:  func() (int, error) { return gallery.CountByPerformerID(ctx, repo.Gallery, id) },
				models.EntityCountGroups:     func() (int, error) { return repo.Group.CountByPerformerID(ctx, id) },
				models.EntityCountPerformers: func() (int, error) { return performer.CountByAppearsWith(ctx, repo.Performer, id) },
				models.EntityCountO: func() (int, error) {
					s, err := repo.Scene.OCountByPerformerID(ctx, id)
					if err != nil {
						return 0, err
					}
					i, err := repo.Image.OCountByPerformerID(ctx, id)
					return s + i, err
				},
				models.EntityHasImage: func() (int, error) {
					v, err := repo.Performer.HasImage(ctx, id)
					if v {
						return 1, err
					}
					return 0, err
				},
			} {
				n, err := single()
				require.NoError(t, err)
				keys = append(keys, models.EntityCountKey{ID: id, Field: field})
				want = append(want, n)
			}
		}
		got, err := repo.Performer.CountMany(ctx, keys)
		require.NoError(t, err)
		require.Equal(t, want, got)
		keys, want = nil, nil
		for _, id := range []int{1, 2, 3, 4, 999, 1} {
			for _, depth := range []int{0, 1, 2, -1} {
				for field, single := range map[models.EntityCountField]func() (int, error){
					models.EntityCountScenes:     func() (int, error) { return scene.CountByStudioID(ctx, repo.Scene, id, &depth) },
					models.EntityCountImages:     func() (int, error) { return image.CountByStudioID(ctx, repo.Image, id, &depth) },
					models.EntityCountGalleries:  func() (int, error) { return gallery.CountByStudioID(ctx, repo.Gallery, id, &depth) },
					models.EntityCountGroups:     func() (int, error) { return group.CountByStudioID(ctx, repo.Group, id, &depth) },
					models.EntityCountPerformers: func() (int, error) { return performer.CountByStudioID(ctx, repo.Performer, id, &depth) },
					models.EntityCountMarkers:    func() (int, error) { return scene.MarkerCountByStudioID(ctx, repo.SceneMarker, id, &depth) },
					models.EntityCountO: func() (int, error) {
						s, err := repo.Scene.OCountByStudioID(ctx, id, depth)
						if err != nil {
							return 0, err
						}
						i, err := repo.Image.OCountByStudioID(ctx, id, depth)
						return s + i, err
					},
				} {
					n, err := single()
					require.NoError(t, err)
					keys = append(keys, models.EntityCountKey{ID: id, Depth: depth, Field: field})
					want = append(want, n)
				}
			}
		}
		got, err = repo.Studio.CountMany(ctx, keys)
		require.NoError(t, err)
		require.Equal(t, want, got)
		empty, err := repo.Studio.CountMany(ctx, nil)
		require.NoError(t, err)
		require.Empty(t, empty)
		performers, total, err := repo.Performer.QueryList(ctx, nil, models.ListQueryOptions{QueryOptions: models.QueryOptions{Count: true}, SkipItems: true})
		require.NoError(t, err)
		require.Nil(t, performers)
		require.Equal(t, 2, total)
		performers, total, err = repo.Performer.QueryList(ctx, nil, models.ListQueryOptions{})
		require.NoError(t, err)
		require.Len(t, performers, 2)
		require.Zero(t, total)
		studios, total, err := repo.Studio.QueryList(ctx, nil, models.ListQueryOptions{QueryOptions: models.QueryOptions{Count: true}, SkipItems: true})
		require.NoError(t, err)
		require.Nil(t, studios)
		require.Equal(t, 4, total)
		studios, total, err = repo.Studio.QueryList(ctx, nil, models.ListQueryOptions{})
		require.NoError(t, err)
		require.Len(t, studios, 4)
		require.Zero(t, total)
		return nil
	}))
}
