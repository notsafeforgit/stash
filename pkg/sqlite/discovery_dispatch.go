package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

const activeDiscoverySelect = `SELECT * FROM archive_jobs INDEXED BY archive_jobs_active_work
 WHERE kind='account.list_page' AND state IN ('queued','running') ORDER BY id LIMIT ?`

func activeDiscoveryJobs(ctx context.Context) ([]archiveJobRow, error) {
	var rows []archiveJobRow
	if err := dbWrapper.Select(ctx, &rows, activeDiscoverySelect, archive.MaxDiscoveryJobs+1); err != nil {
		return nil, err
	}
	if len(rows) > archive.MaxDiscoveryJobs {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return rows, nil
}

func validDiscoveryProfileRequest(collection, policy, extractor string, now time.Time) bool {
	return validSourceRunUUID(collection) && archive.ValidSHA256(policy) && extractor != "" && len(extractor) <= 128 &&
		utf8.ValidString(extractor) && !strings.ContainsAny(extractor, "\r\n\x00") && validJobTime(now)
}

// Ready visits only the bounded active-job index. It grants no lease and leaves
// service cooldown/fairness arbitration to the subsequent owned claim.
func (s *DiscoveryJobStore) Ready(ctx context.Context, collection, policy, extractor string, after int64, limit int, now time.Time) ([]models.DiscoveryJobCandidate, error) {
	if !validDiscoveryProfileRequest(collection, policy, extractor, now) || after < 0 {
		return nil, models.ErrDiscoveryInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	rows, err := activeDiscoveryJobs(ctx)
	if err != nil {
		return nil, err
	}
	ret := []models.DiscoveryJobCandidate{}
	for _, row := range rows {
		job := row.resolve()
		if job.Sequence <= after || job.State != "queued" || now.Before(job.AvailableAt) {
			continue
		}
		listing, err := discoveryJobEligible(ctx, job, now)
		if err != nil {
			if errors.Is(err, models.ErrDiscoveryConflict) {
				continue
			}
			return nil, err
		}
		if listing.CollectionUUID != collection || listing.PolicySHA256 != policy || listing.ExtractorVersion != extractor {
			continue
		}
		ret = append(ret, models.DiscoveryJobCandidate{Sequence: job.Sequence, UUID: job.UUID})
		if len(ret) == limit {
			break
		}
	}
	return ret, nil
}

// ReadyListings bounds inspected rows, not only returned matches. The cursor
// advances through stale definitions and policies without a historical scan.
func (s *DiscoveryJobStore) ReadyListings(ctx context.Context, collection, policy, extractor, after string, limit int, now time.Time) (*models.DiscoveryListingCandidates, error) {
	if !validDiscoveryProfileRequest(collection, policy, extractor, now) || (after != "" && !validSourceRunUUID(after)) {
		return nil, models.ErrDiscoveryInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	var rows []discoveryListingRow
	err = dbWrapper.Select(ctx, &rows, `SELECT * FROM discovery_listings INDEXED BY discovery_listings_collection
 WHERE collection_uuid=? AND uuid>? ORDER BY uuid LIMIT ?`, collection, after, limit+1)
	if err != nil {
		return nil, err
	}
	ret := &models.DiscoveryListingCandidates{Listings: []models.DiscoveryListingCandidate{}, After: after, HasMore: len(rows) > limit}
	if ret.HasMore {
		rows = rows[:limit]
	}
	for _, row := range rows {
		ret.After = row.UUID
		listing, err := resolveDiscoveryListing(func(out any, query string, args ...any) error { return dbWrapper.Get(ctx, out, query, args...) }, row)
		if err != nil {
			return nil, err
		}
		if listing.PolicySHA256 != policy || listing.ExtractorVersion != extractor {
			continue
		}
		if err := discoveryFetchEligible(ctx, listing.DiscoveryListingInput, now); err != nil {
			if errors.Is(err, models.ErrDiscoveryConflict) {
				continue
			}
			return nil, err
		}
		job, err := s.Job(ctx, listing.UUID)
		if err != nil {
			return nil, err
		}
		if job != nil {
			if job.State != "succeeded" || now.Before(job.UpdatedAt) {
				continue
			}
			// Readiness does not need the potentially 32 MiB page body. Admission
			// still reads and validates the original page before advancing it.
			var head struct {
				Ordinal  int  `db:"ordinal"`
				Complete bool `db:"complete"`
			}
			err := dbWrapper.Get(ctx, &head, "SELECT ordinal,complete FROM discovery_pages WHERE listing_uuid=? ORDER BY ordinal DESC LIMIT 1", listing.UUID)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, models.ErrDiscoveryAtomic
			}
			if err != nil {
				return nil, err
			}
			if head.Complete || head.Ordinal >= archive.MaxDiscoveryPages {
				continue
			}
		}
		ret.Listings = append(ret.Listings, models.DiscoveryListingCandidate{UUID: listing.UUID, Digest: listing.Digest})
	}
	return ret, nil
}

// Maintain is application-owned queue maintenance. It never admits listings,
// contacts websites or removes retained pages and original attempt history.
func (s *DiscoveryJobStore) Maintain(ctx context.Context, now time.Time) (*models.DiscoveryMaintenanceResult, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	complete := discoveryAtomic(ctx)
	if !validJobTime(now) {
		return nil, models.ErrDiscoveryInvalid
	}
	rows, err := activeDiscoveryJobs(ctx)
	if err != nil {
		return nil, err
	}
	ret := &models.DiscoveryMaintenanceResult{}
	for _, row := range rows {
		job := row.resolve()
		if now.Before(job.UpdatedAt) {
			continue
		}
		work, err := archive.DecodeDiscoveryJob(job)
		if err != nil {
			return nil, err
		}
		listing, err := s.Listing(ctx, work.ListingUUID)
		if err != nil {
			return nil, err
		}
		if listing == nil || listing.Digest != work.DefinitionSHA256 || listing.CollectionUUID != work.CollectionUUID {
			return nil, models.ErrSourcePayloadCorrupt
		}
		// A future retry deadline is not a stale source definition.
		err = discoveryFetchEligible(ctx, listing.DiscoveryListingInput, maxTime(now, listing.NotBefore))
		if err != nil && !errors.Is(err, models.ErrDiscoveryConflict) {
			return nil, err
		}
		if err != nil {
			if _, err := (&ArchiveJobStore{}).Cancel(ctx, job.UUID, job.Revision, now); err != nil {
				return nil, err
			}
			ret.Cancelled++
		} else if job.State == "running" && job.LeaseUntil != nil && !now.Before(*job.LeaseUntil) {
			if err := recoverArchiveJob(ctx, job, now); err != nil {
				return nil, err
			}
			ret.Recovered++
		}
	}
	*complete = true
	return ret, nil
}
