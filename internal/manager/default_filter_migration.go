package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
)

const defaultFilterMigrationName = "default-filters-v1"
const forkDefaultFilterStateKey = "forkDefaultFilterState" // one-time input only

// PromoteDefaultFilterConfig resumes an interrupted publication without
// importing the source twice or overwriting later native edits.
func (s *Manager) PromoteDefaultFilterConfig(ctx context.Context) error {
	var checkpoint *models.ConfigurationMigration
	if err := s.Repository.WithTxn(ctx, func(ctx context.Context) error {
		var err error
		checkpoint, err = s.Repository.ConfigurationMigration.Find(ctx, defaultFilterMigrationName)
		if err != nil || checkpoint != nil {
			return err
		}
		source, err := config.DefaultFilterImportSource(s.Config.GetUIConfiguration())
		if err != nil {
			return err
		}
		defaults, conflicts, err := importDefaultFilterConfig(source)
		if err != nil {
			return err
		}
		target, err := json.Marshal(defaults)
		if err != nil {
			return err
		}
		checkpoint = &models.ConfigurationMigration{Name: defaultFilterMigrationName, SourceJSON: source, TargetJSON: string(target), State: "prepared"}
		if err := s.Repository.ConfigurationMigration.Create(ctx, checkpoint); err != nil {
			return err
		}
		for _, view := range sortedDefaultViews(defaults) {
			if existing, err := s.Repository.DefaultFilter.Find(ctx, view); err != nil {
				return err
			} else if existing != nil {
				return fmt.Errorf("default filter %q already exists before configuration import", view)
			}
			if err := s.Repository.DefaultFilter.Set(ctx, view, defaults[view]); err != nil {
				return err
			}
		}
		for _, conflict := range conflicts {
			if err := s.Repository.DefaultFilter.CreateConflict(ctx, conflict); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("staging native default filters: %w", err)
	}
	if err := s.Config.PublishDefaultFilterImport(checkpoint.SourceJSON); err != nil {
		return err
	}
	if checkpoint.State == "published" {
		return nil
	}
	return s.Repository.WithTxn(ctx, func(ctx context.Context) error {
		return s.Repository.ConfigurationMigration.Publish(ctx, defaultFilterMigrationName)
	})
}

func sortedDefaultViews(defaults map[string]*models.SavedFilter) []string {
	ret := make([]string, 0, len(defaults))
	for view := range defaults {
		ret = append(ret, view)
	}
	sort.Strings(ret)
	return ret
}

func importDefaultFilterConfig(source string) (map[string]*models.SavedFilter, []*models.DefaultFilterConflict, error) {
	var ui map[string]interface{}
	if err := json.Unmarshal([]byte(source), &ui); err != nil {
		return nil, nil, err
	}
	defaults, ok := stringMap(ui["defaultFilters"])
	if !ok && ui["defaultFilters"] != nil {
		return nil, nil, errors.New("defaultFilters must be an object")
	}
	states, ok := stringMap(ui[forkDefaultFilterStateKey])
	if !ok && ui[forkDefaultFilterStateKey] != nil {
		return nil, nil, errors.New("historical default-filter state must be an object")
	}
	result := make(map[string]*models.SavedFilter)
	var conflicts []*models.DefaultFilterConflict
	for view, raw := range defaults {
		if raw == nil {
			continue
		} // an explicitly cleared historical default
		if !defaultFilterViewPattern.MatchString(view) {
			return nil, nil, fmt.Errorf("invalid default-filter view %q", view)
		}
		entry, ok := stringMap(raw)
		if !ok {
			return nil, nil, fmt.Errorf("default filter %q must be an object", view)
		}
		filter, err := decodeImportedDefaultFilter(entry)
		if err != nil {
			return nil, nil, fmt.Errorf("decoding default filter %q: %w", view, err)
		}
		if !filter.Mode.IsValid() {
			return nil, nil, fmt.Errorf("default filter %q has an invalid mode", view)
		}
		state, ok := stringMap(states[view])
		if !ok && states[view] != nil {
			return nil, nil, fmt.Errorf("default filter %q has malformed historical state", view)
		}
		canonical, err := decodeDefaultFilterAST(state["filter_ast"])
		if err != nil {
			return nil, nil, fmt.Errorf("default filter %q: %w", view, err)
		}
		if canonical == nil {
			canonical, err = decodeDefaultFilterAST(entry["filter_ast"])
			if err != nil {
				return nil, nil, fmt.Errorf("default filter %q: %w", view, err)
			}
		}
		legacy := filter.ObjectFilter
		if canonical == nil {
			canonical, err = models.FilterASTFromLegacySavedFilter(legacy)
			if err != nil {
				return nil, nil, fmt.Errorf("importing default filter %q: %w", view, err)
			}
		}
		pending, hasPending := state["pending_legacy_object_filter"]
		shadow, hasShadow := stringMap(state["legacy_object_filter"])
		if hasShadow && !sameLegacyCriteria(legacy, shadow) {
			imported, importErr := models.FilterASTFromLegacySavedFilter(legacy)
			if canonical == nil || (canonical.IsFlatRepresentable() && importErr == nil) {
				canonical, hasPending = imported, false
			} else {
				pending, hasPending = legacy, true
			}
		}
		filter.FilterAST, filter.ObjectFilter = canonical, nil
		if canonical != nil {
			filter.FilterAST, err = canonical.Normalize()
			if err != nil {
				return nil, nil, fmt.Errorf("invalid default filter %q: %w", view, err)
			}
		}
		result[view] = filter
		if hasPending {
			evidence, err := json.Marshal(map[string]interface{}{"entry": entry, "state": state})
			if err != nil {
				return nil, nil, err
			}
			c := &models.DefaultFilterConflict{View: view, MigrationName: defaultFilterMigrationName, Evidence: string(evidence)}
			alternative, valid := stringMap(pending)
			if !valid && pending != nil {
				c.ImportError = "imported criteria must be an object"
			} else {
				c.Alternative, err = models.FilterASTFromLegacySavedFilter(alternative)
				if err != nil {
					c.ImportError = err.Error()
					c.Alternative = nil
				}
			}
			conflicts = append(conflicts, c)
		}
	}
	return result, conflicts, nil
}

func decodeImportedDefaultFilter(entry map[string]interface{}) (*models.SavedFilter, error) {
	// Old config writers stored pagination as either JSON numbers or strings.
	// Normalize only these known fields; keep the exact source in the checkpoint.
	input := maps.Clone(entry)
	if find, ok := stringMap(entry["find_filter"]); ok {
		find = maps.Clone(find)
		for _, key := range []string{"page", "per_page"} {
			if value, ok := find[key].(string); ok {
				number, err := strconv.ParseInt(strings.TrimSpace(value), 10, 32)
				if err != nil {
					return nil, fmt.Errorf("find_filter.%s must be an integer: %w", key, err)
				}
				find[key] = number
			}
		}
		input["find_filter"] = find
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var filter models.SavedFilter
	if err := json.Unmarshal(data, &filter); err != nil {
		return nil, err
	}
	return &filter, nil
}

func sameLegacyCriteria(a, b map[string]interface{}) bool {
	return (len(a) == 0 && len(b) == 0) || reflect.DeepEqual(a, b)
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
	var ast models.FilterAST
	if err := json.Unmarshal(encoded, &ast); err != nil {
		return nil, err
	}
	return ast.Normalize()
}
