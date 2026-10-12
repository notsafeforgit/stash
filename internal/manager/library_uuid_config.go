package manager

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/entityid"
	"github.com/stashapp/stash/pkg/models"
	"strconv"
)

// ValidateLibraryUIReferences keeps untyped configuration writes from
// reintroducing storage keys into the known library-reference slots.
func ValidateLibraryUIReferences(ctx context.Context, ui map[string]any) error {
	_, err := visitLibraryUIReferences(ui, func(kind models.ArchiveEntityKind, value any) (any, error) {
		id, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%s reference must be a UUID", kind)
		}
		_, err := entityid.FilterKey(ctx, kind, id)
		return value, err
	})
	return err
}

// PromoteLibraryUUIDConfig converts configuration-owned references once, before
// requests can delete/reuse a key. Unknown keys stay permanently nonmatching.
func (s *Manager) PromoteLibraryUUIDConfig(ctx context.Context) error {
	legacy := false
	_, err := visitLibraryUIReferences(s.Config.GetUIConfiguration(), func(_ models.ArchiveEntityKind, value any) (any, error) {
		if _, err := strconv.Atoi(fmt.Sprint(value)); err == nil {
			legacy = true
		}
		return value, nil
	})
	if err != nil || !legacy {
		return err
	}
	_, err = s.Config.UpdateUIConfiguration(func(current map[string]any) (map[string]any, error) {
		err := s.Repository.WithReadTxn(ctx, func(ctx context.Context) error {
			_, err := visitLibraryUIReferences(current, func(kind models.ArchiveEntityKind, value any) (any, error) {
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
		return current, err
	})
	return err
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
	return changed, nil
}
