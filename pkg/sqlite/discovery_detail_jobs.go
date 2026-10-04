package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type DiscoveryDetailStore struct{}

func discoveryDetailJobProof(get func(any, string, ...any) error, job *models.ArchiveJob) (*models.DiscoveryDetailJobArguments, error) {
	work, err := archive.DecodeDiscoveryDetailJob(job)
	if err != nil {
		return nil, err
	}
	var valid bool
	err = get(&valid, `SELECT EXISTS(SELECT 1 FROM discovery_detail_jobs b JOIN discovery_match_targets t ON t.uuid=b.target_uuid
 JOIN discovery_match_candidates c ON c.id=b.candidate_sequence AND c.target_uuid=t.uuid
 JOIN discovery_match_evidence e ON e.target_uuid=t.uuid AND e.page_ordinal=? AND e.namespace=c.namespace AND e.value=c.value
 JOIN discovery_pages p ON p.listing_uuid=t.listing_uuid AND p.ordinal=e.page_ordinal
 JOIN discovery_listings d ON d.uuid=t.listing_uuid
 WHERE b.job_uuid=? AND b.target_uuid=? AND b.target_revision=? AND b.candidate_sequence=? AND b.generation=?
 AND t.revision>=b.target_revision AND b.target_revision>e.page_ordinal AND e.needs_detail=1
 AND t.source_sha256=? AND t.post_uuid=? AND t.post_revision=? AND c.namespace=? AND c.value=? AND e.url=?
 AND t.listing_uuid=? AND d.digest=? AND p.digest=? AND d.collection_uuid=? AND d.collection_revision=? AND d.root_uuid IS ?)`,
		work.PageOrdinal, job.UUID, work.TargetUUID, work.TargetRevision, work.CandidateSequence, work.Generation, work.SourceSHA256, work.PostUUID, work.PostRevision,
		work.Namespace, work.Value, work.URL, work.ListingUUID, work.DefinitionSHA256, work.PageSHA256, work.CollectionUUID, work.CollectionRevision, work.RootUUID)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return work, nil
}

func discoveryDetailEligible(ctx context.Context, work *models.DiscoveryDetailJobArguments, now time.Time) error {
	target, err := (&DiscoveryMatchStore{}).Target(ctx, work.TargetUUID)
	if err != nil {
		return err
	}
	if target == nil || target.Revision != work.TargetRevision || target.PostUUID != work.PostUUID || target.PostRevision != work.PostRevision || target.SourceSHA256 != work.SourceSHA256 || now.Before(target.UpdatedAt) {
		return models.ErrDiscoveryConflict
	}
	if err := checkDiscoveryMatchTarget(ctx, target, now); err != nil {
		return err
	}
	listing, err := (&DiscoveryJobStore{}).Listing(ctx, work.ListingUUID)
	if err != nil {
		return err
	}
	if listing == nil || listing.Digest != work.DefinitionSHA256 || listing.CollectionUUID != work.CollectionUUID || listing.CollectionRevision != work.CollectionRevision || !reflect.DeepEqual(listing.RootUUID, work.RootUUID) {
		return models.ErrSourcePayloadCorrupt
	}
	if err := discoveryFetchEligible(ctx, listing.DiscoveryListingInput, maxTime(now, listing.NotBefore)); err != nil {
		return err
	}
	var closed bool
	err = dbWrapper.Get(ctx, &closed, `SELECT EXISTS(SELECT 1 FROM discovery_match_publications WHERE target_uuid=?)
 OR EXISTS(SELECT 1 FROM source_post_identifiers WHERE post_uuid=? AND namespace NOT LIKE 'legacy:catalog:%')`, work.TargetUUID, work.PostUUID)
	if err != nil {
		return err
	}
	if closed {
		return models.ErrDiscoveryConflict
	}
	return nil
}

func (s *DiscoveryDetailStore) CheckJob(ctx context.Context, id string, now time.Time) (*models.ArchiveJob, error) {
	job, err := (&ArchiveJobStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	work, err := archive.DecodeDiscoveryDetailJob(job)
	if err != nil {
		return nil, err
	}
	if err := discoveryDetailEligible(ctx, work, now); err != nil {
		return nil, err
	}
	return job, nil
}

func discoveryDetailSubmissionGuard(ctx context.Context, request string) {
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		job, err := (&ArchiveJobStore{}).FindSubmission(ctx, request)
		if err != nil {
			return err
		}
		_, err = discoveryDetailJobProof(func(out any, q string, args ...any) error { return dbWrapper.Get(ctx, out, q, args...) }, job)
		if errors.Is(err, models.ErrSourcePayloadCorrupt) {
			return models.ErrDiscoveryAtomic
		}
		return err
	})
}
func discoveryDetailAttemptGuard(ctx context.Context, id string, fence int64) {
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		attempt, err := (&DiscoveryDetailStore{}).Attempt(ctx, id, fence)
		if err != nil {
			return err
		}
		if attempt == nil {
			return models.ErrDiscoveryAtomic
		}
		return nil
	})
}

func (s *DiscoveryDetailStore) Admit(ctx context.Context, input models.DiscoveryDetailAdmission, now time.Time) (*models.ArchiveJob, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(input.TargetUUID) || input.ExpectedTargetRevision < 1 || input.CandidateSequence < 1 || !validJobTime(now) {
		return nil, models.ErrDiscoveryInvalid
	}
	// An exact admission can be acknowledged after comparison progress or edits.
	prior, err := findArchiveJob(ctx, `SELECT j.* FROM discovery_detail_jobs b JOIN archive_jobs j ON j.uuid=b.job_uuid
 WHERE b.target_uuid=? AND b.target_revision=? AND b.candidate_sequence=? AND b.generation=1`, input.TargetUUID, input.ExpectedTargetRevision, input.CandidateSequence)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		work, err := archive.DecodeDiscoveryDetailJob(prior)
		if err != nil {
			return nil, err
		}
		if work.PolicySHA256 != input.PolicySHA256 || work.ExtractorVersion != input.ExtractorVersion {
			return nil, models.ErrDiscoveryConflict
		}
		return prior, nil
	}
	target, err := (&DiscoveryMatchStore{}).Target(ctx, input.TargetUUID)
	if err != nil {
		return nil, err
	}
	if target == nil || target.Revision != input.ExpectedTargetRevision {
		return nil, models.ErrDiscoveryConflict
	}
	candidates, err := (&DiscoveryMatchStore{}).Candidates(ctx, input.TargetUUID, input.CandidateSequence-1, 1)
	if err != nil {
		return nil, err
	}
	if len(candidates) != 1 || candidates[0].Sequence != input.CandidateSequence || !candidates[0].NeedsDetail {
		return nil, models.ErrDiscoveryConflict
	}
	candidate := candidates[0]
	listing, err := (&DiscoveryJobStore{}).Listing(ctx, target.ListingUUID)
	if err != nil {
		return nil, err
	}
	if listing == nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	receipt, err := (&DiscoveryMatchStore{}).Receipt(ctx, target.UUID, candidate.BestPage)
	if err != nil {
		return nil, err
	}
	if receipt == nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	work := models.DiscoveryDetailJobArguments{Version: 1, Generation: 1, TargetUUID: target.UUID, TargetRevision: target.Revision, SourceSHA256: target.SourceSHA256,
		PostUUID: target.PostUUID, PostRevision: target.PostRevision, CandidateSequence: candidate.Sequence, Namespace: candidate.Namespace, Value: candidate.Value, URL: candidate.URL,
		ListingUUID: listing.UUID, DefinitionSHA256: listing.Digest, PageOrdinal: candidate.BestPage, PageSHA256: receipt.PageSHA256, CollectionUUID: listing.CollectionUUID, CollectionRevision: listing.CollectionRevision,
		RootUUID: listing.RootUUID, PolicySHA256: input.PolicySHA256, ExtractorVersion: input.ExtractorVersion, CapturePolicy: archive.CaptureContextPolicy}
	return s.admit(ctx, work, listing.NotBefore, now)
}

func (s *DiscoveryDetailStore) admit(ctx context.Context, work models.DiscoveryDetailJobArguments, available, now time.Time) (*models.ArchiveJob, error) {
	submission, err := archive.PrepareDiscoveryDetailJob(work)
	if err != nil {
		return nil, err
	}
	if err := discoveryDetailEligible(ctx, &work, now); err != nil {
		return nil, err
	}
	complete := discoveryAtomic(ctx)
	previous, err := findArchiveJob(ctx, `SELECT * FROM archive_jobs
 WHERE kind='post.verify_candidate' AND state IN ('queued','running')
 AND json_extract(arguments,'$.target_uuid')=? AND json_extract(arguments,'$.candidate_sequence')=?`, work.TargetUUID, work.CandidateSequence)
	if err != nil {
		return nil, err
	}
	if previous != nil {
		old, err := archive.DecodeDiscoveryDetailJob(previous)
		if err != nil {
			return nil, err
		}
		if err := discoveryDetailEligible(ctx, old, now); !errors.Is(err, models.ErrDiscoveryConflict) {
			if err != nil {
				return nil, err
			}
			return nil, models.ErrDiscoveryConflict
		}
		if _, err := (&ArchiveJobStore{}).Cancel(ctx, previous.UUID, previous.Revision, now); err != nil {
			return nil, err
		}
		available = maxTime(available, previous.AvailableAt)
	}
	submission.AvailableAt = available
	job, err := (&ArchiveJobStore{}).Submit(ctx, submission, now, 100000)
	if err != nil {
		return nil, err
	}
	_, err = dbWrapper.Exec(ctx, "INSERT INTO discovery_detail_jobs VALUES(?,?,?,?,?)", job.UUID, work.TargetUUID, work.TargetRevision, work.CandidateSequence, work.Generation)
	if err != nil {
		return nil, err
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error { return discoveryDetailEligible(ctx, &work, now) })
	*complete = true
	return job, nil
}

func (s *DiscoveryDetailStore) Retry(ctx context.Context, id string, now time.Time) (*models.ArchiveJob, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validJobTime(now) {
		return nil, models.ErrDiscoveryInvalid
	}
	prior, err := (&ArchiveJobStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	work, err := archive.DecodeDiscoveryDetailJob(prior)
	if err != nil {
		return nil, err
	}
	if (prior.State != "failed" && prior.State != "cancelled") || now.Before(prior.UpdatedAt) {
		return nil, models.ErrDiscoveryConflict
	}
	work.Generation++
	next, err := findArchiveJob(ctx, `SELECT j.* FROM discovery_detail_jobs b JOIN archive_jobs j ON j.uuid=b.job_uuid
 WHERE b.target_uuid=? AND b.target_revision=? AND b.candidate_sequence=? AND b.generation=?`, work.TargetUUID, work.TargetRevision, work.CandidateSequence, work.Generation)
	if err != nil || next != nil {
		return next, err
	}
	return s.admit(ctx, *work, prior.AvailableAt, now)
}

func (s *DiscoveryDetailStore) Attempt(ctx context.Context, id string, fence int64) (*models.EnrichmentJobAttempt, error) {
	if !validSourceRunUUID(id) || fence < 1 {
		return nil, models.ErrDiscoveryInvalid
	}
	ret := &models.EnrichmentJobAttempt{}
	err := dbWrapper.Get(ctx, ret, "SELECT * FROM discovery_detail_attempts WHERE job_uuid=? AND fence=?", id, fence)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return ret, err
}
func (s *DiscoveryDetailStore) BindAttempt(ctx context.Context, lease models.EnrichmentJobLease, now time.Time) error {
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
	work, err := archive.DecodeDiscoveryDetailJob(job)
	if err != nil {
		return err
	}
	if err := discoveryDetailEligible(ctx, work, now); err != nil {
		return err
	}
	prior, err := s.Attempt(ctx, lease.JobUUID, lease.Fence)
	if err != nil {
		return err
	}
	if prior != nil {
		if prior.ProducerUUID != lease.ProducerUUID {
			return models.ErrArchiveJobLease
		}
		return nil
	}
	_, err = dbWrapper.Exec(ctx, "INSERT INTO discovery_detail_attempts VALUES(?,?,?)", lease.JobUUID, lease.Fence, lease.ProducerUUID)
	if err == nil {
		txn.AddPreCommitHook(ctx, func(ctx context.Context) error { return discoveryDetailEligible(ctx, work, now) })
	}
	return err
}
func (s *DiscoveryDetailStore) ownedAttempt(ctx context.Context, lease models.EnrichmentJobLease) error {
	if !validSourceRunUUID(lease.JobUUID) || !validSourceRunUUID(lease.OwnerUUID) || !validSourceRunUUID(lease.ProducerUUID) || lease.Fence < 1 {
		return models.ErrArchiveJobLease
	}
	var owned bool
	err := dbWrapper.Get(ctx, &owned, `SELECT EXISTS(SELECT 1 FROM discovery_detail_attempts p JOIN archive_job_attempts a ON a.job_uuid=p.job_uuid AND a.fence=p.fence
 WHERE p.job_uuid=? AND p.fence=? AND p.producer_uuid=? AND a.owner_uuid=?)`, lease.JobUUID, lease.Fence, lease.ProducerUUID, lease.OwnerUUID)
	if err != nil {
		return err
	}
	if !owned {
		return models.ErrArchiveJobLease
	}
	return nil
}
func (s *DiscoveryDetailStore) CheckLease(ctx context.Context, lease models.EnrichmentJobLease, now time.Time) (*models.ArchiveJob, error) {
	if err := s.ownedAttempt(ctx, lease); err != nil {
		return nil, err
	}
	if _, err := (&ArchiveJobStore{}).CheckLease(ctx, lease.ArchiveJobLease, now); err != nil {
		return nil, err
	}
	return s.CheckJob(ctx, lease.JobUUID, now)
}
func (s *DiscoveryDetailStore) ReserveSource(ctx context.Context, lease models.EnrichmentJobLease, url string, now time.Time) (bool, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return false, err
	}
	job, err := s.CheckLease(ctx, lease, now)
	if err != nil {
		return false, err
	}
	work, err := archive.DecodeDiscoveryDetailJob(job)
	if err != nil {
		return false, err
	}
	ready, err := reserveMetadataSource(ctx, job, work.CollectionUUID, url, now)
	if errors.Is(err, models.ErrEnrichmentInvalid) {
		return false, models.ErrDiscoveryInvalid
	}
	return ready, err
}
