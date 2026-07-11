package task

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
)

type uiConfigStore interface {
	GetUIConfiguration() map[string]interface{}
	SetUIConfiguration(map[string]interface{})
	Write() error
}

type legacyDefaultFilter struct {
	key          string
	filter       map[string]interface{}
	objectFilter map[string]interface{}
}

type MigrateLegacySavedFiltersJob struct {
	Repository models.Repository
	Config     uiConfigStore
}

func (j *MigrateLegacySavedFiltersJob) Execute(ctx context.Context, progress *job.Progress) error {
	var (
		savedFilters   []*models.SavedFilter
		uiConfig       map[string]interface{}
		defaultFilters []legacyDefaultFilter
		err            error
	)

	progress.ExecuteTask("Finding legacy saved filters", func() {
		savedFilters, err = j.findLegacySavedFilters(ctx)
	})
	if err != nil {
		return fmt.Errorf("finding legacy saved filters: %w", err)
	}

	progress.ExecuteTask("Finding legacy default filters", func() {
		uiConfig, defaultFilters, err = j.findLegacyDefaultFilters()
	})
	if err != nil {
		return fmt.Errorf("finding legacy default filters: %w", err)
	}

	progress.SetTotal(len(savedFilters) + len(defaultFilters))
	if len(savedFilters) == 0 && len(defaultFilters) == 0 {
		logger.Infof("No legacy saved filters to migrate")
		return nil
	}

	migratedSaved, failedSaved := j.migrateSavedFilters(ctx, progress, savedFilters)
	if job.IsCancelled(ctx) {
		logger.Infof("Cancelled legacy saved filter migration")
		return nil
	}

	migratedDefaults, failedDefaults, err := j.migrateDefaultFilters(ctx, progress, uiConfig, defaultFilters)
	if err != nil {
		return err
	}

	failed := failedSaved + failedDefaults
	logger.Infof(
		"Finished legacy saved filter migration: %d saved filter(s) and %d default filter(s) migrated, %d failed",
		migratedSaved,
		migratedDefaults,
		failed,
	)
	if failed > 0 {
		return fmt.Errorf("%d legacy saved filter(s) failed to migrate; see logs for details", failed)
	}

	return nil
}

func (j *MigrateLegacySavedFiltersJob) findLegacySavedFilters(ctx context.Context) ([]*models.SavedFilter, error) {
	var ret []*models.SavedFilter
	if err := j.Repository.WithReadTxn(ctx, func(ctx context.Context) error {
		filters, err := j.Repository.SavedFilter.All(ctx)
		if err != nil {
			return err
		}

		for _, filter := range filters {
			if filter == nil || filter.FilterAST != nil || len(filter.ObjectFilter) == 0 {
				continue
			}
			ret = append(ret, filter)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func (j *MigrateLegacySavedFiltersJob) migrateSavedFilters(ctx context.Context, progress *job.Progress, filters []*models.SavedFilter) (migrated int, failed int) {
	for _, filter := range filters {
		if job.IsCancelled(ctx) {
			return migrated, failed
		}

		description := fmt.Sprintf("Migrating saved filter %q", filter.Name)
		progress.ExecuteTask(description, func() {
			defer progress.Increment()

			changed, err := j.migrateSavedFilter(ctx, filter.ID)
			if err != nil {
				failed++
				logger.Warnf("saved filter %d (%q): leaving unconverted: %v", filter.ID, filter.Name, err)
				return
			}

			if changed {
				migrated++
			}
		})
	}

	return migrated, failed
}

func (j *MigrateLegacySavedFiltersJob) migrateSavedFilter(ctx context.Context, id int) (bool, error) {
	var changed bool
	err := j.Repository.WithTxn(ctx, func(ctx context.Context) error {
		filter, err := j.Repository.SavedFilter.Find(ctx, id)
		if err != nil {
			return err
		}
		if filter == nil || filter.FilterAST != nil || len(filter.ObjectFilter) == 0 {
			return nil
		}

		ast, err := models.FilterASTFromLegacySavedFilter(filter.ObjectFilter)
		if err != nil {
			return err
		}
		if ast == nil {
			return nil
		}

		filter.FilterAST = ast
		filter.ObjectFilter = nil
		changed = true
		return j.Repository.SavedFilter.Update(ctx, filter)
	})

	return changed, err
}

func (j *MigrateLegacySavedFiltersJob) findLegacyDefaultFilters() (map[string]interface{}, []legacyDefaultFilter, error) {
	if j.Config == nil {
		return nil, nil, nil
	}

	uiConfig, err := cloneStringMap(j.Config.GetUIConfiguration())
	if err != nil {
		return nil, nil, err
	}
	if uiConfig == nil {
		return nil, nil, nil
	}

	defaultFilters, ok := asStringMap(uiConfig["defaultFilters"])
	if !ok {
		return uiConfig, nil, nil
	}

	var ret []legacyDefaultFilter
	for key, raw := range defaultFilters {
		filter, ok := asStringMap(raw)
		if !ok || hasValue(filter["filter_ast"]) {
			continue
		}

		objectFilter, ok := asStringMap(filter["object_filter"])
		if !ok || len(objectFilter) == 0 {
			continue
		}

		ret = append(ret, legacyDefaultFilter{
			key:          key,
			filter:       filter,
			objectFilter: objectFilter,
		})
	}

	return uiConfig, ret, nil
}

func (j *MigrateLegacySavedFiltersJob) migrateDefaultFilters(ctx context.Context, progress *job.Progress, uiConfig map[string]interface{}, filters []legacyDefaultFilter) (migrated int, failed int, err error) {
	if len(filters) == 0 || j.Config == nil {
		return 0, 0, nil
	}

	for _, filter := range filters {
		if job.IsCancelled(ctx) {
			return migrated, failed, nil
		}

		description := fmt.Sprintf("Migrating default filter %q", filter.key)
		progress.ExecuteTask(description, func() {
			defer progress.Increment()

			ast, convErr := models.FilterASTFromLegacySavedFilter(filter.objectFilter)
			if convErr != nil {
				failed++
				logger.Warnf("default filter %q: leaving unconverted: %v", filter.key, convErr)
				return
			}
			if ast == nil {
				return
			}

			astMap, convErr := filterASTToMap(ast)
			if convErr != nil {
				failed++
				logger.Warnf("default filter %q: leaving unconverted: %v", filter.key, convErr)
				return
			}

			filter.filter["filter_ast"] = astMap
			migrated++
		})
	}

	if migrated == 0 {
		return migrated, failed, nil
	}

	j.Config.SetUIConfiguration(uiConfig)
	if err := j.Config.Write(); err != nil {
		return migrated, failed, fmt.Errorf("writing UI configuration: %w", err)
	}

	return migrated, failed, nil
}

func hasValue(v interface{}) bool {
	if v == nil {
		return false
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s) != ""
	}
	return true
}

func asStringMap(raw interface{}) (map[string]interface{}, bool) {
	if raw == nil {
		return nil, false
	}

	if ret, ok := raw.(map[string]interface{}); ok {
		return ret, true
	}

	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}

	var ret map[string]interface{}
	if err := json.Unmarshal(encoded, &ret); err != nil {
		return nil, false
	}

	return ret, true
}

func cloneStringMap(raw map[string]interface{}) (map[string]interface{}, error) {
	if raw == nil {
		return nil, nil
	}

	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}

	var ret map[string]interface{}
	if err := json.Unmarshal(encoded, &ret); err != nil {
		return nil, err
	}

	return ret, nil
}

func filterASTToMap(ast *models.FilterAST) (map[string]interface{}, error) {
	encoded, err := json.Marshal(ast)
	if err != nil {
		return nil, err
	}

	var ret map[string]interface{}
	if err := json.Unmarshal(encoded, &ret); err != nil {
		return nil, err
	}

	return ret, nil
}
