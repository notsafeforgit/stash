package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type discoveryActivationRow struct {
	UUID           string    `db:"uuid"`
	InputSHA256    string    `db:"input_sha256"`
	PlanSHA256     string    `db:"plan_sha256"`
	SnapshotUUID   string    `db:"snapshot_uuid"`
	ManifestSHA256 string    `db:"manifest_sha256"`
	ListingUUID    string    `db:"listing_uuid"`
	Plan           string    `db:"plan"`
	CreatedAt      time.Time `db:"created_at"`
}

func (r discoveryActivationRow) decode() (*models.DiscoveryActivationPlan, error) {
	plan, err := archive.DecodeDiscoveryActivationPlan([]byte(r.Plan))
	if err != nil || plan.Input.UUID != r.UUID || plan.PlanSHA256 != r.PlanSHA256 || !validJobTime(r.CreatedAt) ||
		plan.Input.Listing.UUID != r.ListingUUID || plan.Input.Listing.Legacy.SnapshotUUID != r.SnapshotUUID || plan.Input.ManifestSHA256 != r.ManifestSHA256 {
		return nil, models.ErrSourcePayloadCorrupt
	}
	_, digest, err := archive.PrepareDiscoveryActivation(plan.Input)
	if err != nil || digest != r.InputSHA256 {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return plan, nil
}

// Validate the immutable receipt graph without requiring historical source
// revisions to remain current or previously active posts to remain active.
func validateDiscoveryActivationBindings(get enrichmentGet, row discoveryActivationRow, plan *models.DiscoveryActivationPlan) error {
	var listing struct {
		Digest    string    `db:"digest"`
		CreatedAt time.Time `db:"created_at"`
		Account   string    `db:"data_sha256"`
	}
	err := get(&listing, `SELECT d.digest,d.created_at,e.data_sha256 FROM discovery_listings d
 JOIN discovery_listing_legacy l ON l.listing_uuid=d.uuid
 JOIN automation_discovery_imports i ON i.snapshot_uuid=l.snapshot_uuid
 JOIN automation_snapshot_records e ON e.snapshot_uuid=l.snapshot_uuid AND e.ordinal=l.account_ordinal
 WHERE d.uuid=? AND l.snapshot_uuid=? AND l.account_ordinal=? AND i.manifest_sha256=? AND i.state!='running'`,
		row.ListingUUID, row.SnapshotUUID, plan.Input.Listing.Legacy.AccountOrdinal, row.ManifestSHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return models.ErrSourcePayloadCorrupt
	}
	if err != nil {
		return err
	}
	_, available, err := discoveryListingAtDigest(get, row.ListingUUID, plan.ListingSHA256)
	if err != nil || listing.Account != plan.AccountSHA256 || row.CreatedAt.Before(listing.CreatedAt) || row.CreatedAt.Before(available) {
		return models.ErrSourcePayloadCorrupt
	}
	var count int
	if err := get(&count, "SELECT count(*) FROM discovery_activation_targets WHERE activation_uuid=?", row.UUID); err != nil {
		return err
	}
	if count != len(plan.Entries) {
		return models.ErrSourcePayloadCorrupt
	}
	for _, entry := range plan.Entries {
		var bound struct {
			models.DiscoveryMatchTarget
			CurrentRevision int `db:"current_revision"`
		}
		err := get(&bound, `SELECT t.*,p.revision AS current_revision FROM discovery_activation_targets a
 JOIN discovery_match_targets t ON t.uuid=a.target_uuid JOIN source_posts p ON p.uuid=t.post_uuid
 WHERE a.activation_uuid=? AND a.target_uuid=?`, row.UUID, entry.TargetUUID)
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrSourcePayloadCorrupt
		}
		if err != nil {
			return err
		}
		if bound.ListingUUID != row.ListingUUID || bound.SnapshotUUID != row.SnapshotUUID || bound.SourceOrdinal != entry.SourceOrdinal || bound.SourceSHA256 != entry.SourceSHA256 ||
			bound.PostUUID != entry.PostUUID || bound.PostRevision != entry.PostRevision || bound.CurrentRevision < entry.PostRevision || row.CreatedAt.Before(bound.CreatedAt) {
			return models.ErrSourcePayloadCorrupt
		}
	}
	return nil
}

func validateDiscoveryActivationSchema(conn *sqlx.DB) error {
	for _, name := range []string{"discovery_activations", "discovery_activations_listing", "discovery_activation_immutable", "discovery_activation_source",
		"discovery_activation_targets", "discovery_activation_targets_target", "discovery_activation_target_immutable", "discovery_activation_target_scope"} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	for _, table := range []string{"discovery_activations", "discovery_activation_targets"} {
		var child, parent string
		var row, key int64
		err := conn.QueryRow("PRAGMA foreign_key_check("+table+")").Scan(&child, &row, &parent, &key)
		if err == nil {
			return models.ErrSourcePayloadCorrupt
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	for after := ""; ; {
		var row discoveryActivationRow
		err := conn.Get(&row, "SELECT * FROM discovery_activations WHERE uuid>? ORDER BY uuid LIMIT 1", after)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		plan, err := row.decode()
		if err != nil {
			return err
		}
		if err := validateDiscoveryActivationBindings(conn.Get, row, plan); err != nil {
			return err
		}
		after = row.UUID
	}
}
