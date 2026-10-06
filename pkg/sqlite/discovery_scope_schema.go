package sqlite

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type discoveryScopeRow struct {
	ID                 int64     `db:"id"`
	UUID               string    `db:"uuid"`
	ListingUUID        string    `db:"listing_uuid"`
	PreviousUUID       *string   `db:"previous_uuid"`
	CollectionUUID     string    `db:"collection_uuid"`
	CollectionRevision int       `db:"collection_revision"`
	RootUUID           *string   `db:"root_uuid"`
	InputSHA256        string    `db:"input_sha256"`
	PlanSHA256         string    `db:"plan_sha256"`
	Plan               string    `db:"plan"`
	Definition         string    `db:"definition"`
	Digest             string    `db:"digest"`
	CreatedAt          time.Time `db:"created_at"`
}

func (r discoveryScopeRow) decode() (*models.DiscoveryScopePlan, error) {
	plan, err := archive.DecodeDiscoveryScopePlan([]byte(r.Plan))
	if err != nil || r.ID < 1 || !validJobTime(r.CreatedAt) || plan.Input.UUID != r.UUID || plan.Input.ListingUUID != r.ListingUUID ||
		plan.Collection.UUID != r.CollectionUUID || plan.Collection.Revision != r.CollectionRevision ||
		!reflect.DeepEqual(plan.Collection.RootUUID, r.RootUUID) || !reflect.DeepEqual(plan.PreviousReviewUUID, r.PreviousUUID) ||
		plan.PlanSHA256 != r.PlanSHA256 || plan.DefinitionSHA256 != r.Digest {
		return nil, models.ErrSourcePayloadCorrupt
	}
	input, err := archive.PrepareDiscoveryScope(plan.Input)
	if err != nil || input != r.InputSHA256 {
		return nil, models.ErrSourcePayloadCorrupt
	}
	body, digest, err := archive.DiscoveryScopeDefinition(*plan)
	if err != nil || string(body) != r.Definition || digest != r.Digest {
		return nil, models.ErrSourcePayloadCorrupt
	}
	encoded, err := json.Marshal(plan)
	if err != nil || string(encoded) != r.Plan {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return plan, nil
}

func validateDiscoveryScopeRow(get enrichmentGet, row discoveryScopeRow) (*models.DiscoveryScopePlan, error) {
	plan, err := row.decode()
	if err != nil {
		return nil, err
	}
	var original discoveryListingRow
	if err := get(&original, "SELECT * FROM discovery_listings WHERE uuid=?", row.ListingUUID); err != nil {
		return nil, err
	}
	previous, err := original.resolve()
	if err != nil {
		return nil, err
	}
	if row.PreviousUUID != nil {
		var before discoveryScopeRow
		if err := get(&before, "SELECT * FROM discovery_scope_reviews WHERE uuid=?", *row.PreviousUUID); err != nil {
			return nil, err
		}
		prior, err := before.decode()
		if err != nil || before.ListingUUID != row.ListingUUID || before.ID >= row.ID || row.CreatedAt.Before(before.CreatedAt) {
			return nil, models.ErrSourcePayloadCorrupt
		}
		previous = discoveryScopeListing(prior)
	}
	if previous.Digest != plan.Previous.Digest || !previous.CreatedAt.Equal(plan.Previous.CreatedAt) || row.CreatedAt.Before(previous.CreatedAt) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	collection, err := discoveryScopeCollection(get, row.CollectionUUID, row.CollectionRevision)
	if err != nil {
		return nil, err
	}
	old, err := discoveryScopeCollection(get, previous.CollectionUUID, previous.CollectionRevision)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(collection.SourceCollectionDefinition, plan.Collection.SourceCollectionDefinition) ||
		!collection.CreatedAt.Equal(plan.Collection.CreatedAt) || !discoveryScopeCollectionCompatible(old.SourceCollectionDefinition, collection.SourceCollectionDefinition) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return plan, nil
}

func validateDiscoveryScopeSchema(conn *sqlx.DB) error {
	for _, object := range []struct{ name, kind string }{
		{"discovery_scope_reviews", "table"}, {"discovery_scope_reviews_listing", "index"},
		{"discovery_scope_review_initial", "index"}, {"discovery_scope_review_immutable", "trigger"},
		{"discovery_scope_review_scope", "trigger"}, {"discovery_effective_listings", "view"},
	} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=? AND type=?)", object.name, object.kind); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", object.name)
		}
	}
	var invalid bool
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM pragma_foreign_key_check('discovery_scope_reviews'))
 OR NOT EXISTS(SELECT 1 FROM pragma_table_info('discovery_scope_reviews') WHERE name='created_at' AND upper(type)='DATETIME')
 OR EXISTS(SELECT 1 FROM discovery_scope_reviews r WHERE r.previous_uuid IS NOT
 (SELECT p.uuid FROM discovery_scope_reviews p WHERE p.listing_uuid=r.listing_uuid AND p.id<r.id ORDER BY p.id DESC LIMIT 1))
 OR EXISTS(SELECT 1 FROM discovery_scope_reviews r JOIN discovery_listing_recoveries x ON x.previous_listing_uuid=r.listing_uuid
 WHERE NOT EXISTS(SELECT 1 FROM discovery_scope_reviews p WHERE p.listing_uuid=r.listing_uuid AND p.digest=x.previous_sha256 AND p.id>=r.id))
 OR (SELECT count(*) FROM discovery_effective_listings)!=(SELECT count(*) FROM discovery_listings)
 OR EXISTS(SELECT 1 FROM discovery_listings d LEFT JOIN discovery_scope_reviews r
 ON r.id=(SELECT max(s.id) FROM discovery_scope_reviews s WHERE s.listing_uuid=d.uuid)
 LEFT JOIN discovery_effective_listings e ON e.uuid=d.uuid WHERE e.uuid IS NULL
 OR e.account_uuid IS NOT d.account_uuid OR e.collection_uuid IS NOT d.collection_uuid OR e.created_at IS NOT d.created_at
 OR e.collection_revision IS NOT (CASE WHEN r.uuid IS NULL THEN d.collection_revision ELSE r.collection_revision END)
 OR e.root_uuid IS NOT (CASE WHEN r.uuid IS NULL THEN d.root_uuid ELSE r.root_uuid END)
 OR e.definition IS NOT coalesce(r.definition,d.definition) OR e.digest IS NOT coalesce(r.digest,d.digest))`); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	for after := int64(0); ; {
		var rows []discoveryScopeRow
		if err := conn.Select(&rows, "SELECT * FROM discovery_scope_reviews WHERE id>? ORDER BY id LIMIT 100", after); err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			if _, err := validateDiscoveryScopeRow(conn.Get, row); err != nil {
				return err
			}
			after = row.ID
		}
	}
}
