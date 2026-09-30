package manager

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/stashapp/stash/pkg/models"
)

var defaultFilterViewPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// UIConfiguration overlays the native default-filter records onto ordinary UI
// preferences. These derived fields are never written back to the config file.
func (s *Manager) UIConfiguration(ctx context.Context) (map[string]interface{}, error) {
	ui := s.Config.GetUIConfiguration()
	if ui == nil {
		ui = make(map[string]interface{})
	}
	if s.Database == nil || s.Database.Ready() != nil {
		return ui, nil
	} // setup/migration screen
	delete(ui, "defaultFilters")
	delete(ui, forkDefaultFilterStateKey)
	delete(ui, "defaultFilterConflicts")
	err := s.Repository.WithReadTxn(ctx, func(ctx context.Context) error {
		filters, err := s.Repository.DefaultFilter.All(ctx)
		if err != nil {
			return err
		}
		defaults := make(map[string]interface{})
		for _, filter := range filters {
			if !filter.Enabled {
				continue
			}
			value, err := defaultFilterUIEntry(filter)
			if err != nil {
				return err
			}
			defaults[filter.View] = value
		}
		if len(defaults) > 0 {
			ui["defaultFilters"] = defaults
		}
		conflicts, err := s.Repository.DefaultFilter.Conflicts(ctx)
		if err != nil {
			return err
		}
		pending := make(map[string]interface{})
		for _, conflict := range conflicts {
			pending[conflict.View] = map[string]interface{}{"revision": conflict.Revision, "import_error": conflict.ImportError}
		}
		if len(pending) > 0 {
			ui["defaultFilterConflicts"] = pending
		}
		return nil
	})
	return ui, err
}

func defaultFilterUIEntry(filter *models.DefaultFilter) (map[string]interface{}, error) {
	data, err := json.Marshal(map[string]interface{}{
		"mode": filter.Filter.Mode, "find_filter": filter.Filter.FindFilter,
		"filter_ast": filter.Filter.FilterAST, "ui_options": filter.Filter.UIOptions,
		"revision": filter.Revision,
	})
	if err != nil {
		return nil, err
	}
	var ret map[string]interface{}
	err = json.Unmarshal(data, &ret)
	return ret, err
}

// ConfigureDefaultFilter updates one view. Resolving imported alternatives
// requires the revision the user reviewed, so a stale action cannot replace a
// default that changed in another session.
func (s *Manager) ConfigureDefaultFilter(ctx context.Context, view, action string, filter *models.SavedFilter, expectedRevision *int) (map[string]interface{}, error) {
	if !defaultFilterViewPattern.MatchString(view) {
		return nil, errors.New("invalid default-filter view")
	}
	err := s.Repository.WithTxn(ctx, func(ctx context.Context) error {
		store := s.Repository.DefaultFilter
		current, err := store.Find(ctx, view)
		if err != nil {
			return err
		}
		revision := 0
		if current != nil {
			revision = current.Revision
		}
		if expectedRevision != nil && *expectedRevision != revision {
			return errors.New("default filter changed; reload before applying this action")
		}
		switch action {
		case "CLEAR":
			if err := store.Clear(ctx, view); err != nil {
				return err
			}
			return store.ResolveConflict(ctx, view, "cleared")
		case "SET":
			if err := store.Set(ctx, view, filter); err != nil {
				return err
			}
			return store.ResolveConflict(ctx, view, "replaced")
		case "USE_IMPORTED", "KEEP_CURRENT":
			if expectedRevision == nil {
				return errors.New("conflict resolution requires the reviewed revision")
			}
			conflicts, err := store.Conflicts(ctx)
			if err != nil {
				return err
			}
			var pending *models.DefaultFilterConflict
			for _, c := range conflicts {
				if c.View == view {
					pending = c
					break
				}
			}
			if current == nil || pending == nil {
				return errors.New("default-filter conflict no longer exists")
			}
			selection := "kept-current"
			if action == "USE_IMPORTED" {
				if pending.ImportError != "" {
					return errors.New("imported criteria are invalid; keep the current default or set a new one")
				}
				current.Filter.FilterAST = pending.Alternative
				selection = "used-imported"
			}
			if err := store.Set(ctx, view, &current.Filter); err != nil {
				return err
			}
			return store.ResolveConflict(ctx, view, selection)
		default:
			return errors.New("invalid default-filter action")
		}
	})
	if err != nil {
		return nil, err
	}
	return s.UIConfiguration(ctx)
}
