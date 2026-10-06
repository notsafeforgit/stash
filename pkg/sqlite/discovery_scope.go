package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// Original definitions remain the authority for historical activation/recovery
// receipts. Execution uses the latest reviewed scope. Older schema validation
// runs before migration and has no effective-listing view yet.
func resolveDiscoveryListing(get enrichmentGet, row discoveryListingRow) (*models.DiscoveryListing, error) {
	var present bool
	if err := get(&present, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name='discovery_effective_listings' AND type='view')"); err != nil {
		return nil, err
	}
	if present {
		if err := get(&row, "SELECT * FROM discovery_effective_listings WHERE uuid=?", row.UUID); err != nil {
			return nil, err
		}
	}
	return row.resolve()
}

// Historical receipts name a definition digest, which may precede the current
// scope. Its availability time is separate from the original listing's creation.
func discoveryListingAtDigest(get enrichmentGet, id, digest string) (*models.DiscoveryListing, time.Time, error) {
	var row discoveryListingRow
	if err := get(&row, "SELECT * FROM discovery_listings WHERE uuid=?", id); err != nil {
		return nil, time.Time{}, err
	}
	if row.Digest == digest {
		listing, err := row.resolve()
		return listing, row.CreatedAt, err
	}
	var present bool
	if err := get(&present, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name='discovery_scope_reviews' AND type='table')"); err != nil {
		return nil, time.Time{}, err
	}
	if !present {
		return nil, time.Time{}, models.ErrDiscoveryConflict
	}
	var scoped struct {
		discoveryListingRow
		ReviewedAt time.Time `db:"reviewed_at"`
	}
	if err := get(&scoped, `SELECT d.uuid,d.account_uuid,d.collection_uuid,r.collection_revision,r.root_uuid,r.definition,r.digest,d.created_at,r.created_at AS reviewed_at
 FROM discovery_listings d JOIN discovery_scope_reviews r ON r.listing_uuid=d.uuid WHERE d.uuid=? AND r.digest=?`, id, digest); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, time.Time{}, models.ErrDiscoveryConflict
		}
		return nil, time.Time{}, err
	}
	listing, err := scoped.resolve()
	return listing, scoped.ReviewedAt, err
}

func discoveryScopeListing(plan *models.DiscoveryScopePlan) *models.DiscoveryListing {
	ret := plan.Previous
	ret.CollectionRevision, ret.RootUUID = plan.Collection.Revision, plan.Collection.RootUUID
	ret.Digest = plan.DefinitionSHA256
	return &ret
}

func discoveryScopeCollectionCompatible(previous, current models.SourceCollectionDefinition) bool {
	return previous.Kind == current.Kind && previous.Namespace == current.Namespace && previous.TargetURL == current.TargetURL &&
		reflect.DeepEqual(previous.AccountUUID, current.AccountUUID)
}

func discoveryScopeCollection(get enrichmentGet, id string, revision int) (*models.SourceCollection, error) {
	var row sourceCollectionRow
	if err := get(&row, sourceCollectionSelect+" WHERE b.uuid=? AND r.revision=?", id, revision); err != nil {
		return nil, err
	}
	return row.resolve(), nil
}

func discoveryScopeDisposition(ctx context.Context, listing *models.DiscoveryListing, collection *models.SourceCollection) (string, error) {
	if collection.State != "active" {
		return "collection_disabled", nil
	}
	if listing.CollectionUUID != collection.UUID || listing.CollectionRevision >= collection.Revision {
		return "collection_changed", nil
	}
	get := func(out any, query string, args ...any) error { return dbWrapper.Get(ctx, out, query, args...) }
	previous, err := discoveryScopeCollection(get, listing.CollectionUUID, listing.CollectionRevision)
	if err != nil {
		return "", err
	}
	if !discoveryScopeCollectionCompatible(previous.SourceCollectionDefinition, collection.SourceCollectionDefinition) {
		return "source_changed", nil
	}
	replacement, err := discoveryReplacement(get, listing.UUID)
	if err != nil {
		return "", err
	}
	if replacement != "" {
		return "search_replaced", nil
	}
	var attempted bool
	if err := get(&attempted, `SELECT EXISTS(SELECT 1 FROM discovery_listing_jobs WHERE listing_uuid=?)
 OR EXISTS(SELECT 1 FROM discovery_detail_jobs j JOIN discovery_match_targets t ON t.uuid=j.target_uuid WHERE t.listing_uuid=?)`, listing.UUID, listing.UUID); err != nil {
		return "", err
	}
	if attempted {
		return "worker_history", nil
	}
	return "eligible", nil
}

func (s *DiscoveryJobStore) ScopeCandidates(ctx context.Context, id string, revision int, after string, limit int) ([]models.DiscoveryScopeCandidate, error) {
	if !validSourceRunUUID(id) || revision < 1 || (after != "" && !validSourceRunUUID(after)) || limit < 1 || limit > 100 {
		return nil, models.ErrDiscoveryInvalid
	}
	collection, err := (&SourceCollectionStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if collection == nil || collection.Revision != revision {
		return nil, models.ErrDiscoveryConflict
	}
	var rows []discoveryListingRow
	if err := dbWrapper.Select(ctx, &rows, `SELECT * FROM discovery_listings INDEXED BY discovery_listings_collection
 WHERE collection_uuid=? AND uuid>? ORDER BY uuid LIMIT ?`, id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.DiscoveryScopeCandidate, 0, len(rows))
	get := func(out any, query string, args ...any) error { return dbWrapper.Get(ctx, out, query, args...) }
	for _, row := range rows {
		listing, err := resolveDiscoveryListing(get, row)
		if err != nil {
			return nil, err
		}
		disposition, err := discoveryScopeDisposition(ctx, listing, collection)
		if err != nil {
			return nil, err
		}
		ret = append(ret, models.DiscoveryScopeCandidate{Listing: *listing, CurrentCollectionRevision: revision, CollectionState: collection.State, Disposition: disposition})
	}
	return ret, nil
}

func (s *DiscoveryJobStore) PreviewScope(ctx context.Context, input models.DiscoveryScopeInput) (*models.DiscoveryScopePlan, error) {
	if _, err := archive.PrepareDiscoveryScope(input); err != nil {
		return nil, err
	}
	listing, err := s.Listing(ctx, input.ListingUUID)
	if err != nil {
		return nil, err
	}
	if listing == nil || listing.Digest != input.ExpectedDefinitionSHA256 {
		return nil, models.ErrDiscoveryConflict
	}
	collection, err := (&SourceCollectionStore{}).Find(ctx, listing.CollectionUUID)
	if err != nil {
		return nil, err
	}
	if collection == nil || collection.Revision != input.CollectionRevision {
		return nil, models.ErrDiscoveryConflict
	}
	disposition, err := discoveryScopeDisposition(ctx, listing, collection)
	if err != nil {
		return nil, err
	}
	if disposition != "eligible" {
		return nil, models.ErrDiscoveryConflict
	}
	ret := &models.DiscoveryScopePlan{Version: 1, Input: input, Previous: *listing, Collection: *collection}
	var previous string
	err = dbWrapper.Get(ctx, &previous, "SELECT uuid FROM discovery_scope_reviews WHERE listing_uuid=? ORDER BY id DESC LIMIT 1", listing.UUID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		ret.PreviousReviewUUID = &previous
	}
	_, ret.DefinitionSHA256, err = archive.DiscoveryScopeDefinition(*ret)
	if err != nil {
		return nil, err
	}
	ret.PlanSHA256, err = archive.DiscoveryScopeDigest(*ret)
	return ret, err
}

func (s *DiscoveryJobStore) ScopeReview(ctx context.Context, id string) (*models.DiscoveryScopeReview, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrDiscoveryInvalid
	}
	var row discoveryScopeRow
	get := func(out any, query string, args ...any) error { return dbWrapper.Get(ctx, out, query, args...) }
	err := get(&row, "SELECT * FROM discovery_scope_reviews WHERE uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	plan, err := validateDiscoveryScopeRow(get, row)
	if err != nil {
		return nil, err
	}
	return &models.DiscoveryScopeReview{DiscoveryScopePlan: *plan, CreatedAt: row.CreatedAt.UTC()}, nil
}

func (s *DiscoveryJobStore) ReviewScope(ctx context.Context, input models.DiscoveryScopeInput, expected string, now time.Time) (*models.DiscoveryScopeReview, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	inputSHA, err := archive.PrepareDiscoveryScope(input)
	if err != nil || !archive.ValidSHA256(expected) || !validJobTime(now) {
		return nil, models.ErrDiscoveryInvalid
	}
	prior, err := s.ScopeReview(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if prior.Input != input || prior.PlanSHA256 != expected {
			return nil, models.ErrDiscoveryConflict
		}
		return prior, nil
	}
	plan, err := s.PreviewScope(ctx, input)
	if err != nil {
		return nil, err
	}
	if plan.PlanSHA256 != expected || now.Before(plan.Previous.CreatedAt) {
		return nil, models.ErrDiscoveryConflict
	}
	if plan.PreviousReviewUUID != nil {
		previous, err := s.ScopeReview(ctx, *plan.PreviousReviewUUID)
		if err != nil {
			return nil, err
		}
		if previous == nil || now.Before(previous.CreatedAt) {
			return nil, models.ErrDiscoveryConflict
		}
	}
	definition, digest, err := archive.DiscoveryScopeDefinition(*plan)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	complete := discoveryAtomic(ctx)
	_, err = dbWrapper.Exec(ctx, `INSERT INTO discovery_scope_reviews
 (uuid,listing_uuid,previous_uuid,collection_uuid,collection_revision,root_uuid,input_sha256,plan_sha256,plan,definition,digest,created_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, input.UUID, input.ListingUUID, plan.PreviousReviewUUID, plan.Collection.UUID,
		plan.Collection.Revision, plan.Collection.RootUUID, inputSHA, expected, string(body), string(definition), digest, now.UTC())
	if err != nil {
		return nil, err
	}
	ret, err := s.ScopeReview(ctx, input.UUID)
	*complete = err == nil
	return ret, err
}
