package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func validateDiscoveryJobSchema(conn *sqlx.DB) error {
	for _, name := range []string{"discovery_listings", "discovery_listings_collection", "discovery_listings_account", "discovery_listing_immutable",
		"discovery_listing_legacy", "discovery_listing_legacy_immutable", "discovery_listing_jobs", "discovery_listing_job_immutable", "discovery_listing_job_scope",
		"archive_jobs_discovery_listing", "discovery_listing_pacing_bind", "discovery_job_attempts", "discovery_job_attempt_immutable",
		"discovery_pages", "discovery_pages_bytes", "discovery_pages_cursor", "discovery_page_immutable", "discovery_page_sequence", "discovery_listing_success"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM archive_jobs j WHERE j.kind='account.list_page' AND NOT EXISTS(SELECT 1 FROM discovery_listing_jobs b WHERE b.job_uuid=j.uuid))
 OR EXISTS(SELECT 1 FROM discovery_listing_jobs b JOIN archive_jobs j ON j.uuid=b.job_uuid WHERE j.kind!='account.list_page')
 OR EXISTS(SELECT 1 FROM archive_job_attempts a JOIN archive_jobs j ON j.uuid=a.job_uuid AND j.kind='account.list_page'
 WHERE NOT EXISTS(SELECT 1 FROM discovery_job_attempts d WHERE d.job_uuid=a.job_uuid AND d.fence=a.fence))
 OR EXISTS(SELECT 1 FROM discovery_job_attempts d LEFT JOIN archive_job_attempts a ON a.job_uuid=d.job_uuid AND a.fence=d.fence
 LEFT JOIN discovery_listing_jobs b ON b.job_uuid=d.job_uuid LEFT JOIN ingest_producers p ON p.uuid=d.producer_uuid
 WHERE a.job_uuid IS NULL OR b.job_uuid IS NULL OR p.uuid IS NULL)
 OR (SELECT count(*) FROM archive_jobs WHERE kind='account.list_page' AND state IN ('queued','running'))>?
 OR (SELECT coalesce(sum(byte_count),0) FROM discovery_pages)>?`, archive.MaxDiscoveryJobs, archive.MaxDiscoveryStoredBytes); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	for after := ""; ; {
		var row discoveryListingRow
		err := conn.Get(&row, "SELECT * FROM discovery_listings WHERE uuid>? ORDER BY uuid LIMIT 1", after)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		listing, err := resolveDiscoveryListing(conn.Get, row)
		if err != nil {
			return err
		}
		if err := verifyDiscoveryListingLegacy(conn.Get, listing.DiscoveryListingInput); err != nil {
			return err
		}
		var rooted bool
		if err := conn.Get(&rooted, `SELECT EXISTS(SELECT 1 FROM source_collection_revisions WHERE collection_uuid=? AND revision=? AND root_uuid IS ?)`, listing.CollectionUUID, listing.CollectionRevision, listing.RootUUID); err != nil {
			return err
		}
		if !rooted {
			return models.ErrSourcePayloadCorrupt
		}
		var legacy []struct {
			Snapshot string `db:"snapshot_uuid"`
			Ordinal  int64  `db:"account_ordinal"`
		}
		if err := conn.Select(&legacy, "SELECT snapshot_uuid,account_ordinal FROM discovery_listing_legacy WHERE listing_uuid=?", listing.UUID); err != nil {
			return err
		}
		if (listing.Legacy == nil && len(legacy) != 0) || (listing.Legacy != nil && (len(legacy) != 1 || legacy[0].Snapshot != listing.Legacy.SnapshotUUID || legacy[0].Ordinal != listing.Legacy.AccountOrdinal)) {
			return models.ErrSourcePayloadCorrupt
		}
		if err := validateDiscoveryListingJobs(conn, listing); err != nil {
			return err
		}
		if err := validateDiscoveryListingPages(conn, listing); err != nil {
			return err
		}
		after = row.UUID
	}
}

func validateDiscoveryListingJobs(conn *sqlx.DB, listing *models.DiscoveryListing) error {
	_, available, err := discoveryListingAtDigest(conn.Get, listing.UUID, listing.Digest)
	if err != nil {
		return err
	}
	expected := 1
	expectedPage := 1
	var previous *models.ArchiveJob
	for {
		var row struct {
			archiveJobRow
			Generation  int `db:"generation"`
			PageOrdinal int `db:"page_ordinal"`
		}
		err := conn.Get(&row, `SELECT j.*,b.generation,b.page_ordinal FROM discovery_listing_jobs b JOIN archive_jobs j ON j.uuid=b.job_uuid
 WHERE b.listing_uuid=? AND b.generation>=? ORDER BY b.generation LIMIT 1`, listing.UUID, expected)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		job := row.resolve()
		work, err := archive.DecodeDiscoveryJob(job)
		if err != nil || row.Generation != expected || work.Generation != expected || work.PageOrdinal != row.PageOrdinal || work.PageOrdinal != expectedPage || work.ListingUUID != listing.UUID || work.DefinitionSHA256 != listing.Digest ||
			work.CollectionUUID != listing.CollectionUUID || job.CreatedAt.UnixMilli() < available.UnixMilli() || job.AvailableAt.Before(listing.NotBefore) {
			return models.ErrSourcePayloadCorrupt
		}
		if previous != nil {
			if (previous.State != "failed" && previous.State != "cancelled" && previous.State != "succeeded") || job.CreatedAt.Before(previous.UpdatedAt) || job.AvailableAt.Before(previous.AvailableAt) {
				return models.ErrSourcePayloadCorrupt
			}
			var final bool
			if err := conn.Get(&final, "SELECT EXISTS(SELECT 1 FROM discovery_pages WHERE job_uuid=? AND complete=1)", previous.UUID); err != nil {
				return err
			}
			if final {
				return models.ErrSourcePayloadCorrupt
			}
		}
		scope, err := scrape.SourceScopeV1(listing.ProfileURL)
		if err != nil {
			return err
		}
		var bound bool
		if err := conn.Get(&bound, "SELECT EXISTS(SELECT 1 FROM enrichment_job_pacing WHERE job_uuid=? AND scope=?)", job.UUID, scope); err != nil {
			return err
		}
		if !bound {
			return models.ErrSourcePayloadCorrupt
		}
		if job.State == "succeeded" {
			var valid bool
			if err := conn.Get(&valid, `SELECT EXISTS(SELECT 1 FROM discovery_pages p WHERE p.listing_uuid=? AND p.job_uuid=? AND p.fence=?
 AND p.ordinal=json_extract(?,'$.page_ordinal') AND p.digest=json_extract(?,'$.page_sha256') AND p.listing_uuid=json_extract(?,'$.listing_uuid'))`, listing.UUID, job.UUID, job.Fence, string(job.Result), string(job.Result), string(job.Result)); err != nil {
				return err
			}
			if !valid {
				return models.ErrSourcePayloadCorrupt
			}
			expectedPage++
		}
		previous = job
		expected++
	}
}

func validateDiscoveryListingPages(conn *sqlx.DB, listing *models.DiscoveryListing) error {
	cursor := listing.InitialCursor
	previous := listing.CreatedAt
	seen := map[string]bool{}
	complete := false
	for ordinal := 1; ; ordinal++ {
		var row discoveryPageRow
		err := conn.Get(&row, "SELECT * FROM discovery_pages WHERE listing_uuid=? AND ordinal>=? ORDER BY ordinal LIMIT 1", listing.UUID, ordinal)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		page, err := row.resolve()
		if err != nil {
			return err
		}
		parsed, err := archive.ParseDiscoveryPage(page.Body)
		if err != nil {
			return err
		}
		if complete || row.Ordinal != ordinal || row.CreatedAt.Before(previous) || parsed.URL != listing.ProfileURL ||
			parsed.ExtractorVersion != listing.ExtractorVersion || !maps.Equal(cursor, parsed.Cursor) {
			return models.ErrSourcePayloadCorrupt
		}
		key, err := archive.EncodeSourceJSON(cursor)
		if err != nil {
			return err
		}
		sha := sourceDigest(key)
		if seen[sha] {
			return models.ErrSourcePayloadCorrupt
		}
		seen[sha] = true
		if !parsed.Complete {
			next, err := archive.EncodeSourceJSON(parsed.NextCursor)
			if err != nil {
				return err
			}
			if seen[sourceDigest(next)] {
				return models.ErrSourcePayloadCorrupt
			}
		}
		var owned bool
		if err := conn.Get(&owned, `SELECT EXISTS(SELECT 1 FROM discovery_job_attempts d JOIN archive_job_attempts a ON a.job_uuid=d.job_uuid AND a.fence=d.fence
 JOIN discovery_listing_jobs b ON b.job_uuid=d.job_uuid JOIN archive_jobs j ON j.uuid=d.job_uuid
 WHERE d.job_uuid=? AND d.fence=? AND d.producer_uuid=? AND b.listing_uuid=? AND a.started_at_ms<=?
 AND a.ended_at_ms>=? AND a.outcome='succeeded' AND j.state='succeeded' AND j.fence=d.fence AND b.page_ordinal=?)`, row.JobUUID, row.Fence, row.ProducerUUID, listing.UUID, row.CreatedAt.UnixMilli(), row.CreatedAt.UnixMilli(), row.Ordinal); err != nil {
			return err
		}
		if !owned {
			return models.ErrSourcePayloadCorrupt
		}
		for _, record := range parsed.Records {
			observed, err := time.Parse(time.RFC3339Nano, record.ObservedAt)
			if err != nil || observed.After(row.CreatedAt) {
				return models.ErrSourcePayloadCorrupt
			}
		}
		cursor, previous, complete = parsed.NextCursor, row.CreatedAt, parsed.Complete
	}
}
