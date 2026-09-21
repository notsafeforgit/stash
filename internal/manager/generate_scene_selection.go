package manager

import (
	"context"
	"fmt"
	"strconv"

	"github.com/stashapp/stash/pkg/models"
)

func (s *Manager) resolveGenerateSceneSelection(ctx context.Context, input GenerateMetadataInput) (GenerateMetadataInput, error) {
	selection := input.SceneSelection
	if selection == nil {
		return input, nil
	}
	if !s.Config.GetEnableV3UI() {
		return input, fmt.Errorf("generating scenes matching filters requires v3")
	}
	if len(input.SceneIDs)+len(input.ImageIDs)+len(input.GalleryIDs)+len(input.MarkerIDs)+len(input.Paths) != 0 {
		return input, fmt.Errorf("scene selection cannot be combined with paths or explicit entity IDs")
	}
	if selection.SceneFilterAST != nil {
		if err := selection.SceneFilterAST.Validate(); err != nil {
			return input, err
		}
	}
	var find models.FindFilterType
	if selection.FindFilter != nil {
		find = *selection.FindFilter
	}
	all := models.PerPageAll
	find.Page, find.PerPage = nil, &all
	// Resolve once, before queueing: resetting covers changes the filter
	// itself, so querying subsequent pages during generation would skip rows.
	err := s.Repository.WithReadTxn(ctx, func(ctx context.Context) error {
		scenes, _, err := s.Repository.Scene.QueryAST(ctx, selection.SceneFilterAST, &find)
		if err != nil {
			return err
		}
		if len(scenes) == 0 {
			// An empty legacy ID list means the entire library. Never allow a
			// stale or empty filtered selection to fall through to that scope.
			return fmt.Errorf("no scenes match the selected filters")
		}
		input.SceneIDs = make([]string, len(scenes))
		for i, scene := range scenes {
			input.SceneIDs[i] = strconv.Itoa(scene.ID)
		}
		return nil
	})
	if err != nil {
		return input, err
	}
	input.SceneSelection = nil
	return input, nil
}
