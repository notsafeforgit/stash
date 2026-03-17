package api

import (
	"context"
	"strconv"

	"github.com/stashapp/stash/pkg/models"
)

func (r *savedFilterResolver) Filter(ctx context.Context, obj *models.SavedFilter) (string, error) {
	return "", nil
}

func (r *savedFilterResolver) LabelMapping(ctx context.Context, obj *models.SavedFilter) (*LabelMappingType, error) {
	mapping := &LabelMappingType{}
	if obj.ObjectFilter == nil {
		return mapping, nil
	}

	// Helper to extract IDs from a list of strings
	extractIDs := func(v interface{}) []int {
		var ids []int
		if list, ok := v.([]interface{}); ok {
			for _, item := range list {
				if strID, ok := item.(string); ok {
					if intID, err := strconv.Atoi(strID); err == nil {
						ids = append(ids, intID)
					}
				}
			}
		}
		return ids
	}

	// Helper to fetch and populate mapping
	populateMapping := func(criteriaKey string, fetchLabels func([]int) map[string]interface{}) {
		criterion, ok := obj.ObjectFilter[criteriaKey].(map[string]interface{})
		if !ok {
			return
		}

		var allIDs []int
		if val, ok := criterion["value"]; ok {
			allIDs = append(allIDs, extractIDs(val)...)
		}
		if excl, ok := criterion["excludes"]; ok {
			allIDs = append(allIDs, extractIDs(excl)...)
		}

		if len(allIDs) > 0 {
			labels := fetchLabels(allIDs)
			// Assign directly to the proper field via reflection or switch statement.
			// Instead of reflection, just assign the returned map to the respective field on LabelMappingType
			switch criteriaKey {
			case "tags":
				mapping.Tags = labels
			case "scene_tags":
				mapping.SceneTags = labels
			case "performer_tags":
				mapping.PerformerTags = labels
			case "performers":
				mapping.Performers = labels
			case "studios":
				mapping.Studios = labels
			case "groups":
				mapping.Groups = labels
			case "galleries":
				mapping.Galleries = labels
			case "folders":
				mapping.Folders = labels
			case "parent_folder":
				mapping.ParentFolder = labels
			}
		}
	}

	err := r.withReadTxn(ctx, func(ctx context.Context) error {
		// Tags
		populateMapping("tags", func(ids []int) map[string]interface{} {
			res := make(map[string]interface{})
			tags, _ := r.repository.Tag.FindMany(ctx, ids)
			for _, t := range tags {
				res[strconv.Itoa(t.ID)] = t.Name
			}
			return res
		})

		populateMapping("scene_tags", func(ids []int) map[string]interface{} {
			res := make(map[string]interface{})
			tags, _ := r.repository.Tag.FindMany(ctx, ids)
			for _, t := range tags {
				res[strconv.Itoa(t.ID)] = t.Name
			}
			return res
		})

		populateMapping("performer_tags", func(ids []int) map[string]interface{} {
			res := make(map[string]interface{})
			tags, _ := r.repository.Tag.FindMany(ctx, ids)
			for _, t := range tags {
				res[strconv.Itoa(t.ID)] = t.Name
			}
			return res
		})

		// Performers
		populateMapping("performers", func(ids []int) map[string]interface{} {
			res := make(map[string]interface{})
			performers, _ := r.repository.Performer.FindMany(ctx, ids)
			for _, p := range performers {
				res[strconv.Itoa(p.ID)] = p.Name
			}
			return res
		})

		// Studios
		populateMapping("studios", func(ids []int) map[string]interface{} {
			res := make(map[string]interface{})
			studios, _ := r.repository.Studio.FindMany(ctx, ids)
			for _, s := range studios {
				res[strconv.Itoa(s.ID)] = s.Name
			}
			return res
		})

		// Groups
		populateMapping("groups", func(ids []int) map[string]interface{} {
			res := make(map[string]interface{})
			groups, _ := r.repository.Group.FindMany(ctx, ids)
			for _, g := range groups {
				res[strconv.Itoa(g.ID)] = g.Name
			}
			return res
		})

		// Galleries
		populateMapping("galleries", func(ids []int) map[string]interface{} {
			res := make(map[string]interface{})
			galleries, _ := r.repository.Gallery.FindMany(ctx, ids)
			for _, g := range galleries {
				res[strconv.Itoa(g.ID)] = g.Title
			}
			return res
		})

		// Folders
		populateMapping("folders", func(ids []int) map[string]interface{} {
			res := make(map[string]interface{})
			folderIDs := make([]models.FolderID, len(ids))
			for i, id := range ids {
				folderIDs[i] = models.FolderID(id)
			}
			folders, _ := r.repository.Folder.FindMany(ctx, folderIDs)
			for _, f := range folders {
				res[strconv.Itoa(int(f.ID))] = f.Path
			}
			return res
		})

		// Parent Folders
		populateMapping("parent_folder", func(ids []int) map[string]interface{} {
			res := make(map[string]interface{})
			folderIDs := make([]models.FolderID, len(ids))
			for i, id := range ids {
				folderIDs[i] = models.FolderID(id)
			}
			folders, _ := r.repository.Folder.FindMany(ctx, folderIDs)
			for _, f := range folders {
				res[strconv.Itoa(int(f.ID))] = f.Path
			}
			return res
		})

		return nil
	})

	return mapping, err
}
