package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/entityid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/common"
	"github.com/stashapp/stash/pkg/plugin/hook"
)

func hookEntityKind(trigger hook.TriggerEnum) models.ArchiveEntityKind {
	name, _, _ := strings.Cut(string(trigger), ".")
	switch name {
	case "SceneMarker":
		return models.ArchiveSceneMarker
	case "GalleryChapter":
		return models.ArchiveGalleryChapter
	}
	return models.ArchiveEntityKind(strings.ToLower(name))
}

func (c Cache) publicHookContext(ctx context.Context, trigger hook.TriggerEnum, notice common.HookContext) (common.HookContext, error) {
	if _, err := entityid.From(ctx); err != nil {
		ctx = entityid.WithRepository(ctx, c.repository)
	}
	kind := hookEntityKind(trigger)
	var input any
	if notice.Input != nil {
		data, err := json.Marshal(notice.Input)
		if err != nil {
			return notice, err
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&input); err != nil {
			return notice, err
		}
	}
	// Durable deletion events already carry the UUID captured in their receipt.
	if values, ok := input.(map[string]any); ok {
		if id, ok := values["id"].(string); ok {
			if u, err := uuid.Parse(id); err == nil && u != uuid.Nil && u.String() == id {
				notice.UUID = id
			}
		}
	}
	if notice.UUID == "" && notice.ID > 0 {
		var err error
		if strings.Contains(string(trigger), ".Destroy.") {
			notice.UUID = entityid.Recorded(ctx, kind, notice.ID)
		}
		if notice.UUID == "" {
			notice.UUID, err = entityid.Public(ctx, kind, notice.ID)
		}
		if err != nil {
			return notice, err
		}
	}
	var err error
	notice.Input, err = publicHookObject(ctx, kind, input, notice.UUID)
	return notice, err
}

func publicHookObject(ctx context.Context, kind models.ArchiveEntityKind, raw any, ownUUID string) (any, error) {
	switch value := raw.(type) {
	case []any:
		for i, item := range value {
			mapped, err := publicHookObject(ctx, kind, item, "")
			if err != nil {
				return nil, err
			}
			value[i] = mapped
		}
		return value, nil
	case map[string]any:
		for key, item := range value {
			if item == nil {
				continue
			}
			if key == "id" && ownUUID != "" {
				value[key] = ownUUID
				continue
			}
			var related models.ArchiveEntityKind
			switch key {
			case "id", "ids", "source", "destination", "source_id", "source_ids", "destination_id", "parent_ids", "child_ids":
				related = kind
			case "scene_id", "scene_ids":
				related = models.ArchiveScene
			case "image_id", "image_ids":
				related = models.ArchiveImage
			case "performer_id", "performer_ids":
				related = models.ArchivePerformer
			case "tag_id", "tag_ids", "primary_tag_id":
				related = models.ArchiveTag
			case "studio_id":
				related = models.ArchiveStudio
			case "group_id", "group_ids", "movie_id":
				related = models.ArchiveGroup
			case "gallery_id", "gallery_ids":
				related = models.ArchiveGallery
			case "file_id", "file_ids", "primary_file_id", "zip_file_id":
				related = models.ArchiveFile
			case "parent_folder_id":
				related = models.ArchiveFolder
			case "before", "after", "sources", "groups", "movies", "containing_groups", "sub_groups":
				mapped, err := publicHookObject(ctx, kind, item, "")
				if err != nil {
					return nil, err
				}
				value[key] = mapped
				continue
			}
			if related != "" {
				mapped, err := publicHookIDs(ctx, related, item)
				if err != nil {
					return nil, err
				}
				value[key] = mapped
			}
		}
	}
	return raw, nil
}

func publicHookIDs(ctx context.Context, kind models.ArchiveEntityKind, raw any) (any, error) {
	if raw == nil {
		return nil, nil
	}
	switch value := raw.(type) {
	case []any:
		for i, id := range value {
			mapped, err := publicHookIDs(ctx, kind, id)
			if err != nil {
				return nil, err
			}
			value[i] = mapped
		}
		return value, nil
	case map[string]any:
		// Bulk relationship input uses ids/mode; merge snapshots use id/name.
		return publicHookObject(ctx, kind, value, "")
	}
	value := fmt.Sprint(raw)
	if value == "" || value == "0" {
		return nil, nil
	}
	if id, err := uuid.Parse(value); err == nil && id != uuid.Nil && id.String() == value {
		return value, nil
	}
	id, err := strconv.Atoi(value)
	if err != nil {
		return nil, fmt.Errorf("invalid %s hook reference", kind)
	}
	if captured := entityid.Recorded(ctx, kind, id); captured != "" {
		return captured, nil
	}
	return entityid.Public(ctx, kind, id)
}
