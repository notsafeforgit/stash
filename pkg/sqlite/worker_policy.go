package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type WorkerPolicyStore struct{}

func metadataWorkerPolicy(ctx context.Context, kind, original string) (string, *string, error) {
	var row struct {
		Policy string `db:"policy_sha256"`
		UUID   string `db:"request_uuid"`
	}
	err := dbWrapper.Get(ctx, &row, `SELECT policy_sha256,request_uuid FROM metadata_worker_policy_upgrades
WHERE kind=? AND original_policy_sha256=? ORDER BY id DESC LIMIT 1`, kind, original)
	if errors.Is(err, sql.ErrNoRows) {
		return original, nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	return row.Policy, &row.UUID, nil
}

func (s *WorkerPolicyStore) Resolve(ctx context.Context, kind, original string) (string, error) {
	if !metadataJobKind(kind) || !archive.ValidSHA256(original) {
		return "", models.ErrEnrichmentInvalid
	}
	policy, _, err := metadataWorkerPolicy(ctx, kind, original)
	return policy, err
}

func (s *WorkerPolicyStore) Receipt(ctx context.Context, id string) (*models.WorkerPolicyUpgrade, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrEnrichmentInvalid
	}
	var row struct {
		Request  string `db:"request_uuid"`
		Kind     string `db:"kind"`
		Original string `db:"original_policy_sha256"`
		Expected string `db:"expected_policy_sha256"`
		Policy   string `db:"policy_sha256"`
		Reason   string `db:"reason"`
		Created  int64  `db:"created_at_ms"`
	}
	err := dbWrapper.Get(ctx, &row, `SELECT request_uuid,kind,original_policy_sha256,expected_policy_sha256,policy_sha256,reason,created_at_ms
FROM metadata_worker_policy_upgrades WHERE request_uuid=?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &models.WorkerPolicyUpgrade{WorkerPolicyUpgradeInput: models.WorkerPolicyUpgradeInput{
		RequestUUID: row.Request, Kind: row.Kind, OriginalPolicySHA256: row.Original, ExpectedPolicySHA256: row.Expected,
		PolicySHA256: row.Policy, Reason: row.Reason}, CreatedAt: time.UnixMilli(row.Created).UTC()}, nil
}

func (s *WorkerPolicyStore) Upgrade(ctx context.Context, input models.WorkerPolicyUpgradeInput, now time.Time) (*models.WorkerPolicyUpgrade, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(input.RequestUUID) || !metadataJobKind(input.Kind) || !archive.ValidSHA256(input.OriginalPolicySHA256) ||
		!archive.ValidSHA256(input.ExpectedPolicySHA256) || !archive.ValidSHA256(input.PolicySHA256) || input.PolicySHA256 == input.ExpectedPolicySHA256 ||
		!validJobTime(now) || input.Reason == "" || len(input.Reason) > 1024 || strings.TrimSpace(input.Reason) != input.Reason || strings.ContainsFunc(input.Reason, unicode.IsControl) {
		return nil, models.ErrEnrichmentInvalid
	}
	prior, err := s.Receipt(ctx, input.RequestUUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if prior.WorkerPolicyUpgradeInput != input {
			return nil, models.ErrEnrichmentConflict
		}
		return prior, nil
	}
	current, err := s.Resolve(ctx, input.Kind, input.OriginalPolicySHA256)
	if err != nil {
		return nil, err
	}
	if current != input.ExpectedPolicySHA256 {
		return nil, models.ErrEnrichmentConflict
	}
	// Claims and approvals share the native write transaction. Never change an
	// executing worker's authority; expiry must be recovered by queue maintenance.
	var active []archiveJobRow
	err = dbWrapper.Select(ctx, &active, `SELECT * FROM archive_jobs INDEXED BY archive_jobs_active_work
WHERE kind=? AND state IN ('queued','running') AND state='running' ORDER BY id LIMIT 10001`, input.Kind)
	if err != nil {
		return nil, err
	}
	if len(active) > 10000 {
		return nil, models.ErrArchiveJobCapacity
	}
	for _, row := range active {
		original, err := metadataJobOriginalPolicy(ctx, row.resolve())
		if err != nil {
			return nil, err
		}
		if original == input.OriginalPolicySHA256 {
			return nil, models.ErrEnrichmentConflict
		}
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO metadata_worker_policy_upgrades
(request_uuid,kind,original_policy_sha256,expected_policy_sha256,policy_sha256,reason,created_at_ms) VALUES(?,?,?,?,?,?,?)`,
		input.RequestUUID, input.Kind, input.OriginalPolicySHA256, input.ExpectedPolicySHA256, input.PolicySHA256, input.Reason, now.UnixMilli())
	if err != nil {
		return nil, err
	}
	return s.Receipt(ctx, input.RequestUUID)
}

func metadataJobOriginalPolicy(ctx context.Context, job *models.ArchiveJob) (string, error) {
	switch job.Kind {
	case models.ArchiveJobEnrichPost:
		work, err := archive.DecodeEnrichmentJob(job)
		if err != nil {
			return "", err
		}
		return work.PolicySHA256, nil
	case models.ArchiveJobVerifyCandidate:
		work, err := archive.DecodeDiscoveryDetailJob(job)
		if err != nil {
			return "", err
		}
		return work.PolicySHA256, nil
	case models.ArchiveJobListAccount:
		work, err := archive.DecodeDiscoveryJob(job)
		if err != nil {
			return "", err
		}
		listing, _, err := discoveryListingAtDigest(func(out any, query string, args ...any) error {
			return dbWrapper.Get(ctx, out, query, args...)
		}, work.ListingUUID, work.DefinitionSHA256)
		if err != nil {
			return "", err
		}
		if listing == nil || listing.Digest != work.DefinitionSHA256 {
			return "", models.ErrDiscoveryConflict
		}
		return listing.PolicySHA256, nil
	}
	return "", nil
}

func metadataAttemptPolicy(ctx context.Context, job *models.ArchiveJob, fence int64, original string) (string, error) {
	var policy string
	err := dbWrapper.Get(ctx, &policy, `SELECT policy_sha256 FROM metadata_worker_attempt_policies WHERE job_uuid=? AND fence=?`, job.UUID, fence)
	if errors.Is(err, sql.ErrNoRows) {
		return original, nil
	}
	return policy, err
}

func resolveMetadataJobPolicy(ctx context.Context, job *models.ArchiveJob) error {
	if !metadataJobKind(job.Kind) {
		return nil
	}
	original, err := metadataJobOriginalPolicy(ctx, job)
	if err != nil {
		return err
	}
	switch {
	case job.State == "queued":
		job.ExecutionPolicySHA256, _, err = metadataWorkerPolicy(ctx, job.Kind, original)
	case job.Fence > 0:
		job.ExecutionPolicySHA256, err = metadataAttemptPolicy(ctx, job, job.Fence, original)
	default:
		job.ExecutionPolicySHA256 = original
	}
	return err
}

func bindMetadataAttemptPolicy(ctx context.Context, job *models.ArchiveJob, fence int64) error {
	if !metadataJobKind(job.Kind) {
		return nil
	}
	original, err := metadataJobOriginalPolicy(ctx, job)
	if err != nil {
		return err
	}
	policy, approval, err := metadataWorkerPolicy(ctx, job.Kind, original)
	if err != nil {
		return err
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO metadata_worker_attempt_policies(job_uuid,fence,original_policy_sha256,policy_sha256,approval_uuid)
VALUES(?,?,?,?,?)`, job.UUID, fence, original, policy, approval)
	return err
}
