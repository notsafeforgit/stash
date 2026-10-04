package manager

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
)

type GenerateCoverTask struct {
	repository       models.Repository
	Scene            models.Scene
	ScreenshotAt     *float64
	Overwrite        bool
	ResetToDefault   bool
	onError          func(error)
	publicationGuard func(context.Context) error
}

func (t *GenerateCoverTask) GetDescription() string {
	return fmt.Sprintf("Generating cover for %s", t.Scene.GetTitle())
}

func (t *GenerateCoverTask) Start(ctx context.Context) {
	if err := t.generate(ctx); err != nil && ctx.Err() == nil {
		logger.Error(err)
		logErrorOutput(err)
		if t.onError != nil {
			t.onError(err)
		}
	}
}

// generate returns failures to callers that expose the task as a monitored job.
func (t *GenerateCoverTask) generate(ctx context.Context) error {
	return t.generateWithCoverSource(ctx)
}

// required returns true if the cover needs to be generated
// assumes in a transaction
func (t *GenerateCoverTask) required(ctx context.Context) bool {
	if t.Overwrite {
		return true
	}
	if t.Scene.Path == "" {
		return false
	}

	// if the scene has a cover, then we don't need to generate it
	hasCover, err := t.repository.Scene.HasCover(ctx, t.Scene.ID)
	if err != nil {
		logger.Errorf("Error getting cover: %v", err)
		return false
	}

	return !hasCover
}
