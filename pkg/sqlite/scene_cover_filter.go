package sqlite

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

func sceneCoverFrameCriterionHandler(value interface{}) (criterionHandler, error) {
	input, err := decodeASTValue[models.SceneCoverFrameCriterionInput](value)
	if err != nil {
		return nil, err
	}
	if !input.Value.IsValid() {
		return nil, fmt.Errorf("invalid scene cover frame %q", input.Value)
	}
	if input.Modifier != models.CriterionModifierEquals && input.Modifier != models.CriterionModifierNotEquals {
		return nil, fmt.Errorf("unsupported scene cover frame modifier %q", input.Modifier)
	}

	// Correlated predicates avoid multiplying rows or restricting an OR branch
	// through shared file joins. Match the checksum even before reconciliation
	// so an upstream/uploaded replacement cannot inherit the old selection.
	known := `SELECT 1 FROM fork_scene_cover_sources AS cover_source
WHERE cover_source.scene_id = scenes.id AND cover_source.cover_checksum = scenes.cover_blob`
	defaultFrame := `EXISTS (SELECT 1 FROM scenes_files AS cover_file
JOIN video_files AS cover_video ON cover_video.file_id = cover_file.file_id
WHERE cover_file.scene_id = scenes.id AND cover_file."primary" = 1
  AND cover_file.file_id = cover_source.source_file_id AND cover_video.duration > 0
  AND ABS(cover_source.at - cover_video.duration * ?) <= ?)`
	var predicate string
	var args []interface{}
	switch input.Value {
	case models.SceneCoverFrameDefault, models.SceneCoverFrameSpecific:
		if input.Value == models.SceneCoverFrameSpecific {
			defaultFrame = "NOT " + defaultFrame
		}
		predicate = "EXISTS (" + known + " AND " + defaultFrame + ")"
		// Only absorb floating point round-off, not nearby, intentionally
		// selected video frames (including a selection at exactly zero).
		args = []interface{}{models.DefaultSceneCoverFraction, 0.000001}
	case models.SceneCoverFrameUnknown:
		predicate = "NOT EXISTS (" + known + ")"
	}
	if input.Modifier == models.CriterionModifierNotEquals {
		predicate = "NOT (" + predicate + ")"
	}
	return criterionHandlerFunc(func(_ context.Context, f *filterBuilder) {
		f.addWhere(predicate, args...)
	}), nil
}
