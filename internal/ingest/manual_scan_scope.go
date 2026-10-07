package ingest

import (
	"context"
	"encoding/json"
	"path"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// ManualScanScope chooses an automatic intake policy within a configured manual
// scope. Explicit interactive imports remain independent of automatic discovery.
type ManualScanScope struct {
	ScanCollectionUUID     string `json:"scan_collection_uuid"`
	ScanCollectionRevision int    `json:"scan_collection_revision"`
	ScanPathPrefix         string `json:"scan_path_prefix"`
	RootUUID               string `json:"root_uuid"`
	RootRevision           int    `json:"root_revision"`
	Directory              string `json:"directory"`
	CollectionUUID         string `json:"collection_uuid,omitempty"`
	CollectionRevision     int    `json:"collection_revision,omitempty"`
	PathPrefix             string `json:"path_prefix,omitempty"`
	PolicyRevision         int    `json:"policy_revision"`
	BlockedReason          string `json:"blocked_reason,omitempty"`
}

func includesManualDirectory(prefix, directory string) bool {
	return prefix == "." || prefix == directory || strings.HasPrefix(directory, prefix+"/")
}

func manualDirectoryDepth(prefix string) int {
	if prefix == "." {
		return 0
	}
	return strings.Count(prefix, "/") + 1
}

func (s *Service) manualScanScope(ctx context.Context, collectionUUID, directory string) (*ManualScanScope, error) {
	if !ValidUUID(collectionUUID) || !archive.ValidRootRelativePath(directory, true) {
		return nil, ErrInvalid
	}
	base, err := s.Repo.SourceCollection.Find(ctx, collectionUUID)
	if err != nil {
		return nil, err
	}
	if base == nil {
		return nil, ErrNotFound
	}
	if base.State != "active" || base.RootUUID == nil || (base.Kind != "directory" && base.Kind != "manual_batch") {
		return nil, ErrDefinition
	}
	if !includesManualDirectory(base.PathPrefix, directory) {
		return nil, ErrInvalid
	}
	root, err := s.Repo.MediaRoot.Find(ctx, *base.RootUUID)
	if err != nil {
		return nil, err
	}
	if root == nil || root.State != "active" || root.Binding == nil {
		return nil, ErrDefinition
	}
	ret := &ManualScanScope{ScanCollectionUUID: base.UUID, ScanCollectionRevision: base.Revision,
		ScanPathPrefix: base.PathPrefix, RootUUID: root.UUID, RootRevision: root.Revision, Directory: directory}
	collections, err := s.Repo.SourceCollection.DirectoryScopes(ctx, root.UUID, directory)
	if err != nil {
		return nil, err
	}
	// A paused or retired scrape still owns its configured folder. It must not
	// become a manual import simply because its scraper is currently disabled.
	for _, collection := range collections {
		if collection.Kind != "directory" && collection.Kind != "manual_batch" {
			ret.BlockedReason = "source_folder"
			return ret, nil
		}
	}
	var selected *models.SourceCollection
	var selectedPolicy *models.MetadataPolicy
	ambiguous := false
	for _, collection := range collections {
		if collection.State != "active" || !includesManualDirectory(base.PathPrefix, collection.PathPrefix) {
			continue
		}
		policy, err := s.Repo.MetadataPolicy.Find(ctx, collection.UUID)
		if err != nil {
			return nil, err
		}
		if collection.UUID != base.UUID && (policy == nil || !policy.Definition.ApplyToScans) {
			continue
		}
		directPolicy := policy != nil && policy.Definition.ApplyToScans
		selectedDirect := selectedPolicy != nil && selectedPolicy.Definition.ApplyToScans
		if selected == nil || manualDirectoryDepth(collection.PathPrefix) > manualDirectoryDepth(selected.PathPrefix) ||
			(collection.PathPrefix == selected.PathPrefix && directPolicy && !selectedDirect) {
			selected, selectedPolicy, ambiguous = collection, policy, false
		} else if collection.PathPrefix == selected.PathPrefix && directPolicy == selectedDirect {
			ambiguous = true
		}
	}
	if selected == nil {
		return nil, ErrDefinition
	}
	if ambiguous {
		ret.BlockedReason = "ambiguous_directory"
		return ret, nil
	}
	if selectedPolicy != nil {
		if selectedPolicy.CollectionRevision != selected.Revision {
			ret.BlockedReason = "policy_changed"
			return ret, nil
		}
		if !selectedPolicy.Definition.ApplyToScans {
			ret.BlockedReason = "policy_not_for_scans"
			return ret, nil
		}
		ret.PolicyRevision = selectedPolicy.Revision
	}
	ret.CollectionUUID, ret.CollectionRevision, ret.PathPrefix = selected.UUID, selected.Revision, selected.PathPrefix
	return ret, nil
}

func (s *Service) ManualScanScope(ctx context.Context, collectionUUID, directory string) (*ManualScanScope, error) {
	var result *ManualScanScope
	err := s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		result, err = s.manualScanScope(ctx, collectionUUID, directory)
		return err
	})
	return result, err
}

func (s *Service) manualFileScanRevision(ctx context.Context, input ManualFileInput) (int, error) {
	if input.ScanCollectionUUID == "" {
		return 0, nil
	}
	scope, err := s.manualScanScope(ctx, input.ScanCollectionUUID, path.Dir(input.RelativePath))
	if err != nil {
		return 0, err
	}
	if scope.BlockedReason != "" || scope.CollectionUUID != input.CollectionUUID {
		return 0, ErrManualFileChanged
	}
	return scope.ScanCollectionRevision, nil
}

// A new destination may not have historical folder evidence yet. Producer
// admissions and exact source locations still reserve their files, including
// failed work that needs source recovery rather than a second manual import.
func (s *Service) checkAutomaticFileReservation(ctx context.Context, input ManualFileInput, target FileTarget) error {
	if input.ScanCollectionUUID == "" {
		return nil
	}
	observations, err := s.Repo.SourceFile.LocationObservations(ctx, target.RootUUID, nil, target.RelativePath, "", 100)
	if err != nil {
		return err
	}
	for _, observation := range observations {
		collection, err := s.Repo.SourceCollection.Find(ctx, observation.CollectionUUID)
		if err != nil {
			return err
		}
		if collection != nil && collection.RootUUID != nil && *collection.RootUUID == target.RootUUID && collection.Kind != "directory" && collection.Kind != "manual_batch" {
			return ErrManualFileChanged
		}
	}
	resource := target.PathFence.Path
	if !target.PathFence.CaseSensitive {
		resource = strings.ToLower(resource)
	}
	history, err := s.Repo.ArchiveJob.ResourceHistory(ctx, models.ArchiveJobVerifyMedia, Digest([]byte(resource)), 0, 100)
	if err != nil {
		return err
	}
	for _, job := range history {
		var work FileWork
		if err := json.Unmarshal(job.Arguments, &work); err != nil {
			return err
		}
		if work.Version == 1 {
			return ErrManualFileChanged
		}
	}
	if len(observations) == 100 || len(history) == 100 {
		return ErrManualFileChanged // Bound automatic decisions; explicit intake can review longer history.
	}
	return nil
}
