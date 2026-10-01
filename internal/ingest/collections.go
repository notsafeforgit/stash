package ingest

import (
	"context"
	"net/url"
	"reflect"
	"strings"
	"unicode"

	"github.com/stashapp/stash/pkg/models"
)

// LookupCollections returns current definitions visible to the producer. A URL
// need not be unique: callers must resolve ambiguity before scheduling work.
func (s *Service) LookupCollections(ctx context.Context, token string, root *string, targets []string) ([]*models.SourceCollection, error) {
	if len(targets) < 1 || len(targets) > 50 || (root != nil && !ValidUUID(*root)) {
		return nil, ErrInvalid
	}
	seen := make(map[string]bool, len(targets))
	for _, target := range targets {
		parsed, err := url.Parse(target)
		if err != nil || seen[target] || len(target) > 8192 || strings.TrimSpace(target) != target || strings.ContainsFunc(target, unicode.IsControl) ||
			(parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil {
			return nil, ErrInvalid
		}
		seen[target] = true
	}
	var result []*models.SourceCollection
	err := s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		credential, err := s.authenticate(ctx, token)
		if err != nil {
			return err
		}
		collections := make([]string, 0, len(credential.Scopes))
		for _, scope := range credential.Scopes {
			if reflect.DeepEqual(scope.RootUUID, root) {
				collections = append(collections, scope.CollectionUUID)
			}
		}
		if len(collections) == 0 {
			return ErrForbidden
		}
		result, err = s.Repo.SourceCollection.LookupCurrentTargets(ctx, targets, collections, root)
		return err
	})
	return result, err
}
