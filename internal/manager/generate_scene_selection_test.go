package manager

import (
	"context"
	"errors"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestGenerateSceneSelectionFreezesAllMatchingScenes(t *testing.T) {
	mgr, db := coverTestManager(t)
	page, perPage, search := 3, 1, "selected studio"
	ast := &models.FilterAST{Root: &models.FilterASTNode{Condition: &models.FilterASTCondition{Field: "cover_frame",
		Value: models.SceneCoverFrameCriterionInput{Value: models.SceneCoverFrameSpecific, Modifier: models.CriterionModifierEquals}}}}
	selection := &models.GenerateSceneSelectionInput{FindFilter: &models.FindFilterType{Q: &search, Page: &page, PerPage: &perPage}, SceneFilterAST: ast}
	db.Scene.On("QueryAST", mock.Anything, ast, mock.MatchedBy(func(find *models.FindFilterType) bool {
		return find.Page == nil && find.PerPage != nil && *find.PerPage == models.PerPageAll && find.Q != nil && *find.Q == search
	})).Return([]*models.Scene{{ID: 7}, {ID: 9}, {ID: 15}}, 3, nil).Once()
	input, err := mgr.resolveGenerateSceneSelection(context.Background(), GenerateMetadataInput{
		Covers: true, ResetCoversToDefault: true, SceneSelection: selection,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"7", "9", "15"}, input.SceneIDs)
	require.Nil(t, input.SceneSelection)
	require.True(t, input.Covers)
	require.True(t, input.ResetCoversToDefault)
	require.Equal(t, 3, *selection.FindFilter.Page, "do not mutate the caller's list filter")
	require.Equal(t, 1, *selection.FindFilter.PerPage)
	db.Scene.AssertExpectations(t)
}

func TestGenerateSceneSelectionCannotExpandAnEmptyOrInvalidTarget(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{"empty result", nil},
		{"query failure", errors.New("unsupported scene filter")},
	} {
		t.Run(test.name, func(t *testing.T) {
			mgr, db := coverTestManager(t)
			db.Scene.On("QueryAST", mock.Anything, (*models.FilterAST)(nil), mock.Anything).
				Return([]*models.Scene(nil), 0, test.err).Once()
			id, err := mgr.Generate(context.Background(), GenerateMetadataInput{Covers: true, ResetCoversToDefault: true, SceneSelection: &models.GenerateSceneSelectionInput{}})
			require.Error(t, err)
			require.Zero(t, id)
			db.Scene.AssertExpectations(t)
		})
	}
	for _, change := range []func(*GenerateMetadataInput){
		func(in *GenerateMetadataInput) { in.SceneIDs = []string{"1"} },
		func(in *GenerateMetadataInput) { in.ImageIDs = []string{"1"} },
		func(in *GenerateMetadataInput) { in.GalleryIDs = []string{"1"} },
		func(in *GenerateMetadataInput) { in.MarkerIDs = []string{"1"} },
		func(in *GenerateMetadataInput) { in.Paths = []string{"/library"} },
		func(in *GenerateMetadataInput) { in.SceneSelection.SceneFilterAST = &models.FilterAST{} },
	} {
		mgr, db := coverTestManager(t)
		input := GenerateMetadataInput{Covers: true, SceneSelection: &models.GenerateSceneSelectionInput{}}
		change(&input)
		_, err := mgr.resolveGenerateSceneSelection(context.Background(), input)
		require.Error(t, err)
		db.Scene.AssertNotCalled(t, "QueryAST", mock.Anything, mock.Anything, mock.Anything)
	}
}

func TestGenerateSceneSelectionIsOptIn(t *testing.T) {
	mgr, db := coverTestManager(t)
	mgr.Config.SetBool(config.EnableV3UI, false)
	legacy := GenerateMetadataInput{SceneIDs: []string{"1"}, Covers: true}
	input, err := mgr.resolveGenerateSceneSelection(context.Background(), legacy)
	require.NoError(t, err)
	require.Equal(t, legacy, input)
	_, err = mgr.resolveGenerateSceneSelection(context.Background(), GenerateMetadataInput{SceneSelection: &models.GenerateSceneSelectionInput{}})
	require.ErrorContains(t, err, "requires v3")
	db.Scene.AssertNotCalled(t, "QueryAST", mock.Anything, mock.Anything, mock.Anything)
}
