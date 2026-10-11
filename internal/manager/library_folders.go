package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

const libraryFolderMigrationName = "library-folders-v1"

// LibraryPaths reads one database snapshot for an operation. Disabled folders
// remain registered, so turning scanning off cannot make cleanup forget them.
func LibraryPaths(ctx context.Context, repo models.Repository) (config.StashConfigs, error) {
	var ret config.StashConfigs
	err := repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		ret, err = ReadLibraryPaths(ctx, repo)
		return err
	})
	return ret, err
}

// ReadLibraryPaths is for callers already inside a repository transaction.
func ReadLibraryPaths(ctx context.Context, repo models.Repository) (config.StashConfigs, error) {
	roots, err := libraryRoots(ctx, repo)
	if err != nil {
		return nil, err
	}
	ret := config.StashConfigs{}
	for _, root := range roots {
		if path := root.LocalPath(); path != "" {
			ret = append(ret, &config.StashConfig{Path: path, ExcludeVideo: root.Scan.ExcludeVideo, ExcludeImage: root.Scan.ExcludeImage,
				ScanDisabled: root.State != "active" || root.Binding == nil || !root.Scan.Enabled})
		}
	}
	return ret, nil
}

func libraryRoots(ctx context.Context, repo models.Repository) ([]*models.MediaRoot, error) {
	var ret []*models.MediaRoot
	after := ""
	for {
		roots, err := repo.MediaRoot.List(ctx, after, 100)
		if err != nil {
			return nil, err
		}
		ret = append(ret, roots...)
		if len(roots) < 100 {
			return ret, nil
		}
		after = roots[len(roots)-1].UUID
	}
}

// PromoteLibraryFolderConfig imports the old list exactly once, reusing roots
// by local path. Commit the records before removing the writable YAML list;
// the checkpoint makes interruption safe without ever reapplying old settings.
func (s *Manager) PromoteLibraryFolderConfig(ctx context.Context) error {
	var checkpoint *models.ConfigurationMigration
	err := s.Repository.WithTxn(ctx, func(ctx context.Context) error {
		var err error
		checkpoint, err = s.Repository.ConfigurationMigration.Find(ctx, libraryFolderMigrationName)
		if err != nil || checkpoint != nil {
			return err
		}
		source, err := s.Config.LibraryFolderImportSource()
		if err != nil {
			return err
		}
		var folders config.StashConfigs
		if err := json.Unmarshal([]byte(source), &folders); err != nil {
			return err
		}
		roots, err := libraryRoots(ctx, s.Repository)
		if err != nil {
			return err
		}
		seen := map[string]*config.StashConfig{}
		for _, folder := range folders {
			if folder == nil || !filepath.IsAbs(folder.Path) {
				return fmt.Errorf("library folder must have an absolute path")
			}
			path := filepath.Clean(folder.Path)
			if prior := seen[path]; prior != nil {
				if prior.ExcludeVideo != folder.ExcludeVideo || prior.ExcludeImage != folder.ExcludeImage {
					return fmt.Errorf("conflicting scan options for library folder %q", path)
				}
				continue
			}
			seen[path] = folder
			var matched *models.MediaRoot
			for _, root := range roots {
				if root.LocalPath() == path {
					if matched != nil {
						return fmt.Errorf("multiple roots already use library folder %q", path)
					}
					matched = root
				}
			}
			input := models.MediaRootInput{Origin: "migration", Reason: "Import library scan options",
				MediaRootDefinition: models.MediaRootDefinition{Label: filepath.Base(path), State: "active", UnboundPath: path}}
			if matched != nil {
				input.UUID, input.ExpectedRevision = matched.UUID, matched.Revision
				input.MediaRootDefinition = matched.MediaRootDefinition
			} else if binding, err := archive.ProbeMediaRoot(path); err == nil {
				input.Binding, input.UnboundPath = binding, ""
			}
			scanRevision := 0
			if matched != nil {
				scanRevision = matched.Scan.Revision
			}
			input.Scan = &models.MediaRootScan{Revision: scanRevision, Enabled: true, ExcludeVideo: folder.ExcludeVideo, ExcludeImage: folder.ExcludeImage}
			if _, err := s.Repository.MediaRoot.Put(ctx, input); err != nil {
				return err
			}
		}
		checkpoint = &models.ConfigurationMigration{Name: libraryFolderMigrationName, SourceJSON: source, TargetJSON: "{}", State: "prepared"}
		return s.Repository.ConfigurationMigration.Create(ctx, checkpoint)
	})
	if err != nil {
		return fmt.Errorf("importing library folders: %w", err)
	}
	if err := s.Config.PublishLibraryFolderImport(checkpoint.SourceJSON); err != nil {
		return err
	}
	if checkpoint.State == "published" {
		return nil
	}
	return s.Repository.WithTxn(ctx, func(ctx context.Context) error {
		return s.Repository.ConfigurationMigration.Publish(ctx, libraryFolderMigrationName)
	})
}

func (s *Manager) PromoteConfiguration(ctx context.Context) error {
	if err := s.PromoteDefaultFilterConfig(ctx); err != nil {
		return err
	}
	return s.PromoteLibraryFolderConfig(ctx)
}
