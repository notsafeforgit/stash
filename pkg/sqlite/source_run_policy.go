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

// The indexed history also identifies the exact policy used by each attempt.
// Several reviewed upgrades before a claim affect only the next fence.
func sourceRunExecutionPolicy(ctx context.Context, run, original string) (string, error) {
	var result string
	err := dbWrapper.Get(ctx, &result, `SELECT coalesce((SELECT policy_sha256 FROM source_run_policy_upgrades
WHERE run_uuid=? ORDER BY expected_revision DESC LIMIT 1),?)`, run, original)
	return result, err
}

func (s *SourceRunStore) PolicyUpgrade(ctx context.Context, id string) (*models.SourceRunPolicyUpgrade, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrSourceRunInvalid
	}
	var row struct {
		Request  string `db:"request_uuid"`
		Run      string `db:"run_uuid"`
		Revision int64  `db:"expected_revision"`
		Fence    int64  `db:"effective_after_fence"`
		Previous string `db:"expected_policy_sha256"`
		Policy   string `db:"policy_sha256"`
		Reason   string `db:"reason"`
		Created  int64  `db:"created_at_ms"`
	}
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM source_run_policy_upgrades WHERE request_uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &models.SourceRunPolicyUpgrade{
		SourceRunPolicyUpgradeInput: models.SourceRunPolicyUpgradeInput{RequestUUID: row.Request, RunUUID: row.Run,
			ExpectedRevision: row.Revision, ExpectedPolicySHA256: row.Previous, PolicySHA256: row.Policy, Reason: row.Reason},
		EffectiveAfterFence: row.Fence, CreatedAt: time.UnixMilli(row.Created).UTC(),
	}, nil
}

func (s *SourceRunStore) UpgradePolicy(ctx context.Context, input models.SourceRunPolicyUpgradeInput, now time.Time) (*models.SourceRunPolicyUpgrade, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(input.RequestUUID) || !validSourceRunUUID(input.RunUUID) || input.ExpectedRevision < 1 ||
		!archive.ValidSHA256(input.ExpectedPolicySHA256) || !archive.ValidSHA256(input.PolicySHA256) || input.ExpectedPolicySHA256 == input.PolicySHA256 ||
		!validJobTime(now) || input.Reason == "" || len(input.Reason) > 1024 || strings.TrimSpace(input.Reason) != input.Reason || strings.ContainsFunc(input.Reason, unicode.IsControl) {
		return nil, models.ErrSourceRunInvalid
	}
	prior, err := s.PolicyUpgrade(ctx, input.RequestUUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if prior.SourceRunPolicyUpgradeInput != input {
			return nil, models.ErrSourceRunConflict
		}
		return prior, nil
	}
	run, err := s.Find(ctx, input.RunUUID)
	if err != nil {
		return nil, err
	}
	if run == nil || run.Revision != input.ExpectedRevision || run.ExecutionPolicySHA256 != input.ExpectedPolicySHA256 ||
		(run.State != "queued" && run.State != "deferred") || now.Before(run.UpdatedAt) {
		return nil, models.ErrSourceRunConflict
	}
	if _, _, err := sourceRunDefinition(ctx, run); err != nil {
		return nil, err
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO source_run_policy_upgrades(request_uuid,run_uuid,expected_revision,effective_after_fence,
expected_policy_sha256,policy_sha256,reason,created_at_ms) VALUES(?,?,?,?,?,?,?,?)`,
		input.RequestUUID, input.RunUUID, input.ExpectedRevision, run.Fence, input.ExpectedPolicySHA256, input.PolicySHA256, input.Reason, now.UnixMilli())
	if err != nil {
		return nil, err
	}
	return s.PolicyUpgrade(ctx, input.RequestUUID)
}
