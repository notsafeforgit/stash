// Package entityid owns the UUID/repository-key boundary.
package entityid

import (
	"errors"
	"strings"
)

func ModeKind(mode string) string {
	switch strings.ToLower(mode) {
	case "movies", "groups", "group":
		return "group"
	case "galleries", "gallery":
		return "gallery"
	case "scene_markers", "scene_marker":
		return "scene_marker"
	default:
		return strings.TrimSuffix(strings.ToLower(mode), "s")
	}
}

// FilterAST's Any values use the same entity kinds as the typed criteria in
// the schema. Labels, text, provider IDs and custom fields remain untouched.
func CriterionKind(scope, field string) string {
	switch field {
	case "id", "parents", "children":
		return scope
	case "performers":
		return "performer"
	case "studios":
		return "studio"
	case "groups", "movies", "containing_groups", "sub_groups":
		return "group"
	case "galleries":
		return "gallery"
	case "scenes":
		return "scene"
	case "tags", "scene_tags", "performer_tags", "ancestor_tags", "descendant_tags":
		return "tag"
	case "parent_folder":
		return "folder"
	case "zip_file":
		return "file"
	default:
		return ""
	}
}

// MapCriterionIDs preserves user labels and operators while translating only
// known library reference slots. Site/provider identifiers remain untouched.
func MapCriterionIDs(scope, field string, value any, convert func(string, any) (any, error)) (any, error) {
	kind := CriterionKind(scope, field)
	if kind == "" || value == nil {
		return value, nil
	}
	criterion, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("library criterion must be an object")
	}
	ret := make(map[string]any, len(criterion))
	for key, raw := range criterion {
		ret[key] = raw
		if raw == nil || (key != "value" && key != "value2" && key != "excludes") {
			continue
		}
		mapped, err := mapChoiceIDs(kind, raw, convert)
		if err != nil {
			return nil, err
		}
		ret[key] = mapped
	}
	return ret, nil
}

func mapChoiceIDs(kind string, raw any, convert func(string, any) (any, error)) (any, error) {
	switch value := raw.(type) {
	case []any:
		ret := make([]any, len(value))
		for i, item := range value {
			mapped, err := mapChoiceIDs(kind, item, convert)
			if err != nil {
				return nil, err
			}
			ret[i] = mapped
		}
		return ret, nil
	case map[string]any:
		ret := make(map[string]any, len(value))
		for k, v := range value {
			ret[k] = v
		}
		key := "id"
		if _, ok := value[key]; !ok {
			key = "value"
		}
		id, ok := value[key]
		if !ok {
			return nil, errors.New("library choice has no identity")
		}
		mapped, err := mapChoiceIDs(kind, id, convert)
		if err != nil {
			return nil, err
		}
		ret[key] = mapped
		return ret, nil
	default:
		return convert(kind, raw)
	}
}

// MapFilterAST creates a new tree; it never mutates a request or saved filter.
func MapFilterAST(scope string, node map[string]any, convert func(string, any) (any, error)) (map[string]any, error) {
	ret := make(map[string]any, len(node))
	for key, value := range node {
		ret[key] = value
		switch typed := value.(type) {
		case map[string]any:
			if key == "condition" {
				condition := make(map[string]any, len(typed))
				for k, v := range typed {
					condition[k] = v
				}
				field, _ := condition["field"].(string)
				mapped, err := MapCriterionIDs(scope, field, condition["value"], convert)
				if err != nil {
					return nil, err
				}
				condition["value"] = mapped
				ret[key] = condition
			} else {
				mapped, err := MapFilterAST(scope, typed, convert)
				if err != nil {
					return nil, err
				}
				ret[key] = mapped
			}
		case []any:
			items := make([]any, len(typed))
			for i, v := range typed {
				child, ok := v.(map[string]any)
				if !ok {
					return nil, errors.New("invalid filter node")
				}
				mapped, err := MapFilterAST(scope, child, convert)
				if err != nil {
					return nil, err
				}
				items[i] = mapped
			}
			ret[key] = items
		}
	}
	return ret, nil
}
