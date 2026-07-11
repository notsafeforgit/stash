package manager

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/stashapp/stash/pkg/models"
)

const forkDefaultFilterStateKey = "forkDefaultFilterState"

func (s *Manager) reconcileDefaultFilterConfig() error {
	uiConfig := s.Config.GetUIConfiguration()
	changed, err := reconcileDefaultFilterConfig(uiConfig)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	s.Config.SetUIConfiguration(uiConfig)
	return s.Config.Write()
}

func reconcileDefaultFilterConfig(uiConfig map[string]interface{}) (bool, error) {
	if uiConfig == nil {
		return false, nil
	}

	defaults, ok := stringMap(uiConfig["defaultFilters"])
	if !ok || len(defaults) == 0 {
		if _, exists := uiConfig[forkDefaultFilterStateKey]; exists {
			delete(uiConfig, forkDefaultFilterStateKey)
			return true, nil
		}
		return false, nil
	}

	states, _ := stringMap(uiConfig[forkDefaultFilterStateKey])
	if states == nil {
		states = make(map[string]interface{})
	}
	changed := false

	for key, rawDefault := range defaults {
		defaultFilter, ok := stringMap(rawDefault)
		if !ok {
			delete(defaults, key)
			delete(states, key)
			changed = true
			continue
		}
		objectFilter, _ := stringMap(defaultFilter["object_filter"])

		state, _ := stringMap(states[key])
		if state == nil {
			state = make(map[string]interface{})
		}

		canonical, err := decodeDefaultFilterAST(state["filter_ast"])
		if err != nil {
			return false, fmt.Errorf("decoding fork default filter %q: %w", key, err)
		}
		if canonical == nil {
			canonical, err = decodeDefaultFilterAST(defaultFilter["filter_ast"])
			if err != nil {
				return false, fmt.Errorf("decoding default filter %q: %w", key, err)
			}
		}
		if canonical == nil && len(objectFilter) > 0 {
			canonical, err = models.FilterASTFromLegacySavedFilter(objectFilter)
			if err != nil {
				return false, fmt.Errorf("importing default filter %q: %w", key, err)
			}
		}
		if canonical == nil {
			if _, exists := states[key]; exists {
				delete(states, key)
				changed = true
			}
			if _, exists := defaultFilter["filter_ast"]; exists {
				delete(defaultFilter, "filter_ast")
				changed = true
			}
			continue
		}

		shadow, shadowSet := stringMap(state["legacy_object_filter"])
		if shadowSet && !reflect.DeepEqual(objectFilter, shadow) {
			if canonical.IsFlatRepresentable() {
				if len(objectFilter) == 0 {
					delete(states, key)
					delete(defaultFilter, "filter_ast")
					changed = true
					continue
				}
				imported, importErr := models.FilterASTFromLegacySavedFilter(objectFilter)
				if importErr == nil && imported != nil {
					canonical = imported
					state["legacy_object_filter"] = objectFilter
					delete(state, "pending_legacy_object_filter")
				} else {
					state["pending_legacy_object_filter"] = objectFilter
				}
			} else {
				state["pending_legacy_object_filter"] = objectFilter
			}
			changed = true
		}

		if !shadowSet {
			objectFilter, _ = canonical.FlatObjectFilter()
			if !reflect.DeepEqual(defaultFilter["object_filter"], objectFilter) {
				defaultFilter["object_filter"] = objectFilter
			}
			state["legacy_object_filter"] = objectFilter
			changed = true
		}

		encodedAST, err := encodeDefaultFilterAST(canonical)
		if err != nil {
			return false, fmt.Errorf("encoding default filter %q: %w", key, err)
		}
		if !reflect.DeepEqual(defaultFilter["filter_ast"], encodedAST) {
			defaultFilter["filter_ast"] = encodedAST
			changed = true
		}
		if !reflect.DeepEqual(state["filter_ast"], encodedAST) {
			state["filter_ast"] = encodedAST
			changed = true
		}
		states[key] = state
	}

	for key := range states {
		if _, exists := defaults[key]; !exists {
			delete(states, key)
			changed = true
		}
	}

	if len(states) == 0 {
		if _, exists := uiConfig[forkDefaultFilterStateKey]; exists {
			delete(uiConfig, forkDefaultFilterStateKey)
			changed = true
		}
	} else {
		if !reflect.DeepEqual(uiConfig[forkDefaultFilterStateKey], states) {
			uiConfig[forkDefaultFilterStateKey] = states
			changed = true
		}
	}

	return changed, nil
}

func stringMap(v interface{}) (map[string]interface{}, bool) {
	ret, ok := v.(map[string]interface{})
	return ret, ok
}

func decodeDefaultFilterAST(v interface{}) (*models.FilterAST, error) {
	if v == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var ret models.FilterAST
	if err := json.Unmarshal(encoded, &ret); err != nil {
		return nil, err
	}
	if ret.Root == nil {
		return nil, nil
	}
	return &ret, nil
}

func encodeDefaultFilterAST(v *models.FilterAST) (map[string]interface{}, error) {
	encoded, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var ret map[string]interface{}
	if err := json.Unmarshal(encoded, &ret); err != nil {
		return nil, err
	}
	return ret, nil
}
