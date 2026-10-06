package ingest

import (
	"context"
	"fmt"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type ManualDirectoryPage struct {
	CollectionUUID     string `json:"collection_uuid"`
	CollectionRevision int    `json:"collection_revision"`
	RootUUID           string `json:"root_uuid"`
	RootRevision       int    `json:"root_revision"`
	PathPrefix         string `json:"path_prefix"`
	*archive.MediaDirectoryPage
}

func (s *Service) ManualDirectory(ctx context.Context, collectionUUID string, input archive.MediaDirectoryInput) (*ManualDirectoryPage, error) {
	if !ValidUUID(collectionUUID) {
		return nil, ErrInvalid
	}
	var collection *models.SourceCollection
	var root *models.MediaRoot
	err := s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		collection, err = s.Repo.SourceCollection.Find(ctx, collectionUUID)
		if err != nil {
			return err
		}
		if collection == nil {
			return ErrNotFound
		}
		if collection.RootUUID == nil || collection.State != "active" || (collection.Kind != "directory" && collection.Kind != "manual_batch") {
			return ErrDefinition
		}
		if input.Directory == "" {
			input.Directory = collection.PathPrefix
		}
		if !archive.ValidRootRelativePath(input.Directory, true) || (collection.PathPrefix != "." && input.Directory != collection.PathPrefix && !strings.HasPrefix(input.Directory, collection.PathPrefix+"/")) {
			return ErrInvalid
		}
		root, err = s.Repo.MediaRoot.Find(ctx, *collection.RootUUID)
		if err != nil {
			return err
		}
		if root == nil || root.State != "active" || root.Binding == nil {
			return ErrDefinition
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	input.Scope = fmt.Sprintf("%s:%d", collection.UUID, collection.Revision)
	page, err := archive.ReadMediaDirectory(ctx, *root, input)
	if err != nil {
		return nil, err
	}
	// Filesystem inspection does not keep an SQLite transaction open. Recheck
	// the exact definitions before exposing the completed page.
	err = s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		current, err := s.Repo.SourceCollection.Find(ctx, collectionUUID)
		if err != nil {
			return err
		}
		currentRoot, err := s.Repo.MediaRoot.Find(ctx, root.UUID)
		if err != nil {
			return err
		}
		if current == nil || currentRoot == nil || current.Revision != collection.Revision || currentRoot.Revision != root.Revision {
			return ErrDefinition
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &ManualDirectoryPage{CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, RootUUID: root.UUID,
		RootRevision: root.Revision, PathPrefix: collection.PathPrefix, MediaDirectoryPage: page}, nil
}
