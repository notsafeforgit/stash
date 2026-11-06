package dedup

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/image"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scene"
)

// mergeMedia runs only after both complete files have been verified equal and
// their unchanged identities checked under the native writer transaction.
func (s *Service) mergeMedia(ctx context.Context, state models.FileDeduplicationState) error {
	source, destination := state.MergeSource, state.Owner
	if state.Input.MediaPolicy != models.FileDeduplicationEarliest || source == nil || destination == nil || source.Kind != destination.Kind {
		return models.ErrFileDeduplicationInvalid
	}
	for _, expected := range []*models.ArchiveEntity{source, destination} {
		actual, err := s.repo.ArchiveEntity.Find(ctx, expected.UUID)
		if err != nil {
			return err
		}
		if actual == nil || actual.State != models.ArchiveEntityActive || actual.Revision != expected.Revision {
			return models.ErrFileDeduplicationConflict
		}
	}
	for _, field := range []string{"date", "details", "urls"} {
		dest, err := s.repo.MetadataField.State(ctx, destination.UUID, field)
		if err != nil {
			return err
		}
		src, err := s.repo.MetadataField.State(ctx, source.UUID, field)
		if err != nil {
			return err
		}
		var value json.RawMessage
		if field == "urls" {
			// Explicit clears remain clears. Original source links also remain in
			// the retained source history through the media identity redirect.
			if dest.Mode == "clear" || dest.Pending {
				continue
			}
			var a, b []string
			if json.Unmarshal(dest.Value, &a) != nil || json.Unmarshal(src.Value, &b) != nil {
				return models.ErrFileDeduplicationConflict
			}
			before := len(a)
			for _, url := range b {
				if !slices.Contains(a, url) {
					a = append(a, url)
				}
			}
			if len(a) == before {
				continue
			}
			value, err = json.Marshal(a)
			if err != nil {
				return err
			}
		} else {
			var candidate *string
			if !metadata.CanFillBlank(dest) || json.Unmarshal(src.Value, &candidate) != nil || candidate == nil || strings.TrimSpace(*candidate) == "" {
				continue
			}
			value = src.Value
		}
		_, err = s.repo.MetadataField.Decide(ctx, models.MetadataFieldDecisionInput{
			EntityUUID: destination.UUID, ExpectedEntityRevision: dest.Entity.Revision,
			Field: field, Mode: "set", Origin: "review", Value: value,
			Reason: "Reviewed equal-byte deduplication: retain the earliest record, fill eligible blanks, and preserve source URLs.",
		})
		if err != nil {
			return err
		}
	}
	current, err := s.repo.ArchiveEntity.Find(ctx, destination.UUID)
	if err != nil {
		return err
	}
	if source.Kind == models.ArchiveImage {
		_, err = image.Merge(ctx, s.repo, *source.LocalID, *destination.LocalID, source.Revision, current.Revision)
		return err
	}
	if source.Kind != models.ArchiveScene || s.scenes == nil {
		return models.ErrFileDeduplicationInvalid
	}
	src, err := s.repo.Scene.Find(ctx, *source.LocalID)
	if err != nil {
		return err
	}
	if err := src.LoadGalleryIDs(ctx, s.repo.Scene); err != nil {
		return err
	}
	partial := models.NewScenePartial()
	partial.GalleryIDs = &models.UpdateIDs{Mode: models.RelationshipUpdateModeAdd, IDs: src.GalleryIDs.List()}
	deleter := &scene.FileDeleter{Deleter: file.NewDeleterWithTrash(s.trash),
		FileNamingAlgo: s.scenes.Config.GetVideoFileNamingAlgorithm(), Paths: s.scenes.Paths}
	deleter.RegisterHooks(ctx)
	return s.scenes.Merge(ctx, []int{*source.LocalID}, *destination.LocalID, deleter,
		scene.MergeOptions{ScenePartial: partial, IncludePlayHistory: true, IncludeOHistory: true})
}
