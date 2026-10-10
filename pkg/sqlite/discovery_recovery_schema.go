package sqlite

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
)

func validateDiscoveryRecovery(get enrichmentGet, id string) error {
	var row discoveryListingRow
	if err := get(&row, "SELECT * FROM discovery_listings WHERE uuid=?", id); err != nil {
		return err
	}
	listing, err := row.resolve()
	if err != nil || listing.RecoveryOf == nil {
		return models.ErrSourcePayloadCorrupt
	}
	previous, err := discoveryRecoveryOriginal(get, listing.DiscoveryListingInput)
	if err != nil || listing.CreatedAt.Before(previous.CreatedAt) {
		return models.ErrSourcePayloadCorrupt
	}
	_, available, err := discoveryListingAtDigest(get, previous.UUID, previous.Digest)
	if err != nil || listing.CreatedAt.Before(available) {
		return models.ErrSourcePayloadCorrupt
	}
	var bound bool
	if err := get(&bound, `SELECT EXISTS(SELECT 1 FROM discovery_listing_recoveries
 WHERE listing_uuid=? AND previous_listing_uuid=? AND previous_sha256=?)`, id, previous.UUID, previous.Digest); err != nil {
		return err
	}
	if !bound {
		return models.ErrSourcePayloadCorrupt
	}
	var invalid bool
	if err := get(&invalid, `SELECT EXISTS(SELECT 1 FROM discovery_listing_jobs b JOIN archive_jobs j ON j.uuid=b.job_uuid
 WHERE b.listing_uuid=? AND (j.state IN ('queued','running') OR j.updated_at_ms>? OR j.available_at_ms>?))
 OR EXISTS(SELECT 1 FROM discovery_pages WHERE listing_uuid=? AND created_at>?)`,
		previous.UUID, listing.CreatedAt.UnixMilli(), listing.NotBefore.UnixMilli(), previous.UUID, listing.CreatedAt); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	return nil
}

func validateDiscoveryRecoveryTarget(get enrichmentGet, id string) error {
	var valid bool
	err := get(&valid, `SELECT EXISTS(SELECT 1 FROM discovery_recovery_targets r
 JOIN discovery_match_targets n ON n.uuid=r.target_uuid JOIN discovery_match_targets p ON p.uuid=r.previous_target_uuid
 JOIN discovery_listing_recoveries l ON l.listing_uuid=n.listing_uuid AND l.previous_listing_uuid=p.listing_uuid
 WHERE n.uuid=? AND n.snapshot_uuid=p.snapshot_uuid AND n.source_ordinal=p.source_ordinal
 AND n.source_sha256=p.source_sha256 AND n.post_uuid=p.post_uuid AND n.created_at>=p.created_at)`, id)
	if err != nil {
		return err
	}
	if !valid {
		return models.ErrSourcePayloadCorrupt
	}
	return nil
}

func validateDiscoveryRecoverySchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"discovery_listing_recoveries", "discovery_listing_recovery_immutable", "discovery_listing_recovery_scope",
		"discovery_recovery_job_scope", "discovery_recovery_page_scope", "discovery_recovery_targets", "discovery_recovery_target_immutable", "discovery_recovery_target_scope"} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	if !auditData {
		return nil
	}
	for _, table := range []string{"discovery_listing_recoveries", "discovery_recovery_targets"} {
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
	var invalid bool
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM discovery_listings d
 WHERE coalesce(json_type(d.definition,'$.recovery_of')='object',0)!=
 (EXISTS(SELECT 1 FROM discovery_listing_recoveries r WHERE r.listing_uuid=d.uuid)))
 OR EXISTS(SELECT 1 FROM discovery_match_targets t JOIN discovery_listing_recoveries l ON l.listing_uuid=t.listing_uuid
 WHERE NOT EXISTS(SELECT 1 FROM discovery_recovery_targets r WHERE r.target_uuid=t.uuid))`); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	for after := ""; ; {
		var id string
		err := conn.Get(&id, "SELECT listing_uuid FROM discovery_listing_recoveries WHERE listing_uuid>? ORDER BY listing_uuid LIMIT 1", after)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return err
		}
		if err := validateDiscoveryRecovery(conn.Get, id); err != nil {
			return err
		}
		after = id
	}
	for after := ""; ; {
		var id string
		err := conn.Get(&id, "SELECT target_uuid FROM discovery_recovery_targets WHERE target_uuid>? ORDER BY target_uuid LIMIT 1", after)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := validateDiscoveryRecoveryTarget(conn.Get, id); err != nil {
			return err
		}
		after = id
	}
}
