package api

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/stashapp/stash/internal/entityimage"
	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/utils"
)

func (r *mutationResolver) processEntityImageFields(ctx context.Context, compatImage *string, imageInput *models.EntityImageInput) ([]byte, bool, error) {
	if compatImage != nil && imageInput != nil {
		return nil, false, fmt.Errorf("image and image_input cannot both be set")
	}

	if compatImage != nil {
		imageData, err := r.processEntityImageInput(ctx, *compatImage, true)
		return imageData, true, err
	}

	return r.processEntityImageInputObject(ctx, imageInput)
}

func (r *mutationResolver) processEntityImageInputObject(ctx context.Context, input *models.EntityImageInput) ([]byte, bool, error) {
	if input == nil {
		return nil, false, nil
	}

	sources := 0
	for _, present := range []bool{input.Data != nil, input.ImageID != nil, input.Scene != nil} {
		if present {
			sources++
		}
	}
	if sources != 1 {
		return nil, false, fmt.Errorf("exactly one of data, image_id or scene must be set")
	}

	if input.Data != nil {
		if *input.Data == "" {
			return nil, true, nil
		}

		imageData, err := r.processEntityImageInput(ctx, *input.Data, false)
		return imageData, true, err
	}

	if input.ImageID != nil {
		imageData, err := r.entityImageDataFromImageID(ctx, *input.ImageID, true)
		return imageData, true, err
	}

	imageData, err := r.entityImageDataFromScene(ctx, *input.Scene)
	if err != nil {
		return nil, true, err
	}
	imageData, err = r.processEntityImageBytes(ctx, imageData, false)
	return imageData, true, err
}

func (r *mutationResolver) processEntityImageInput(ctx context.Context, imageInput string, allowHEIC bool) ([]byte, error) {
	imageData, err := utils.ProcessImageInput(ctx, imageInput)
	if err != nil {
		return nil, fmt.Errorf("processing image: %w", err)
	}

	return r.processEntityImageBytes(ctx, imageData, allowHEIC)
}

func (r *mutationResolver) entityImageDataFromImageID(ctx context.Context, imageID string, allowHEIC bool) ([]byte, error) {
	srcImageID, err := strconv.Atoi(imageID)
	if err != nil {
		return nil, fmt.Errorf("converting image_id: %w", err)
	}

	var imageData []byte
	if err := r.withReadTxn(ctx, func(ctx context.Context) error {
		img, err := r.repository.Image.Find(ctx, srcImageID)
		if err != nil {
			return fmt.Errorf("finding source image: %w", err)
		}
		if img == nil {
			return fmt.Errorf("source image %d not found", srcImageID)
		}

		if err := img.LoadPrimaryFile(ctx, r.repository.File); err != nil {
			return fmt.Errorf("loading source image file: %w", err)
		}

		f := img.Files.Primary()
		if f == nil {
			return fmt.Errorf("source image %d has no primary file", srcImageID)
		}

		rc, err := f.Base().Open(&file.OsFS{})
		if err != nil {
			return fmt.Errorf("opening source image file: %w", err)
		}
		defer rc.Close()

		imageData, err = io.ReadAll(rc)
		if err != nil {
			return fmt.Errorf("reading source image file: %w", err)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return r.processEntityImageBytes(ctx, imageData, allowHEIC)
}

func (r *mutationResolver) processEntityImageBytes(ctx context.Context, imageData []byte, allowHEIC bool) ([]byte, error) {
	return entityimage.Normalize(ctx, manager.GetInstance().FFMpeg, imageData, entityimage.NormalizeOptions{
		AllowHEIC: allowHEIC,
	})
}
