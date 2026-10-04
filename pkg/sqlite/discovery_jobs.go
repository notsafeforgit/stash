package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type DiscoveryJobStore struct{}

type discoveryListingRow struct {
	UUID               string    `db:"uuid"`
	AccountUUID        string    `db:"account_uuid"`
	CollectionUUID     string    `db:"collection_uuid"`
	CollectionRevision int       `db:"collection_revision"`
	RootUUID           *string   `db:"root_uuid"`
	Definition         string    `db:"definition"`
	Digest             string    `db:"digest"`
	CreatedAt          time.Time `db:"created_at"`
}

func (row discoveryListingRow) resolve() (*models.DiscoveryListing, error) {
	var input models.DiscoveryListingInput
	decoder := json.NewDecoder(bytes.NewBufferString(row.Definition))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	body, digest, err := archive.PrepareDiscoveryListing(input)
	if err != nil || string(body) != row.Definition || digest != row.Digest || input.UUID != row.UUID || input.AccountUUID != row.AccountUUID ||
		input.CollectionUUID != row.CollectionUUID || input.CollectionRevision != row.CollectionRevision || !reflect.DeepEqual(input.RootUUID, row.RootUUID) || !validJobTime(row.CreatedAt) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return &models.DiscoveryListing{DiscoveryListingInput: input, Digest: digest, CreatedAt: row.CreatedAt}, nil
}

func (s *DiscoveryJobStore) Listing(ctx context.Context, id string) (*models.DiscoveryListing, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrDiscoveryInvalid
	}
	var row discoveryListingRow
	err := dbWrapper.Get(ctx, &row, "SELECT * FROM discovery_listings WHERE uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row.resolve()
}

func discoveryAtomic(ctx context.Context) *bool {
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return models.ErrDiscoveryAtomic
		}
		return nil
	})
	return &complete
}

func (s *DiscoveryJobStore) CheckListing(ctx context.Context, id string, now time.Time) (*models.DiscoveryListing, error) {
	listing, err := s.Listing(ctx, id)
	if err != nil {
		return nil, err
	}
	if listing == nil {
		return nil, models.ErrDiscoveryInvalid
	}
	if err := discoveryListingEligible(ctx, listing.DiscoveryListingInput, now); err != nil {
		return nil, err
	}
	return listing, nil
}

func discoveryListingEligible(ctx context.Context, input models.DiscoveryListingInput, now time.Time) error {
	if !validJobTime(now) || now.Before(input.NotBefore) {
		return models.ErrDiscoveryConflict
	}
	account, err := (&SourceAccountStore{}).Find(ctx, input.AccountUUID)
	if err != nil {
		return err
	}
	platform, err := archive.DiscoveryProfilePlatform(input.ProfileURL)
	if err != nil {
		return err
	}
	if account == nil || account.RedirectTo != nil || account.Namespace != "native:"+platform {
		return models.ErrDiscoveryConflict
	}
	collection, err := (&SourceCollectionStore{}).Find(ctx, input.CollectionUUID)
	if err != nil {
		return err
	}
	if collection == nil || collection.Revision != input.CollectionRevision || collection.State != "active" || !reflect.DeepEqual(collection.RootUUID, input.RootUUID) {
		return models.ErrDiscoveryConflict
	}
	if input.RootUUID != nil {
		root, err := (&MediaRootStore{}).Find(ctx, *input.RootUUID)
		if err != nil {
			return err
		}
		if root == nil || root.State != "active" {
			return models.ErrDiscoveryConflict
		}
	}
	return nil
}

func (s *DiscoveryJobStore) CreateListing(ctx context.Context, input models.DiscoveryListingInput, now time.Time) (*models.DiscoveryListing, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validJobTime(now) {
		return nil, models.ErrDiscoveryInvalid
	}
	body, digest, err := archive.PrepareDiscoveryListing(input)
	if err != nil {
		return nil, err
	}
	prior, err := s.Listing(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if prior.Digest != digest {
			return nil, models.ErrDiscoveryConflict
		}
		return prior, nil
	}
	// Definitions may retain a future retry deadline; creation grants no lease.
	if err := discoveryListingEligible(ctx, input, maxTime(now, input.NotBefore)); err != nil {
		return nil, err
	}
	if err := verifyDiscoveryListingLegacy(func(out any, q string, args ...any) error { return dbWrapper.Get(ctx, out, q, args...) }, input); err != nil {
		return nil, err
	}
	complete := discoveryAtomic(ctx)
	_, err = dbWrapper.Exec(ctx, `INSERT INTO discovery_listings(uuid,account_uuid,collection_uuid,collection_revision,root_uuid,definition,digest,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		input.UUID, input.AccountUUID, input.CollectionUUID, input.CollectionRevision, input.RootUUID, string(body), digest, now.UTC())
	if err != nil {
		return nil, err
	}
	if input.Legacy != nil {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO discovery_listing_legacy VALUES(?,?,?)", input.UUID, input.Legacy.SnapshotUUID, input.Legacy.AccountOrdinal); err != nil {
			return nil, err
		}
	}
	ret, err := s.Listing(ctx, input.UUID)
	*complete = err == nil
	return ret, err
}

func (s *DiscoveryJobStore) Job(ctx context.Context, listing string) (*models.ArchiveJob, error) {
	if !validSourceRunUUID(listing) {
		return nil, models.ErrDiscoveryInvalid
	}
	return findArchiveJob(ctx, `SELECT j.* FROM discovery_listing_jobs b JOIN archive_jobs j ON j.uuid=b.job_uuid WHERE b.listing_uuid=? ORDER BY b.generation DESC LIMIT 1`, listing)
}

func (s *DiscoveryJobStore) Admit(ctx context.Context, id string, now time.Time) (*models.ArchiveJob, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	prior, err := s.Job(ctx, id)
	if err != nil || (prior != nil && prior.State != "succeeded") {
		return prior, err
	}
	if prior != nil {
		if !validJobTime(now) || now.Before(prior.UpdatedAt) {
			return nil, models.ErrDiscoveryInvalid
		}
		head, err := s.PageHead(ctx, id)
		if err != nil {
			return nil, err
		}
		if head == nil {
			return nil, models.ErrDiscoveryAtomic
		}
		if head.Complete {
			return prior, nil
		}
		work, err := archive.DecodeDiscoveryJob(prior)
		if err != nil {
			return nil, err
		}
		return s.admit(ctx, id, work.Generation+1, now, now)
	}
	return s.admit(ctx, id, 1, now, now)
}

func (s *DiscoveryJobStore) Retry(ctx context.Context, id, previous string, now time.Time) (*models.ArchiveJob, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(id) || !validJobTime(now) {
		return nil, models.ErrDiscoveryInvalid
	}
	prior, err := (&ArchiveJobStore{}).Find(ctx, previous)
	if err != nil {
		return nil, err
	}
	if prior == nil || (prior.State != "failed" && prior.State != "cancelled") {
		return nil, models.ErrDiscoveryConflict
	}
	work, err := archive.DecodeDiscoveryJob(prior)
	if err != nil {
		return nil, err
	}
	if work.ListingUUID != id {
		return nil, models.ErrDiscoveryConflict
	}
	next, err := findArchiveJob(ctx, `SELECT j.* FROM discovery_listing_jobs b JOIN archive_jobs j ON j.uuid=b.job_uuid WHERE b.listing_uuid=? AND b.generation=?`, id, work.Generation+1)
	if err != nil || next != nil {
		return next, err
	}
	if now.Before(prior.UpdatedAt) {
		return nil, models.ErrDiscoveryInvalid
	}
	return s.admit(ctx, id, work.Generation+1, maxTime(now, prior.AvailableAt), now)
}

func (s *DiscoveryJobStore) admit(ctx context.Context, id string, generation int, available, now time.Time) (*models.ArchiveJob, error) {
	listing, err := s.Listing(ctx, id)
	if err != nil {
		return nil, err
	}
	if listing == nil {
		return nil, models.ErrDiscoveryInvalid
	}
	if err := discoveryListingEligible(ctx, listing.DiscoveryListingInput, maxTime(now, listing.NotBefore)); err != nil {
		return nil, err
	}
	head, err := s.PageHead(ctx, id)
	if err != nil {
		return nil, err
	}
	if head != nil && head.Complete {
		return nil, models.ErrDiscoveryConflict
	}
	ordinal := 1
	if head != nil {
		ordinal = head.Ordinal + 1
	}
	input, err := archive.PrepareDiscoveryJob(models.DiscoveryJobArguments{Version: 1, ListingUUID: id, Generation: generation, PageOrdinal: ordinal, DefinitionSHA256: listing.Digest, CollectionUUID: listing.CollectionUUID})
	if err != nil {
		return nil, err
	}
	input.AvailableAt = maxTime(available, listing.NotBefore)
	complete := discoveryAtomic(ctx)
	job, err := (&ArchiveJobStore{}).Submit(ctx, input, now, 10000)
	if err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO discovery_listing_jobs VALUES(?,?,?,?)", job.UUID, id, generation, ordinal); err != nil {
		return nil, err
	}
	*complete = true
	return job, nil
}

func discoveryJobEligible(ctx context.Context, job *models.ArchiveJob, now time.Time) (*models.DiscoveryListing, error) {
	work, err := archive.DecodeDiscoveryJob(job)
	if err != nil {
		return nil, err
	}
	s := &DiscoveryJobStore{}
	listing, err := s.Listing(ctx, work.ListingUUID)
	if err != nil {
		return nil, err
	}
	if listing == nil || listing.Digest != work.DefinitionSHA256 || listing.CollectionUUID != work.CollectionUUID {
		return nil, models.ErrDiscoveryConflict
	}
	if err := discoveryListingEligible(ctx, listing.DiscoveryListingInput, now); err != nil {
		return nil, err
	}
	var bound bool
	if err := dbWrapper.Get(ctx, &bound, "SELECT EXISTS(SELECT 1 FROM discovery_listing_jobs WHERE job_uuid=? AND listing_uuid=? AND generation=?)", job.UUID, listing.UUID, work.Generation); err != nil {
		return nil, err
	}
	if !bound {
		return nil, models.ErrDiscoveryAtomic
	}
	return listing, nil
}

func discoveryJobSubmissionGuard(ctx context.Context, request string) {
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		job, err := (&ArchiveJobStore{}).FindSubmission(ctx, request)
		if err != nil {
			return err
		}
		work, err := archive.DecodeDiscoveryJob(job)
		if err != nil {
			return err
		}
		var bound bool
		if err := dbWrapper.Get(ctx, &bound, "SELECT EXISTS(SELECT 1 FROM discovery_listing_jobs WHERE job_uuid=? AND listing_uuid=? AND generation=?)", job.UUID, work.ListingUUID, work.Generation); err != nil {
			return err
		}
		if !bound {
			return models.ErrDiscoveryAtomic
		}
		return nil
	})
}

func discoveryJobAttemptGuard(ctx context.Context, id string, fence int64) {
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		attempt, err := (&DiscoveryJobStore{}).Attempt(ctx, id, fence)
		if err != nil {
			return err
		}
		if attempt == nil {
			return models.ErrDiscoveryAtomic
		}
		return nil
	})
}

func (s *DiscoveryJobStore) Attempt(ctx context.Context, id string, fence int64) (*models.DiscoveryJobAttempt, error) {
	if !validSourceRunUUID(id) || fence < 1 {
		return nil, models.ErrDiscoveryInvalid
	}
	ret := &models.DiscoveryJobAttempt{}
	err := dbWrapper.Get(ctx, ret, "SELECT * FROM discovery_job_attempts WHERE job_uuid=? AND fence=?", id, fence)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return ret, err
}

func (s *DiscoveryJobStore) BindAttempt(ctx context.Context, lease models.DiscoveryJobLease, now time.Time) error {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return err
	}
	if !validSourceRunUUID(lease.ProducerUUID) {
		return models.ErrDiscoveryInvalid
	}
	job, err := (&ArchiveJobStore{}).CheckLease(ctx, lease.ArchiveJobLease, now)
	if err != nil {
		return err
	}
	if _, err := discoveryJobEligible(ctx, job, now); err != nil {
		return err
	}
	prior, err := s.Attempt(ctx, job.UUID, job.Fence)
	if err != nil {
		return err
	}
	if prior != nil {
		if prior.ProducerUUID != lease.ProducerUUID {
			return models.ErrArchiveJobLease
		}
		return nil
	}
	_, err = dbWrapper.Exec(ctx, "INSERT INTO discovery_job_attempts VALUES(?,?,?)", job.UUID, job.Fence, lease.ProducerUUID)
	return err
}

func (s *DiscoveryJobStore) ownedAttempt(ctx context.Context, lease models.DiscoveryJobLease) error {
	if !validSourceRunUUID(lease.JobUUID) || !validSourceRunUUID(lease.OwnerUUID) || !validSourceRunUUID(lease.ProducerUUID) || lease.Fence < 1 {
		return models.ErrArchiveJobLease
	}
	var owned bool
	err := dbWrapper.Get(ctx, &owned, `SELECT EXISTS(SELECT 1 FROM discovery_job_attempts d JOIN archive_job_attempts a ON a.job_uuid=d.job_uuid AND a.fence=d.fence
 WHERE d.job_uuid=? AND d.fence=? AND d.producer_uuid=? AND a.owner_uuid=?)`, lease.JobUUID, lease.Fence, lease.ProducerUUID, lease.OwnerUUID)
	if err != nil {
		return err
	}
	if !owned {
		return models.ErrArchiveJobLease
	}
	return nil
}

func (s *DiscoveryJobStore) CheckLease(ctx context.Context, lease models.DiscoveryJobLease, now time.Time) (*models.ArchiveJob, error) {
	if err := s.ownedAttempt(ctx, lease); err != nil {
		return nil, err
	}
	job, err := (&ArchiveJobStore{}).CheckLease(ctx, lease.ArchiveJobLease, now)
	if err != nil {
		return nil, err
	}
	if _, err := discoveryJobEligible(ctx, job, now); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *DiscoveryJobStore) ReserveSource(ctx context.Context, lease models.DiscoveryJobLease, url string, now time.Time) (bool, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return false, err
	}
	job, err := s.CheckLease(ctx, lease, now)
	if err != nil {
		return false, err
	}
	listing, err := discoveryJobEligible(ctx, job, now)
	if err != nil {
		return false, err
	}
	if listing.ProfileURL != url {
		return false, models.ErrDiscoveryInvalid
	}
	var ready bool
	err = dbWrapper.Get(ctx, &ready, `SELECT EXISTS(SELECT 1 FROM enrichment_job_pacing p JOIN enrichment_attempt_pacing a ON a.job_uuid=p.job_uuid AND a.scope=p.scope
 JOIN source_pacing s ON s.scope=p.scope WHERE p.job_uuid=? AND a.fence=? AND s.available_at_ms<=? AND s.last_started_at_ms<=?)`, job.UUID, job.Fence, now.UnixMilli(), now.UnixMilli())
	return ready, err
}
