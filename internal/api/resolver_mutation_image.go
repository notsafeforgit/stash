package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/pkg/file"
	fileimage "github.com/stashapp/stash/pkg/file/image"
	"github.com/stashapp/stash/pkg/hash/md5"
	"github.com/stashapp/stash/pkg/image"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/sliceutil/stringslice"
	"github.com/stashapp/stash/pkg/utils"
)

// used to refetch image after hooks run
func (r *mutationResolver) getImage(ctx context.Context, id int) (ret *models.Image, err error) {
	if err := r.withTxn(ctx, func(ctx context.Context) error {
		ret, err = r.repository.Image.Find(ctx, id)
		return err
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

type imageBulkUpdateOperation struct {
	repository     models.ImageReaderWriter
	galleryService manager.GalleryService
	updatedImage   models.ImagePartial
}

func (o imageBulkUpdateOperation) Update(ctx context.Context, id int) error {
	i, err := o.repository.Find(ctx, id)
	if err != nil {
		return err
	}
	if i == nil {
		return fmt.Errorf("image with id %d not found", id)
	}

	if o.updatedImage.GalleryIDs != nil {
		if err := i.LoadGalleryIDs(ctx, o.repository); err != nil {
			return err
		}

		if err := o.galleryService.ValidateImageGalleryChange(ctx, i, *o.updatedImage.GalleryIDs); err != nil {
			return err
		}
	}

	if _, err := o.repository.UpdatePartial(ctx, id, o.updatedImage); err != nil {
		return err
	}

	if o.updatedImage.GalleryIDs != nil {
		thisUpdatedGalleryIDs := o.updatedImage.GalleryIDs.ImpactedIDs(i.GalleryIDs.List())
		for _, galleryID := range thisUpdatedGalleryIDs {
			if err := o.galleryService.Updated(ctx, galleryID); err != nil {
				return fmt.Errorf("updating gallery %d: %w", galleryID, err)
			}
		}
	}

	return nil
}

func (r *mutationResolver) ImageUpdate(ctx context.Context, input models.ImageUpdateInput) (ret *models.Image, err error) {
	translator := changesetTranslator{
		inputMap: getUpdateInputMap(ctx),
	}

	// Start the transaction and save the image
	if err := r.withTxn(ctx, func(ctx context.Context) error {
		ret, err = r.imageUpdate(ctx, input, translator)
		return err
	}); err != nil {
		return nil, err
	}

	// execute post hooks outside txn
	r.hookExecutor.ExecutePostHooks(ctx, ret.ID, hook.ImageUpdatePost, input, translator.getFields())
	return r.getImage(ctx, ret.ID)
}

func (r *mutationResolver) ImagesUpdate(ctx context.Context, input []*models.ImageUpdateInput) (ret []*models.Image, err error) {
	inputMaps := getUpdateInputMaps(ctx)

	// Start the transaction and save the image
	if err := r.withTxn(ctx, func(ctx context.Context) error {
		for i, image := range input {
			translator := changesetTranslator{
				inputMap: inputMaps[i],
			}

			thisImage, err := r.imageUpdate(ctx, *image, translator)
			if err != nil {
				return err
			}

			ret = append(ret, thisImage)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	// execute post hooks outside txn
	var newRet []*models.Image
	for i, image := range ret {
		translator := changesetTranslator{
			inputMap: inputMaps[i],
		}

		r.hookExecutor.ExecutePostHooks(ctx, image.ID, hook.ImageUpdatePost, input, translator.getFields())

		image, err = r.getImage(ctx, image.ID)
		if err != nil {
			return nil, err
		}

		newRet = append(newRet, image)
	}

	return newRet, nil
}

func (r *mutationResolver) imageUpdate(ctx context.Context, input models.ImageUpdateInput, translator changesetTranslator) (*models.Image, error) {
	imageID, err := strconv.Atoi(input.ID)
	if err != nil {
		return nil, fmt.Errorf("converting id: %w", err)
	}

	i, err := r.repository.Image.Find(ctx, imageID)
	if err != nil {
		return nil, err
	}

	if i == nil {
		return nil, fmt.Errorf("image with id %d not found", imageID)
	}

	// Populate image from the input
	updatedImage := models.NewImagePartial()

	updatedImage.Title = translator.optionalString(input.Title, "title")
	updatedImage.Code = translator.optionalString(input.Code, "code")
	updatedImage.Details = translator.optionalString(input.Details, "details")
	updatedImage.Photographer = translator.optionalString(input.Photographer, "photographer")
	updatedImage.Rating = translator.optionalInt(input.Rating100, "rating100")
	updatedImage.Organized = translator.optionalBool(input.Organized, "organized")

	updatedImage.Date, err = translator.optionalDate(input.Date, "date")
	if err != nil {
		return nil, fmt.Errorf("converting date: %w", err)
	}
	updatedImage.StudioID, err = translator.optionalIntFromString(input.StudioID, "studio_id")
	if err != nil {
		return nil, fmt.Errorf("converting studio id: %w", err)
	}

	updatedImage.URLs = translator.optionalURLs(input.Urls, input.URL)

	updatedImage.PrimaryFileID, err = translator.fileIDPtrFromString(input.PrimaryFileID)
	if err != nil {
		return nil, fmt.Errorf("converting primary file id: %w", err)
	}
	if updatedImage.PrimaryFileID != nil {
		primaryFileID := *updatedImage.PrimaryFileID

		if err := i.LoadFiles(ctx, r.repository.Image); err != nil {
			return nil, err
		}

		// ensure that new primary file is associated with image
		var f models.File
		for _, ff := range i.Files.List() {
			if ff.Base().ID == primaryFileID {
				f = ff
			}
		}

		if f == nil {
			return nil, fmt.Errorf("file with id %d not associated with image", primaryFileID)
		}
	}

	var updatedGalleryIDs []int

	updatedImage.GalleryIDs, err = translator.updateIds(input.GalleryIds, "gallery_ids")
	if err != nil {
		return nil, fmt.Errorf("converting gallery ids: %w", err)
	}
	if updatedImage.GalleryIDs != nil {
		// ensure gallery IDs are loaded
		if err := i.LoadGalleryIDs(ctx, r.repository.Image); err != nil {
			return nil, err
		}

		if err := r.galleryService.ValidateImageGalleryChange(ctx, i, *updatedImage.GalleryIDs); err != nil {
			return nil, err
		}

		updatedGalleryIDs = updatedImage.GalleryIDs.ImpactedIDs(i.GalleryIDs.List())
	}

	updatedImage.PerformerIDs, err = translator.updateIds(input.PerformerIds, "performer_ids")
	if err != nil {
		return nil, fmt.Errorf("converting performer ids: %w", err)
	}
	updatedImage.TagIDs, err = translator.updateIds(input.TagIds, "tag_ids")
	if err != nil {
		return nil, fmt.Errorf("converting tag ids: %w", err)
	}

	if input.CustomFields != nil {
		updatedImage.CustomFields = *input.CustomFields
		// convert json.Numbers to int/float
		updatedImage.CustomFields.Full = convertMapJSONNumbers(updatedImage.CustomFields.Full)
		updatedImage.CustomFields.Partial = convertMapJSONNumbers(updatedImage.CustomFields.Partial)
	}

	qb := r.repository.Image
	image, err := qb.UpdatePartial(ctx, imageID, updatedImage)
	if err != nil {
		return nil, err
	}

	// #3759 - update all impacted galleries
	for _, galleryID := range updatedGalleryIDs {
		if err := r.galleryService.Updated(ctx, galleryID); err != nil {
			return nil, fmt.Errorf("updating gallery %d: %w", galleryID, err)
		}
	}

	return image, nil
}

func (r *mutationResolver) BulkImageUpdate(ctx context.Context, input BulkImageUpdateInput) ([]*models.Image, error) {
	imageIDs, err := stringslice.StringSliceToIntSlice(input.Ids)
	if err != nil {
		return nil, fmt.Errorf("converting ids: %w", err)
	}

	compatInput := input
	compatInput.ApplyToItemsMatchingFilters = nil
	compatInput.FindFilter = nil
	compatInput.ImageFilterAst = nil

	if _, err := r.BulkImageUpdateJob(ctx, compatInput); err != nil {
		return nil, err
	}

	return refetchBulkUpdateResults(ctx, imageIDs, r.getImage)
}

func (r *mutationResolver) BulkImageUpdateJob(ctx context.Context, input BulkImageUpdateInput) (string, error) {
	imageIDs, err := stringslice.StringSliceToIntSlice(input.Ids)
	if err != nil {
		return "", fmt.Errorf("converting ids: %w", err)
	}

	useBackgroundJob := input.ApplyToItemsMatchingFilters != nil && *input.ApplyToItemsMatchingFilters
	if useBackgroundJob {
		if !hasBulkUpdateFilter(input.FindFilter, input.ImageFilterAst) {
			return "", fmt.Errorf("image_filter_ast or find_filter.q is required when apply_to_items_matching_filters is true")
		}

		findFilter := sanitizeBulkUpdateFindFilter(input.FindFilter)
		err = r.withReadTxn(ctx, func(ctx context.Context) error {
			result, _, qErr := r.repository.Image.QueryAST(ctx, input.ImageFilterAst, findFilter)
			if qErr != nil {
				return qErr
			}

			imageIDs = idsFromItems(result, func(item *models.Image) int {
				return item.ID
			})
			return nil
		})
		if err != nil {
			return "", err
		}
	}

	translator := changesetTranslator{
		inputMap: getUpdateInputMap(ctx),
	}

	// Populate image from the input
	updatedImage := models.NewImagePartial()

	updatedImage.Title = translator.optionalString(input.Title, "title")
	updatedImage.Code = translator.optionalString(input.Code, "code")
	updatedImage.Details = translator.optionalString(input.Details, "details")
	updatedImage.Photographer = translator.optionalString(input.Photographer, "photographer")
	updatedImage.Rating = translator.optionalInt(input.Rating100, "rating100")
	updatedImage.Organized = translator.optionalBool(input.Organized, "organized")

	updatedImage.Date, err = translator.optionalDate(input.Date, "date")
	if err != nil {
		return "", fmt.Errorf("converting date: %w", err)
	}
	updatedImage.StudioID, err = translator.optionalIntFromString(input.StudioID, "studio_id")
	if err != nil {
		return "", fmt.Errorf("converting studio id: %w", err)
	}

	updatedImage.URLs = translator.optionalURLsBulk(input.Urls, input.URL)

	updatedImage.GalleryIDs, err = translator.updateIdsBulk(input.GalleryIds, "gallery_ids")
	if err != nil {
		return "", fmt.Errorf("converting gallery ids: %w", err)
	}
	updatedImage.PerformerIDs, err = translator.updateIdsBulk(input.PerformerIds, "performer_ids")
	if err != nil {
		return "", fmt.Errorf("converting performer ids: %w", err)
	}
	updatedImage.TagIDs, err = translator.updateIdsBulk(input.TagIds, "tag_ids")
	if err != nil {
		return "", fmt.Errorf("converting tag ids: %w", err)
	}

	if input.CustomFields != nil {
		updatedImage.CustomFields = *input.CustomFields
		// convert json.Numbers to int/float
		updatedImage.CustomFields.Full = convertMapJSONNumbers(updatedImage.CustomFields.Full)
		updatedImage.CustomFields.Partial = convertMapJSONNumbers(updatedImage.CustomFields.Partial)
	}

	operation := imageBulkUpdateOperation{
		repository:     r.repository.Image,
		galleryService: r.galleryService,
		updatedImage:   updatedImage,
	}

	if !useBackgroundJob {
		if err := r.withTxn(ctx, func(ctx context.Context) error {
			for _, imageID := range imageIDs {
				if err := operation.Update(ctx, imageID); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return "", err
		}

		for _, imageID := range imageIDs {
			r.hookExecutor.ExecutePostHooks(ctx, imageID, hook.ImageUpdatePost, input, translator.getFields())
		}

		return "sync", nil
	}

	jobID := r.enqueueBulkUpdate(ctx, "Bulk Image Update", imageIDs, operation, hook.ImageUpdatePost, input, translator.getFields())

	return strconv.Itoa(jobID), nil
}

// imageSetDateFromMTimeOperation is the per-id worker for
// imagesSetDateFromFileMTime. Each Update finds the image, loads its primary
// file, and writes that file's mtime back as the image's date.
type imageSetDateFromMTimeOperation struct {
	repository models.ImageReaderWriter
	fileGetter models.FileGetter
}

func (o imageSetDateFromMTimeOperation) Update(ctx context.Context, id int) error {
	i, err := o.repository.Find(ctx, id)
	if err != nil {
		return err
	}
	if i == nil {
		return fmt.Errorf("image with id %d not found", id)
	}

	if err := i.LoadPrimaryFile(ctx, o.fileGetter); err != nil {
		return fmt.Errorf("loading primary file: %w", err)
	}
	primary := i.Files.Primary()
	if primary == nil {
		return nil
	}

	mtime := primary.Base().ModTime
	partial := models.NewImagePartial()
	partial.Date = models.NewOptionalDate(models.Date{Time: mtime, Precision: models.DatePrecisionDay})

	if _, err := o.repository.UpdatePartial(ctx, id, partial); err != nil {
		return err
	}
	return nil
}

func (r *mutationResolver) ImagesSetDateFromFileMTime(ctx context.Context, input ImagesSetDateFromFileMTimeInput) (string, error) {
	var imageIDs []int
	var err error
	useBackgroundJob := input.ApplyToItemsMatchingFilters != nil && *input.ApplyToItemsMatchingFilters
	if len(input.Ids) > 0 {
		imageIDs, err = stringslice.StringSliceToIntSlice(input.Ids)
		if err != nil {
			return "", fmt.Errorf("converting ids: %w", err)
		}
	}
	if useBackgroundJob {
		if !hasBulkUpdateFilter(input.FindFilter, input.ImageFilterAst) {
			return "", fmt.Errorf("image_filter_ast or find_filter.q is required when apply_to_items_matching_filters is true")
		}

		findFilter := sanitizeBulkUpdateFindFilter(input.FindFilter)
		if err := r.withReadTxn(ctx, func(ctx context.Context) error {
			images, _, err := r.repository.Image.QueryAST(ctx, input.ImageFilterAst, findFilter)
			if err != nil {
				return err
			}

			imageIDs = idsFromItems(images, func(image *models.Image) int {
				return image.ID
			})
			return nil
		}); err != nil {
			return "", fmt.Errorf("querying ids: %w", err)
		}
	}

	operation := imageSetDateFromMTimeOperation{
		repository: r.repository.Image,
		fileGetter: r.repository.File,
	}

	if !useBackgroundJob {
		if err := r.withTxn(ctx, func(ctx context.Context) error {
			for _, id := range imageIDs {
				if err := operation.Update(ctx, id); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return "", err
		}

		for _, id := range imageIDs {
			r.hookExecutor.ExecutePostHooks(ctx, id, hook.ImageUpdatePost, input, []string{"date"})
		}

		return "sync", nil
	}

	jobID := r.enqueueBulkUpdate(ctx, "Set Image Dates From File MTime", imageIDs, operation, hook.ImageUpdatePost, input, []string{"date"})
	return strconv.Itoa(jobID), nil
}

func (r *mutationResolver) ImageDestroy(ctx context.Context, input models.ImageDestroyInput) (ret bool, err error) {
	imageID, err := strconv.Atoi(input.ID)
	if err != nil {
		return false, fmt.Errorf("converting id: %w", err)
	}

	trashPath := manager.GetInstance().Config.GetDeleteTrashPath()

	var i *models.Image
	fileDeleter := &image.FileDeleter{
		Deleter: file.NewDeleterWithTrash(trashPath),
		Paths:   manager.GetInstance().Paths,
	}
	if err := r.withTxn(ctx, func(ctx context.Context) error {
		i, err = r.repository.Image.Find(ctx, imageID)
		if err != nil {
			return err
		}

		if i == nil {
			return fmt.Errorf("image with id %d not found", imageID)
		}

		return r.imageService.Destroy(ctx, i, fileDeleter, utils.IsTrue(input.DeleteGenerated), utils.IsTrue(input.DeleteFile), utils.IsTrue(input.DestroyFileEntry))
	}); err != nil {
		fileDeleter.Rollback()
		return false, err
	}

	// perform the post-commit actions
	fileDeleter.Commit()

	// call post hook after performing the other actions
	r.hookExecutor.ExecutePostHooks(ctx, i.ID, hook.ImageDestroyPost, plugin.ImageDestroyInput{
		ImageDestroyInput: input,
		Checksum:          i.Checksum,
		Path:              i.Path,
	}, nil)

	return true, nil
}

func (r *mutationResolver) ImagesDestroy(ctx context.Context, input models.ImagesDestroyInput) (ret bool, err error) {
	imageIDs, err := stringslice.StringSliceToIntSlice(input.Ids)
	if err != nil {
		return false, fmt.Errorf("converting ids: %w", err)
	}

	trashPath := manager.GetInstance().Config.GetDeleteTrashPath()

	var images []*models.Image
	fileDeleter := &image.FileDeleter{
		Deleter: file.NewDeleterWithTrash(trashPath),
		Paths:   manager.GetInstance().Paths,
	}
	if err := r.withTxn(ctx, func(ctx context.Context) error {
		qb := r.repository.Image

		for _, imageID := range imageIDs {
			i, err := qb.Find(ctx, imageID)
			if err != nil {
				return err
			}

			if i == nil {
				return fmt.Errorf("image with id %d not found", imageID)
			}

			images = append(images, i)

			if err := r.imageService.Destroy(ctx, i, fileDeleter, utils.IsTrue(input.DeleteGenerated), utils.IsTrue(input.DeleteFile), utils.IsTrue(input.DestroyFileEntry)); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		fileDeleter.Rollback()
		return false, err
	}

	// perform the post-commit actions
	fileDeleter.Commit()

	for _, image := range images {
		// call post hook after performing the other actions
		r.hookExecutor.ExecutePostHooks(ctx, image.ID, hook.ImageDestroyPost, plugin.ImagesDestroyInput{
			ImagesDestroyInput: input,
			Checksum:           image.Checksum,
			Path:               image.Path,
		}, nil)
	}

	return true, nil
}

func (r *mutationResolver) ImageIncrementO(ctx context.Context, id string) (ret int, err error) {
	imageID, err := strconv.Atoi(id)
	if err != nil {
		return 0, fmt.Errorf("converting id: %w", err)
	}

	if err := r.withTxn(ctx, func(ctx context.Context) error {
		qb := r.repository.Image

		ret, err = qb.IncrementOCounter(ctx, imageID)
		return err
	}); err != nil {
		return 0, err
	}

	return ret, nil
}

func (r *mutationResolver) ImageDecrementO(ctx context.Context, id string) (ret int, err error) {
	imageID, err := strconv.Atoi(id)
	if err != nil {
		return 0, fmt.Errorf("converting id: %w", err)
	}

	if err := r.withTxn(ctx, func(ctx context.Context) error {
		qb := r.repository.Image

		ret, err = qb.DecrementOCounter(ctx, imageID)
		return err
	}); err != nil {
		return 0, err
	}

	return ret, nil
}

func (r *mutationResolver) ImageResetO(ctx context.Context, id string) (ret int, err error) {
	imageID, err := strconv.Atoi(id)
	if err != nil {
		return 0, fmt.Errorf("converting id: %w", err)
	}

	if err := r.withTxn(ctx, func(ctx context.Context) error {
		qb := r.repository.Image

		ret, err = qb.ResetOCounter(ctx, imageID)
		return err
	}); err != nil {
		return 0, err
	}

	return ret, nil
}

// ImageRotate rewrites the EXIF Orientation tag of the image's primary file
// in place, then re-fingerprints the file and persists the new fingerprint
// + dimensions on the file row. Pixel data is untouched. Only JPEGs that
// already carry an EXIF Orientation tag are supported; everything else
// returns an error.
func (r *mutationResolver) ImageRotate(ctx context.Context, input ImageRotateInput) (*models.Image, error) {
	imageID, err := strconv.Atoi(input.ID)
	if err != nil {
		return nil, fmt.Errorf("converting id: %w", err)
	}

	dir, err := rotateDirectionFromGraphQL(input.Direction)
	if err != nil {
		return nil, err
	}

	// Generated thumbnail/preview files are keyed on the image's MD5
	// checksum, so changing the MD5 naturally orphans the old paths.
	// Mark them for deletion via FileDeleter so the next request
	// regenerates them at the new checksum-keyed path. Generated files
	// bypass trash since they're cheap to recreate.
	fileDeleter := &image.FileDeleter{
		Deleter: file.NewDeleter(),
		Paths:   manager.GetInstance().Paths,
	}

	var ret *models.Image
	if err := r.withTxn(ctx, func(ctx context.Context) error {
		i, err := r.repository.Image.Find(ctx, imageID)
		if err != nil {
			return err
		}
		if i == nil {
			return fmt.Errorf("image with id %d not found", imageID)
		}

		if err := i.LoadPrimaryFile(ctx, r.repository.File); err != nil {
			return fmt.Errorf("loading primary file: %w", err)
		}

		primary := i.Files.Primary()
		if primary == nil {
			return fmt.Errorf("image %d has no primary file", imageID)
		}
		imageFile, ok := primary.(*models.ImageFile)
		if !ok {
			return fmt.Errorf("image %d primary file is not an image file", imageID)
		}
		base := imageFile.Base()
		if base.ZipFileID != nil {
			return fmt.Errorf("rotating zip-contained images is not supported")
		}

		// Mark generated files (thumbnail, clip preview) for deletion
		// using the *old* checksum. We do this before patching the file:
		// if the patch fails we'll roll back via the txn's defer below
		// and FileDeleter.Rollback restores the renames.
		if err := fileDeleter.MarkGeneratedFiles(i); err != nil {
			return fmt.Errorf("marking generated files: %w", err)
		}

		// Mutate the file on disk. Returns the orientation transition and
		// whether displayed dimensions need to swap. Honour the
		// PreserveMtimeOnRotate config setting (default: true) so that
		// scraped images don't lose meaningful upload-time mtimes.
		patch, err := fileimage.PatchJPEGOrientation(base.Path, dir, fileimage.PatchJPEGOrientationOptions{
			PreserveMTime: manager.GetInstance().Config.GetPreserveMtimeOnRotate(),
		})
		if err != nil {
			return fmt.Errorf("patching orientation for %q: %w", base.Path, err)
		}

		// Re-stat for the new size + mtime. When PreserveMTime is on,
		// mtime here equals the original; when off, it's "now". Either
		// way, copying it into the DB row keeps the scanner from
		// re-flagging the file as "changed" on the next scan.
		info, err := os.Stat(base.Path)
		if err != nil {
			return fmt.Errorf("stat after patch: %w", err)
		}

		newMD5, err := md5.FromFilePath(base.Path)
		if err != nil {
			return fmt.Errorf("recomputing MD5: %w", err)
		}

		// Apply changes to the in-memory file struct, then persist.
		if patch.DimensionsSwapped {
			imageFile.Width, imageFile.Height = imageFile.Height, imageFile.Width
		}
		base.Size = info.Size()
		base.ModTime = info.ModTime()
		base.UpdatedAt = time.Now()
		base.SetFingerprint(models.Fingerprint{
			Type:        models.FingerprintTypeMD5,
			Fingerprint: newMD5,
		})

		if err := r.repository.File.Update(ctx, imageFile); err != nil {
			return fmt.Errorf("updating file row: %w", err)
		}

		// Bump the image row's updated_at so the URL builder produces a
		// fresh `?t=<unix>` token. The original /image/<id>/image URL is
		// served with `Cache-Control: immutable, max-age=1y`, so without a
		// new token the browser keeps showing the pre-rotation bytes from
		// its cache on refresh.
		if _, err := r.repository.Image.UpdatePartial(ctx, imageID, models.NewImagePartial()); err != nil {
			return fmt.Errorf("bumping image updated_at: %w", err)
		}

		ret = i
		return nil
	}); err != nil {
		fileDeleter.Rollback()
		return nil, err
	}

	// Commit the deletion of stale generated files now that the txn
	// succeeded.
	fileDeleter.Commit()

	// Refetch outside the txn so the response reflects committed state and
	// post-hook plugins see the updated image.
	ret, err = r.getImage(ctx, imageID)
	if err != nil {
		return nil, err
	}

	r.hookExecutor.ExecutePostHooks(ctx, imageID, hook.ImageUpdatePost, nil, nil)

	return ret, nil
}

func rotateDirectionFromGraphQL(d ImageRotateDirection) (fileimage.RotateDirection, error) {
	switch d {
	case ImageRotateDirectionCw:
		return fileimage.RotateCW, nil
	case ImageRotateDirectionCcw:
		return fileimage.RotateCCW, nil
	case ImageRotateDirectionFlip:
		return fileimage.RotateFlip, nil
	default:
		return 0, errors.New("unknown rotate direction")
	}
}
