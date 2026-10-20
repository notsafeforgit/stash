package bulkupdate

import (
	"errors"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

// Reference is a local relationship target in the accepted command. Admission
// binds it to its UUID so deletion/ID reuse cannot redirect a resumed edit.
type Reference struct {
	Kind models.ArchiveEntityKind `json:"kind"`
	ID   int                      `json:"id"`
}

const MaxReferences = 1000

func References(operation Operation) ([]Reference, error) {
	var ret []Reference
	seen := map[Reference]bool{}
	var invalid error
	add := func(kind models.ArchiveEntityKind, id int) {
		if len(ret) > MaxReferences {
			return
		}
		if id <= 0 {
			invalid = fmt.Errorf("invalid %s relationship ID", kind)
			return
		}
		ref := Reference{kind, id}
		if !seen[ref] {
			ret = append(ret, ref)
			seen[ref] = true
		}
	}
	mode := func(m models.RelationshipUpdateMode) {
		if !m.IsValid() {
			invalid = fmt.Errorf("invalid relationship update mode %q", m)
		}
	}
	optional := func(kind models.ArchiveEntityKind, value models.OptionalInt) {
		if value.Set && !value.Null {
			add(kind, value.Value)
		}
	}
	file := func(id *models.FileID) {
		if id != nil {
			add(models.ArchiveFile, int(*id))
		}
	}
	ids := func(kind models.ArchiveEntityKind, update *models.UpdateIDs) {
		if update != nil {
			mode(update.Mode)
			for _, id := range update.IDs {
				add(kind, id)
			}
		}
	}
	strings := func(update *models.UpdateStrings) {
		if update != nil {
			mode(update.Mode)
		}
	}
	stashIDs := func(update *models.UpdateStashIDs) {
		if update != nil {
			mode(update.Mode)
		}
	}
	groups := func(update *models.UpdateGroupDescriptions) {
		if update != nil {
			mode(update.Mode)
			for _, group := range update.Groups {
				add(models.ArchiveGroup, group.GroupID)
			}
		}
	}
	switch op := operation.(type) {
	case Scene:
		p := op.Changes
		optional(models.ArchiveStudio, p.StudioID)
		file(p.PrimaryFileID)
		ids(models.ArchiveGallery, p.GalleryIDs)
		ids(models.ArchiveTag, p.TagIDs)
		ids(models.ArchivePerformer, p.PerformerIDs)
		strings(p.URLs)
		stashIDs(p.StashIDs)
		if p.GroupIDs != nil {
			mode(p.GroupIDs.Mode)
			for _, group := range p.GroupIDs.Groups {
				add(models.ArchiveGroup, group.GroupID)
			}
		}
	case Image:
		p := op.Changes
		optional(models.ArchiveStudio, p.StudioID)
		file(p.PrimaryFileID)
		ids(models.ArchiveGallery, p.GalleryIDs)
		ids(models.ArchiveTag, p.TagIDs)
		ids(models.ArchivePerformer, p.PerformerIDs)
		strings(p.URLs)
	case Gallery:
		p := op.Changes
		optional(models.ArchiveStudio, p.StudioID)
		file(p.PrimaryFileID)
		ids(models.ArchiveScene, p.SceneIDs)
		ids(models.ArchiveTag, p.TagIDs)
		ids(models.ArchivePerformer, p.PerformerIDs)
		strings(p.URLs)
	case Performer:
		p := op.Changes
		ids(models.ArchiveTag, p.TagIDs)
		strings(p.URLs)
		stashIDs(p.StashIDs)
		if p.Aliases != nil {
			mode(p.Aliases.Mode)
		}
	case Studio:
		p := op.Changes
		optional(models.ArchiveStudio, p.ParentID)
		ids(models.ArchiveTag, p.TagIDs)
		strings(p.URLs)
		strings(p.Aliases)
		stashIDs(p.StashIDs)
	case Tag:
		p := op.Changes
		ids(models.ArchiveTag, p.ParentIDs)
		ids(models.ArchiveTag, p.ChildIDs)
		strings(p.Aliases)
		stashIDs(p.StashIDs)
	case Group:
		p := op.Changes
		optional(models.ArchiveStudio, p.StudioID)
		ids(models.ArchiveTag, p.TagIDs)
		strings(p.URLs)
		groups(p.ContainingGroups)
		groups(p.SubGroups)
	case SceneMarker:
		p := op.Changes
		optional(models.ArchiveTag, p.PrimaryTagID)
		optional(models.ArchiveScene, p.SceneID)
		ids(models.ArchiveTag, p.TagIDs)
	case SceneDateFromMTime, ImageDateFromMTime:
	default:
		return nil, errors.New("bulk operation has no supported durable command")
	}
	if len(ret) > MaxReferences {
		return nil, fmt.Errorf("bulk command exceeds %d relationship references", MaxReferences)
	}
	return ret, invalid
}
