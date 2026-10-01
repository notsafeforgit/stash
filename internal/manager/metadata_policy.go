package manager

import (
	"context"

	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
)

func metadataHandlerRequired(repo models.Repository) file.Filter {
	return file.FilterFunc(func(ctx context.Context, media models.File) bool {
		if repo.MetadataPolicy == nil || media.Base().ZipFileID != nil {
			return false
		}
		kind := models.ArchiveImage
		if useAsVideo(media.Base().Path) {
			kind = models.ArchiveScene
		} else if !useAsImage(media.Base().Path) {
			return false
		}
		matches, err := repo.MetadataPolicy.MatchScan(ctx, media.Base().Path)
		if err != nil {
			return true // Surface the error through the transactional handler.
		}
		for _, match := range matches {
			if match.Policy.Definition.Enabled && match.Policy.Definition.Rules[kind].OnExisting {
				return true
			}
		}
		return false
	})
}

func metadataHookFields(fields []string) []string {
	ret := make([]string, 0, len(fields))
	for _, field := range fields {
		switch field {
		case "performers":
			field = "performer_ids"
		case "tags":
			field = "tag_ids"
		case "studio":
			field = "studio_id"
		}
		ret = append(ret, field)
	}
	return ret
}

// RegisterMetadataPolicyHooks uses the same after-commit notification contract
// as ordinary library edits. Ingestion's resumable worker delivers its own hooks.
func (s *Manager) RegisterMetadataPolicyHooks(ctx context.Context, input metadata.Input, fields []string) error {
	if s.PluginCache == nil || len(fields) == 0 {
		return nil
	}
	entity, err := s.Repository.ArchiveEntity.Find(ctx, input.EntityUUID)
	if err != nil {
		return err
	}
	if entity == nil || entity.State != models.ArchiveEntityActive || entity.LocalID == nil {
		return models.ErrMetadataFieldConflict
	}
	trigger := hook.ImageUpdatePost
	if entity.Kind == models.ArchiveScene {
		trigger = hook.SceneUpdatePost
	}
	s.PluginCache.RegisterPostHooks(ctx, *entity.LocalID, trigger, nil, metadataHookFields(fields))
	return nil
}

func (s *Manager) applyScanMetadata(ctx context.Context, kind models.ArchiveEntityKind, id int, file models.File, created bool) error {
	preview, err := (metadata.Service{Repo: s.Repository}).ApplyScan(ctx, kind, id, file, created)
	if err != nil || preview == nil {
		return err
	}
	if fields := preview.ReviewFields(); len(fields) > 0 {
		logger.Warnf("Metadata policy for %s %d requires review: %v", kind, id, fields)
	}
	if fields := preview.AppliedFields(); !created && len(fields) > 0 && s.PluginCache != nil {
		trigger := hook.ImageUpdatePost
		if kind == models.ArchiveScene {
			trigger = hook.SceneUpdatePost
		}
		s.PluginCache.RegisterPostHooks(ctx, id, trigger, nil, metadataHookFields(fields))
	}
	return nil
}
