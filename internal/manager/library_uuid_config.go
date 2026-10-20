package manager

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/entityid"
	"github.com/stashapp/stash/pkg/models"
	"strconv"
)

// ValidateLibraryUIReferences keeps untyped configuration writes from
// reintroducing storage keys into the known library-reference slots.
func ValidateLibraryUIReferences(ctx context.Context, ui map[string]any) error {
	_, err := visitLibraryUIReferences(ui, validateLibraryReference(ctx))
	return err
}

func ValidateLibraryConfigReferences(ctx context.Context, c *config.Config) error {
	_, err := visitLibraryConfigReferences(c, validateLibraryReference(ctx))
	return err
}

func validateLibraryReference(ctx context.Context) func(models.ArchiveEntityKind, any) (any, error) {
	return func(kind models.ArchiveEntityKind, value any) (any, error) {
		id, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%s reference must be a UUID", kind)
		}
		_, err := entityid.FilterKey(ctx, kind, id)
		return value, err
	}
}

// PromoteLibraryUUIDConfig converts configuration-owned references once, before
// requests can delete/reuse a key. Unknown keys stay permanently nonmatching.
func (s *Manager) PromoteLibraryUUIDConfig(ctx context.Context) error {
	legacy := false
	_, err := visitLibraryConfigReferences(s.Config.Snapshot(), func(_ models.ArchiveEntityKind, value any) (any, error) {
		if _, err := strconv.Atoi(fmt.Sprint(value)); err == nil {
			legacy = true
		}
		return value, nil
	})
	if err != nil || !legacy {
		return err
	}
	err = s.Config.Update(ctx, func(candidate *config.Config) error {
		return s.Repository.WithReadTxn(ctx, func(ctx context.Context) error {
			_, err := visitLibraryConfigReferences(candidate, func(kind models.ArchiveEntityKind, value any) (any, error) {
				id, err := strconv.Atoi(fmt.Sprint(value))
				if err != nil {
					return value, nil
				}
				entity, err := s.Repository.ArchiveEntity.FindByLocalID(ctx, kind, id)
				if err != nil {
					return nil, err
				}
				if entity == nil {
					return uuid.NewString(), nil
				}
				return entity.UUID, nil
			})
			return err
		})
	})
	return err
}

func visitLibraryConfigReferences(c *config.Config, convert func(models.ArchiveEntityKind, any) (any, error)) (bool, error) {
	ui := c.GetUIConfiguration()
	changed, err := visitLibraryUIReferences(ui, convert)
	if err != nil {
		return false, err
	}
	if changed {
		c.SetUIConfiguration(ui)
	}
	identify := c.GetDefaultIdentifySettings()
	identifyChanged := false
	err = identify.VisitReferences(func(kind models.ArchiveEntityKind, id *string) error {
		value, err := convert(kind, *id)
		if err != nil {
			return err
		}
		next := fmt.Sprint(value)
		identifyChanged = identifyChanged || next != *id
		*id = next
		return nil
	})
	if err != nil {
		return false, err
	}
	if identifyChanged {
		c.SetInterface(config.DefaultIdentifySettings, identify)
	}
	autoTag := c.GetDefaultAutoTagSettings()
	autoTagChanged := false
	if autoTag != nil {
		for _, selection := range []struct {
			kind models.ArchiveEntityKind
			ids  []string
		}{
			{models.ArchivePerformer, autoTag.Performers},
			{models.ArchiveStudio, autoTag.Studios},
			{models.ArchiveTag, autoTag.Tags},
		} {
			for i, id := range selection.ids {
				if len(selection.ids) == 1 && id == "*" {
					continue
				}
				value, err := convert(selection.kind, id)
				if err != nil {
					return false, err
				}
				next := fmt.Sprint(value)
				autoTagChanged = autoTagChanged || next != id
				selection.ids[i] = next
			}
		}
		if autoTagChanged {
			c.SetInterface(config.DefaultAutoTagSettings, autoTag)
		}
	}
	return changed || identifyChanged || autoTagChanged, nil
}

// Only these UI preference slots contain library references. Action IDs,
// provider IDs, arbitrary settings and custom field values are opaque.
func visitLibraryUIReferences(ui map[string]any, convert func(models.ArchiveEntityKind, any) (any, error)) (bool, error) {
	changed := false
	field := func(object map[string]any, key string, kind models.ArchiveEntityKind) error {
		value, ok := object[key]
		if !ok || value == nil || value == "" {
			return nil
		}
		mapped, err := convert(kind, value)
		if err != nil {
			return err
		}
		object[key] = mapped
		changed = changed || fmt.Sprint(value) != fmt.Sprint(mapped)
		return nil
	}
	rows, _ := ui["frontPageContent"].([]any)
	for _, raw := range rows {
		if row, ok := raw.(map[string]any); ok && row["__typename"] == "SavedFilter" {
			if err := field(row, "savedFilterId", models.ArchiveSavedFilter); err != nil {
				return false, err
			}
		}
	}
	tv, _ := ui["tv"].(map[string]any)
	for _, name := range []string{"sceneFilter", "markerFilter"} {
		if filter, ok := tv[name].(map[string]any); ok && filter["kind"] == "saved" {
			if err := field(filter, "id", models.ArchiveSavedFilter); err != nil {
				return false, err
			}
		}
	}
	rail, _ := tv["rail"].([]any)
	for _, raw := range rail {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		actions, _ := row["actions"].([]any)
		if row["type"] == "action" {
			actions = []any{row["action"]}
		}
		for _, raw := range actions {
			action, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			switch action["kind"] {
			case "quick-tag":
				if err := field(action, "tagId", models.ArchiveTag); err != nil {
					return false, err
				}
			case "quick-marker":
				if err := field(action, "primaryTagId", models.ArchiveTag); err != nil {
					return false, err
				}
				ids, _ := action["tagIds"].([]any)
				for i, id := range ids {
					mapped, err := convert(models.ArchiveTag, id)
					if err != nil {
						return false, err
					}
					ids[i] = mapped
					changed = changed || fmt.Sprint(id) != fmt.Sprint(mapped)
				}
			}
		}
	}
	defaults, _ := ui["taskDefaults"].(map[string]any)
	for _, selection := range []struct {
		task, field string
		kind        models.ArchiveEntityKind
	}{
		{"autoTag", "performers", models.ArchivePerformer},
		{"autoTag", "studios", models.ArchiveStudio},
		{"autoTag", "tags", models.ArchiveTag},
		{"generate", "sceneIDs", models.ArchiveScene},
		{"generate", "imageIDs", models.ArchiveImage},
		{"generate", "galleryIDs", models.ArchiveGallery},
		{"generate", "markerIDs", models.ArchiveSceneMarker},
	} {
		task, _ := defaults[selection.task].(map[string]any)
		ids, _ := task[selection.field].([]any)
		for i, id := range ids {
			if selection.task == "autoTag" && len(ids) == 1 && id == "*" {
				continue
			}
			mapped, err := convert(selection.kind, id)
			if err != nil {
				return false, err
			}
			changed = changed || fmt.Sprint(id) != fmt.Sprint(mapped)
			ids[i] = mapped
		}
	}
	return changed, nil
}
