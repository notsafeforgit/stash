package sqlite

import (
	"context"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

// Collections uses current source bindings, not a historical listing's root.
// Each grant contributes at most limit candidates; the union is bounded by the
// credential's 128-grant limit and never loads definition or page bodies.
func (s *DiscoveryJobStore) Collections(ctx context.Context, q models.DiscoveryCollectionQuery) ([]models.DiscoveryCollectionCandidate, error) {
	if len(q.Scopes)+len(q.Roots) < 1 || len(q.Scopes)+len(q.Roots) > 128 || (q.After != "" && !validSourceRunUUID(q.After)) {
		return nil, models.ErrDiscoveryInvalid
	}
	limit, err := sourcePageLimit(q.Limit)
	if err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	const listing = " AND EXISTS(SELECT 1 FROM discovery_listings l WHERE l.collection_uuid=c.uuid)"
	const liveRoot = ` AND (d.root_uuid IS NULL OR EXISTS(SELECT 1 FROM media_roots r
 JOIN media_root_revisions rd ON rd.root_uuid=r.uuid AND rd.revision=r.revision
 WHERE r.uuid=d.root_uuid AND rd.state='active'))`
	parts := []string{}
	args := []any{}
	for _, scope := range q.Scopes {
		if !validSourceRunUUID(scope.CollectionUUID) || (scope.RootUUID != nil && !validSourceRunUUID(*scope.RootUUID)) {
			return nil, models.ErrDiscoveryInvalid
		}
		parts = append(parts, `SELECT c.uuid FROM source_collections c
 JOIN source_collection_revisions d ON d.collection_uuid=c.uuid AND d.revision=c.revision
 WHERE c.uuid=? AND c.uuid>? AND d.root_uuid IS ? AND d.state='active'`+liveRoot+listing)
		args = append(args, scope.CollectionUUID, q.After, scope.RootUUID)
	}
	for _, root := range q.Roots {
		if !validSourceRunUUID(root) {
			return nil, models.ErrDiscoveryInvalid
		}
		parts = append(parts, `SELECT uuid FROM (SELECT c.uuid FROM source_collection_revisions d INDEXED BY source_collection_root
 JOIN source_collections c ON c.uuid=d.collection_uuid AND c.revision=d.revision
 WHERE d.root_uuid=? AND d.collection_uuid>? AND d.state='active'`+liveRoot+listing+`
 ORDER BY d.collection_uuid LIMIT ?)`)
		args = append(args, root, q.After, limit)
	}
	ret := []models.DiscoveryCollectionCandidate{}
	query := "SELECT uuid FROM (" + strings.Join(parts, " UNION ") + ") ORDER BY uuid LIMIT ?"
	args = append(args, limit)
	if err := dbWrapper.Select(ctx, &ret, query, args...); err != nil {
		return nil, err
	}
	return ret, nil
}
