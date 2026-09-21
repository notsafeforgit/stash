package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/hash/md5"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestSceneCoverFrameFilter(t *testing.T) {
	config.InitializeEmpty()
	database := sqlite.NewDatabase()
	database.SetBlobStoreOptions(sqlite.BlobStoreOptions{UseDatabase: true})
	path := filepath.Join(t.TempDir(), "cover-filter.sqlite")
	require.NoError(t, database.Open(path))
	t.Cleanup(func() { _ = database.Close() })
	r, ctx := database.Repository(), context.Background()
	ids := make(map[string]int)
	var defaultSource models.SceneCoverSource
	require.NoError(t, r.WithTxn(ctx, func(ctx context.Context) error {
		folder := &models.Folder{Path: "/cover-filter"}
		require.NoError(t, r.Folder.Create(ctx, folder))
		for _, test := range []struct {
			name   string
			at     float64
			source int
		}{
			{"default", 20, 0},
			{"specific", 12.345, 0},
			{"zero", 0, 0},
			{"near default", 20.0001, 0},
			{"secondary", 20, 1},
			{"detached", 20, 2},
			{"legacy", 0, -1},
			{"missing", 0, -1},
			{"replaced", 20, 0},
		} {
			var files []models.FileID
			for _, suffix := range []string{"primary", "secondary"} {
				file := &models.VideoFile{BaseFile: &models.BaseFile{Basename: test.name + suffix, ParentFolderID: folder.ID}, Duration: 100}
				require.NoError(t, r.File.Create(ctx, file))
				files = append(files, file.ID)
			}
			scene := models.NewScene()
			scene.Title = test.name
			require.NoError(t, r.Scene.Create(ctx, &scene, files))
			ids[test.name] = scene.ID
			if test.name == "missing" {
				continue
			}
			cover := []byte(test.name)
			require.NoError(t, r.Scene.UpdateCover(ctx, scene.ID, cover))
			if test.source >= 0 {
				fileID := models.FileID(999999)
				if test.source < len(files) {
					fileID = files[test.source]
				}
				source := models.SceneCoverSource{CoverChecksum: md5.FromBytes(cover), FileID: fileID, At: test.at,
					Fingerprint: models.SceneCoverFingerprint{Version: 1}}
				require.NoError(t, r.Scene.SetCoverSource(ctx, scene.ID, &source))
				if test.name == "default" {
					defaultSource = source
				}
			}
		}
		return nil
	}))
	// Simulate an upstream edit without running its provenance reconciler.
	raw := openRawDB(t, path)
	_, err := raw.Exec("UPDATE scenes SET cover_blob = NULL WHERE id = ?", ids["replaced"])
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	condition := func(value models.SceneCoverFrame, modifier models.CriterionModifier) *models.FilterASTNode {
		return &models.FilterASTNode{Condition: &models.FilterASTCondition{Field: "cover_frame",
			Value: models.SceneCoverFrameCriterionInput{Value: value, Modifier: modifier}}}
	}
	query := func(root *models.FilterASTNode, find *models.FindFilterType) ([]int, int, error) {
		var found []int
		var count int
		err := r.WithReadTxn(ctx, func(ctx context.Context) error {
			scenes, total, err := r.Scene.QueryAST(ctx, &models.FilterAST{Root: root}, find)
			count = total
			for _, scene := range scenes {
				found = append(found, scene.ID)
			}
			return err
		})
		return found, count, err
	}
	for _, test := range []struct {
		value models.SceneCoverFrame
		names []string
	}{
		{models.SceneCoverFrameDefault, []string{"default"}},
		{models.SceneCoverFrameSpecific, []string{"specific", "zero", "near default", "secondary", "detached"}},
		{models.SceneCoverFrameUnknown, []string{"legacy", "missing", "replaced"}},
	} {
		t.Run(string(test.value), func(t *testing.T) {
			var want, complement []int
			for name, id := range ids {
				matches := false
				for _, expected := range test.names {
					matches = matches || name == expected
				}
				if matches {
					want = append(want, id)
				} else {
					complement = append(complement, id)
				}
			}
			found, count, err := query(condition(test.value, models.CriterionModifierEquals), nil)
			require.NoError(t, err)
			require.ElementsMatch(t, want, found)
			require.Equal(t, len(want), count)
			found, count, err = query(condition(test.value, models.CriterionModifierNotEquals), nil)
			require.NoError(t, err)
			require.ElementsMatch(t, complement, found)
			require.Equal(t, len(complement), count)
		})
	}
	// Filters apply before pagination, and compose with OR/AND and text search.
	page, perPage, sort := 2, 1, "id"
	found, count, err := query(condition(models.SceneCoverFrameSpecific, models.CriterionModifierEquals), &models.FindFilterType{Page: &page, PerPage: &perPage, Sort: &sort})
	require.NoError(t, err)
	require.Equal(t, []int{ids["zero"]}, found)
	require.Equal(t, 5, count)
	union := &models.FilterASTNode{Group: &models.FilterASTGroup{Operator: models.FilterGroupOperatorOr, Children: []*models.FilterASTNode{
		condition(models.SceneCoverFrameDefault, models.CriterionModifierEquals), condition(models.SceneCoverFrameUnknown, models.CriterionModifierEquals),
	}}}
	found, count, err = query(union, nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []int{ids["default"], ids["legacy"], ids["missing"], ids["replaced"]}, found)
	require.Equal(t, 4, count)
	search := "legacy"
	found, count, err = query(union, &models.FindFilterType{Q: &search})
	require.NoError(t, err)
	require.Equal(t, []int{ids["legacy"]}, found)
	require.Equal(t, 1, count)
	intersection := &models.FilterASTNode{Group: &models.FilterASTGroup{Operator: models.FilterGroupOperatorAnd, Children: []*models.FilterASTNode{
		union, condition(models.SceneCoverFrameUnknown, models.CriterionModifierNotEquals),
	}}}
	found, count, err = query(intersection, nil)
	require.NoError(t, err)
	require.Equal(t, []int{ids["default"]}, found)
	require.Equal(t, 1, count)
	for _, invalid := range []interface{}{
		nil, map[string]interface{}{}, "DEFAULT",
		models.SceneCoverFrameCriterionInput{Value: "INVALID", Modifier: models.CriterionModifierEquals},
		models.SceneCoverFrameCriterionInput{Value: models.SceneCoverFrameSpecific, Modifier: models.CriterionModifierIncludes},
	} {
		_, _, err := query(&models.FilterASTNode{Condition: &models.FilterASTCondition{Field: "cover_frame", Value: invalid}}, nil)
		require.Error(t, err)
	}
	// The timestamp alone may change while pixels/checksum stay identical.
	// Cached counts and subsequent reads must still observe the new category.
	defaultSource.At = 0
	require.NoError(t, r.WithTxn(ctx, func(ctx context.Context) error {
		return r.Scene.SetCoverSource(ctx, ids["default"], &defaultSource)
	}))
	found, count, err = query(condition(models.SceneCoverFrameDefault, models.CriterionModifierEquals), nil)
	require.NoError(t, err)
	require.Empty(t, found)
	require.Zero(t, count)
}
