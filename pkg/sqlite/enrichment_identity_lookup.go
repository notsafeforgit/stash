package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/txn"
)

func (s *EnrichmentJobStore) IdentityResolution(ctx context.Context, id string) (*models.EnrichmentIdentityResolution, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrEnrichmentInvalid
	}
	var row models.EnrichmentIdentityResolution
	err := dbWrapper.Get(ctx, &row, "SELECT * FROM enrichment_identity_resolutions WHERE job_uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	row.CreatedAt = row.CreatedAt.UTC()
	return &row, err
}

func lookupIdentifierInput(row models.EnrichmentIdentityResolution, observed time.Time) (models.SourcePostIdentifierInput, error) {
	// Details contain references only. Original finder values remain in the
	// retained lookup row and the checkpoint's published native capture.
	details, err := json.Marshal(struct {
		Job        string `json:"job_uuid"`
		Lookup     int64  `json:"lookup_id"`
		Checkpoint int    `json:"checkpoint_revision"`
		Record     int    `json:"record_ordinal"`
		Policy     string `json:"policy"`
	}{row.JobUUID, row.LookupID, row.CheckpointRevision, row.RecordOrdinal, row.Policy})
	if err != nil {
		return models.SourcePostIdentifierInput{}, err
	}
	return models.SourcePostIdentifierInput{
		SourcePostEvidence: models.SourcePostEvidence{UUID: row.EvidenceUUID, PostUUID: row.PostUUID, Origin: "capture", Basis: row.Basis, ObservedAt: observed, Details: details},
		Identifier:         models.SourcePostIdentifier{Namespace: row.Namespace, Value: row.Value}, ExpectedPostRevision: row.PostRevision,
	}, nil
}

// ResolveLookupIdentity is part of publication, not a standalone identity
// mutation. Its deferred FK and commit proof require the same job to publish
// every checkpoint record atomically. Failure retains the original checkpoint.
func (s *EnrichmentJobStore) ResolveLookupIdentity(ctx context.Context, lease models.EnrichmentJobLease, expected int, digest string, ordinal int, now time.Time) (*models.EnrichmentIdentityResolution, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	job, err := s.CheckLease(ctx, lease, now)
	if err != nil {
		return nil, err
	}
	work, err := archive.DecodeEnrichmentJob(job)
	if err != nil {
		return nil, err
	}
	head, err := s.CheckpointHead(ctx, job.UUID)
	if err != nil {
		return nil, err
	}
	if head == nil || head.Revision != expected || head.Digest != digest || head.PendingCount != 0 || ordinal < 0 || ordinal >= head.RecordCount || now.Before(head.CreatedAt) {
		return nil, models.ErrEnrichmentConflict
	}
	transcript, err := archive.ParseEnrichmentTranscript(head.Body)
	if err != nil {
		return nil, err
	}
	if transcript.Records[ordinal].RetainedCapture != nil {
		return nil, models.ErrEnrichmentConflict
	}
	post, err := (&SourceEvidenceStore{}).FindPost(ctx, work.PostUUID)
	if err != nil {
		return nil, err
	}
	if post == nil || post.State != "active" {
		return nil, models.ErrEnrichmentConflict
	}
	var legacyOnly bool
	if err := dbWrapper.Get(ctx, &legacyOnly, `SELECT EXISTS(SELECT 1 FROM source_post_identifiers WHERE post_uuid=?)
 AND NOT EXISTS(SELECT 1 FROM source_post_identifiers WHERE post_uuid=? AND namespace NOT LIKE 'legacy:catalog:%')`, post.UUID, post.UUID); err != nil {
		return nil, err
	}
	if !legacyOnly {
		return nil, models.ErrEnrichmentConflict
	}
	target, err := (&EnrichmentWorkStore{}).Target(ctx, work.TargetUUID)
	if err != nil {
		return nil, err
	}
	if target == nil || transcript.URL != target.URL {
		return nil, models.ErrEnrichmentConflict
	}
	var candidates []enrichmentIdentityLookup
	// The collection UUID may now have a reviewed newer revision. The original
	// collection, post and exact lookup URL must still agree. Multiple original
	// inputs require explicit reconciliation rather than an arbitrary choice.
	err = dbWrapper.Select(ctx, &candidates, `SELECT * FROM enrichment_identity_lookups INDEXED BY enrichment_identity_lookup_target
 WHERE post_uuid=? AND collection_uuid=? AND url=? ORDER BY id LIMIT 2`, post.UUID, work.CollectionUUID, target.URL)
	if err != nil {
		return nil, err
	}
	if len(candidates) != 1 {
		return nil, models.ErrEnrichmentIdentityReview
	}
	values, err := candidates[0].values()
	if err != nil {
		return nil, err
	}
	raw, err := transcript.Metadata(ordinal)
	if err != nil {
		return nil, err
	}
	reference, basis, err := scrape.MatchDiscoveryLookup(values, raw)
	if err != nil {
		return nil, err
	}
	if reference == nil {
		return nil, models.ErrEnrichmentIdentityReview
	}
	existing, err := (&SourceEvidenceStore{}).FindPostByIdentifier(ctx, *reference)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		// The saved filename/account evidence corroborates this exact source
		// identity. Reuse the first post instead of leaving duplicate imports in
		// review. Publication below still has to succeed in the same transaction.
		if err := s.consolidateDiscoveryDuplicate(ctx, job.UUID, post.UUID, existing.UUID, basis, now); err != nil {
			return nil, err
		}
		txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
			_, err := enrichmentPublishedJob(ctx, job.UUID)
			return err
		})
		return nil, nil
	}
	observed, err := time.Parse(time.RFC3339Nano, transcript.Records[ordinal].ObservedAt)
	if err != nil {
		return nil, models.ErrEnrichmentInvalid
	}
	row := &models.EnrichmentIdentityResolution{JobUUID: job.UUID, LookupID: candidates[0].ID,
		CheckpointRevision: expected, RecordOrdinal: ordinal, PostUUID: post.UUID, PostRevision: post.Revision,
		Namespace: reference.Namespace, Value: reference.Value, Policy: scrape.DiscoveryIdentityPolicy, Basis: basis,
		EvidenceUUID: uuid.NewSHA1(uuid.MustParse(job.UUID), []byte("discovery-post-identity-v1")).String(), CreatedAt: now.UTC()}
	complete := enrichmentAtomic(ctx)
	identifier, err := lookupIdentifierInput(*row, observed)
	if err != nil {
		return nil, err
	}
	if _, err := (&SourcePostLinksStore{}).ObserveIdentifier(ctx, identifier); err != nil {
		return nil, err
	}
	_, err = dbWrapper.NamedExec(ctx, `INSERT INTO enrichment_identity_resolutions
 (job_uuid,lookup_id,checkpoint_revision,record_ordinal,post_uuid,post_revision,namespace,value,policy,basis,evidence_uuid,created_at)
 VALUES(:job_uuid,:lookup_id,:checkpoint_revision,:record_ordinal,:post_uuid,:post_revision,:namespace,:value,:policy,:basis,:evidence_uuid,:created_at)`, row)
	if err != nil {
		return nil, err
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		if _, err := enrichmentPublishedJob(ctx, job.UUID); err != nil {
			return err
		}
		return verifyIdentityResolution(func(out any, query string, args ...any) error { return dbWrapper.Get(ctx, out, query, args...) },
			func(out any, query string, args ...any) error { return dbWrapper.Select(ctx, out, query, args...) }, *row)
	})
	*complete = true
	return row, nil
}
