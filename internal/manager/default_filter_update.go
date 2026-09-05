package manager

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/stashapp/stash/pkg/models"
)

var defaultFilterViewPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ConfigureDefaultFilter changes only the requested view under the same lock
// used by legacy UI-setting writes. Conflict actions never trust a client's
// possibly stale configuration snapshot.
func (s *Manager) ConfigureDefaultFilter(view, action string, filter *models.SavedFilter) (map[string]interface{}, error) {
	if !defaultFilterViewPattern.MatchString(view) {
		return nil, errors.New("invalid default-filter view")
	}
	return s.Config.UpdateUIConfiguration(func(ui map[string]interface{}) (map[string]interface{}, error) {
		if err := updateDefaultFilter(ui, view, action, filter); err != nil {
			return nil, err
		}
		return ui, nil
	})
}

func updateDefaultFilter(ui map[string]interface{}, view, action string, filter *models.SavedFilter) error {
	defaults, _ := stringMap(ui["defaultFilters"])
	if defaults == nil {
		defaults = make(map[string]interface{})
	}
	states, _ := stringMap(ui[forkDefaultFilterStateKey])
	if states == nil {
		states = make(map[string]interface{})
	}

	switch action {
	case "CLEAR":
		delete(defaults, view)
		delete(states, view)
	case "SET":
		if filter == nil || !filter.Mode.IsValid() {
			return errors.New("SET requires a filter with a valid mode")
		}
		var canonical *models.FilterAST
		if filter.FilterAST != nil {
			var err error
			canonical, err = filter.FilterAST.Normalize()
			if err != nil {
				return fmt.Errorf("invalid default-filter AST: %w", err)
			}
		}
		// Encode to the existing config shape rather than persisting Go structs.
		encoded, err := json.Marshal(map[string]interface{}{
			"mode": filter.Mode, "find_filter": filter.FindFilter, "ui_options": filter.UIOptions,
		})
		if err != nil {
			return err
		}
		var entry map[string]interface{}
		if err := json.Unmarshal(encoded, &entry); err != nil {
			return err
		}
		if err := storeDefaultFilter(defaults, states, view, entry, canonical); err != nil {
			return err
		}
	case "USE_LEGACY", "KEEP_V3":
		// Reconcile only this view: an unrelated malformed default must not
		// prevent the user from resolving or clearing this one.
		current := map[string]interface{}{
			"defaultFilters":          map[string]interface{}{view: defaults[view]},
			forkDefaultFilterStateKey: map[string]interface{}{view: states[view]},
		}
		if _, err := reconcileDefaultFilterConfig(current); err != nil {
			return err
		}
		entry, _ := stringMap(defaults[view])
		currentStates, _ := stringMap(current[forkDefaultFilterStateKey])
		state, _ := stringMap(currentStates[view])
		if entry == nil || state == nil {
			return errors.New("default-filter conflict no longer exists")
		}
		pending, exists := state["pending_legacy_object_filter"]
		if !exists {
			return errors.New("default-filter conflict no longer exists")
		}
		canonical, err := decodeDefaultFilterAST(state["filter_ast"])
		if action == "USE_LEGACY" {
			legacy, _ := stringMap(pending)
			canonical, err = models.FilterASTFromLegacySavedFilter(legacy)
		}
		if err != nil {
			return err
		}
		if err := storeDefaultFilter(defaults, states, view, entry, canonical); err != nil {
			return err
		}
	default:
		return errors.New("invalid default-filter action")
	}

	if len(defaults) == 0 {
		delete(ui, "defaultFilters")
	} else {
		ui["defaultFilters"] = defaults
	}
	if len(states) == 0 {
		delete(ui, forkDefaultFilterStateKey)
	} else {
		ui[forkDefaultFilterStateKey] = states
	}
	return nil
}

func storeDefaultFilter(defaults, states map[string]interface{}, view string, entry map[string]interface{}, canonical *models.FilterAST) error {
	encoded, err := encodeDefaultFilterAST(canonical)
	if err != nil {
		return err
	}
	legacy, _ := canonical.FlatObjectFilter()
	entry["object_filter"] = legacy
	entry["filter_ast"] = encoded
	defaults[view] = entry
	states[view] = map[string]interface{}{
		"filter_ast":           encoded,
		"legacy_object_filter": legacy,
	}
	return nil
}
