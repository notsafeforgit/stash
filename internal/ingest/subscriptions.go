package ingest

import (
	"context"
	"errors"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

// Subscriptions exposes monitoring intent through the same scoped producer API
// as run admission. A downloader cannot change subscriptions or read other roots.
func (s *Service) Subscriptions(ctx context.Context, token, root, namespace, after string) (*models.SourceSubscriptionPage, error) {
	var result *models.SourceSubscriptionPage
	err := s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		credential, err := s.authenticate(ctx, token)
		if err != nil {
			return err
		}
		if !ValidUUID(root) || namespace == "" {
			return ErrInvalid
		}
		filter := models.SourceSubscriptionFilter{RootUUID: root, Namespace: namespace, EnabledOnly: true, After: after, Limit: 100}
		if strings.HasSuffix(namespace, ":*") {
			filter.Namespace, filter.NamespacePrefix = "", strings.TrimSuffix(namespace, "*")
		}
		if !permittedRoot(credential, root) {
			filter.AllowedCollections = []string{}
			for _, scope := range credential.Scopes {
				if scope.RootUUID != nil && *scope.RootUUID == root {
					filter.AllowedCollections = append(filter.AllowedCollections, scope.CollectionUUID)
				}
			}
			if len(filter.AllowedCollections) == 0 {
				return ErrForbidden
			}
		}
		result, err = s.Repo.SourceSubscription.List(ctx, filter)
		if errors.Is(err, models.ErrSourceDefinitionInvalid) {
			return ErrInvalid
		}
		return err
	})
	return result, err
}
