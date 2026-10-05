package metadata

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"github.com/stashapp/stash/pkg/models"
)

type referenceName struct {
	Name       string `json:"name"`
	SceneIndex *int   `json:"scene_index,omitempty"`
}

type groupReference struct {
	UUID       string `json:"uuid"`
	SceneIndex *int   `json:"scene_index,omitempty"`
}

func referenceNames(field models.MetadataFieldDefinition, raw json.RawMessage) ([]referenceName, error) {
	ret := []referenceName{}
	switch field.Type {
	case "reference":
		var name *string
		if err := json.Unmarshal(raw, &name); err != nil {
			return nil, errors.New("studio name matching requires a name or null")
		}
		if name != nil {
			ret = append(ret, referenceName{Name: *name})
		}
	case "references":
		var names []string
		if err := json.Unmarshal(raw, &names); err != nil || names == nil {
			return nil, errors.New("relationship name matching requires an array of names")
		}
		for _, name := range names {
			ret = append(ret, referenceName{Name: name})
		}
	case "groups":
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if !json.Valid(raw) || decoder.Decode(&ret) != nil || ret == nil {
			return nil, errors.New("group name matching requires an array of name and optional scene_index objects")
		}
	default:
		return nil, errors.New("name matching requires a relationship field")
	}
	if len(ret) > 128 {
		return nil, errors.New("relationship name matching supports at most 128 names")
	}
	for _, ref := range ret {
		if ref.Name == "" || strings.TrimSpace(ref.Name) != ref.Name || len(ref.Name) > 1024 || strings.ContainsFunc(ref.Name, unicode.IsControl) {
			return nil, errors.New("reference name must be nonempty text within 1024 bytes")
		}
	}
	return ret, nil
}

// An unresolved source name cannot remove a previously inherited relationship.
// Known group choices retain their selected scene index; unrelated existing
// groups stay in place until all names can be resolved.
func preserveUnresolvedReferences(field string, current, selected json.RawMessage) (json.RawMessage, error) {
	if field == "groups" {
		var existing, matched []groupReference
		if err := json.Unmarshal(current, &existing); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(selected, &matched); err != nil {
			return nil, err
		}
		ids := make(map[string]bool)
		for _, item := range matched {
			ids[item.UUID] = true
		}
		for _, item := range existing {
			if !ids[item.UUID] {
				matched = append(matched, item)
			}
		}
		return json.Marshal(matched)
	}
	var existing, matched []string
	if err := json.Unmarshal(current, &existing); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(selected, &matched); err != nil {
		return nil, err
	}
	return json.Marshal(append(existing, matched...))
}
