package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

type sourceConsolidationDepth struct{}

// Present the original request identity with the coverage proved by its reviewed
// successor. No original attempts, completed windows, or admission are rewritten.
func resolveSourceRunConsolidation(ctx context.Context, run *models.SourceRun) error {
	var target string
	err := dbWrapper.Get(ctx, &target, "SELECT target_uuid FROM source_run_consolidations WHERE source_uuid=?", run.UUID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	depth, _ := ctx.Value(sourceConsolidationDepth{}).(int)
	if depth >= 32 {
		return models.ErrSourceRunConflict
	}
	successor, err := (&SourceRunStore{}).Find(context.WithValue(ctx, sourceConsolidationDepth{}, depth+1), target)
	if err != nil {
		return err
	}
	if successor == nil {
		return models.ErrSourceRunConflict
	}
	remaining := scrape.Subtract(run.Pending, successor.Completed)
	satisfied := scrape.Subtract(run.Pending, remaining)
	run.Completed = scrape.Union(run.Completed, satisfied)
	run.Pending = remaining
	run.ConsolidatedInto = target
	run.AvailableAt = successor.AvailableAt
	run.ErrorCode = successor.ErrorCode
	run.State = successor.State
	if len(remaining) == 0 {
		run.State = "succeeded"
		run.ErrorCode = ""
	} else if run.State == "running" {
		// Ownership belongs only to the successor, never to its original callers.
		run.State = "queued"
	}
	return nil
}

func (s *SourceRunStore) Consolidate(ctx context.Context, input models.SourceRunConsolidationInput, now time.Time) (*models.SourceRunConsolidation, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(input.RequestUUID) || !validSourceRunUUID(input.SourceUUID) || !validSourceRunUUID(input.TargetUUID) ||
		input.SourceUUID == input.TargetUUID || input.SourceRevision < 1 || input.TargetRevision < 1 || !validJobTime(now) ||
		input.Reason == "" || len(input.Reason) > 1024 || strings.TrimSpace(input.Reason) != input.Reason || strings.ContainsFunc(input.Reason, unicode.IsControl) {
		return nil, models.ErrSourceRunInvalid
	}
	var prior struct {
		Request        string `db:"request_uuid"`
		Source         string `db:"source_uuid"`
		Target         string `db:"target_uuid"`
		SourceRevision int64  `db:"source_revision"`
		TargetRevision int64  `db:"target_revision"`
		Policy         string `db:"policy_sha256"`
		Reason         string `db:"reason"`
		Created        int64  `db:"created_at_ms"`
	}
	err := dbWrapper.Get(ctx, &prior, "SELECT * FROM source_run_consolidations WHERE request_uuid=?", input.RequestUUID)
	if err == nil {
		original := models.SourceRunConsolidationInput{RequestUUID: prior.Request, SourceUUID: prior.Source, TargetUUID: prior.Target,
			SourceRevision: prior.SourceRevision, TargetRevision: prior.TargetRevision, Reason: prior.Reason}
		if original != input {
			return nil, models.ErrSourceRunConflict
		}
		return &models.SourceRunConsolidation{SourceRunConsolidationInput: original, PolicySHA256: prior.Policy, CreatedAt: time.UnixMilli(prior.Created).UTC()}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	from, err := s.Find(ctx, input.SourceUUID)
	if err != nil {
		return nil, err
	}
	to, err := s.Find(ctx, input.TargetUUID)
	if err != nil {
		return nil, err
	}
	if from == nil || to == nil || from.Revision != input.SourceRevision || to.Revision != input.TargetRevision ||
		from.ConsolidatedInto != "" || to.ConsolidatedInto != "" || from.State != to.State ||
		(from.State != "queued" && from.State != "deferred") || now.Before(from.UpdatedAt) || now.Before(to.UpdatedAt) ||
		from.CollectionUUID != to.CollectionUUID || from.CollectionRevision != to.CollectionRevision ||
		!reflect.DeepEqual(from.RootUUID, to.RootUUID) || from.RootRevision != to.RootRevision || from.Operation != to.Operation ||
		from.ExecutionPolicySHA256 != to.ExecutionPolicySHA256 || from.RetrievalURL != to.RetrievalURL || from.CooldownSeconds != to.CooldownSeconds {
		return nil, models.ErrSourceRunConflict
	}
	if _, _, err := sourceRunDefinition(ctx, to); err != nil {
		return nil, err
	}
	// A successor can itself be consolidated later. Bound the full chain before
	// writing so every retained admission remains resolvable after that change.
	var depth int
	if err := dbWrapper.Get(ctx, &depth, `WITH RECURSIVE ancestors(uuid,depth) AS (
 SELECT ?,0 UNION ALL SELECT c.source_uuid,a.depth+1 FROM source_run_consolidations c
 JOIN ancestors a ON c.target_uuid=a.uuid WHERE a.depth<32)
 SELECT max(depth) FROM ancestors`, from.UUID); err != nil {
		return nil, err
	}
	if depth >= 31 {
		return nil, models.ErrSourceRunCapacity
	}
	var sameSubscription bool
	if err := dbWrapper.Get(ctx, &sameSubscription, `SELECT
 coalesce((SELECT revision FROM source_run_subscriptions WHERE run_uuid=?),0)=
 coalesce((SELECT revision FROM source_run_subscriptions WHERE run_uuid=?),0)`, from.UUID, to.UUID); err != nil {
		return nil, err
	}
	if !sameSubscription || len(from.Pending) == 0 || len(to.Pending) == 0 || from.Pending[0].Basis != to.Pending[0].Basis {
		return nil, models.ErrSourceRunConflict
	}
	pending := scrape.Subtract(scrape.Union(to.Pending, from.Pending), to.Completed)
	if len(pending)+len(to.Completed) > scrape.MaxWindows {
		return nil, models.ErrSourceRunCapacity
	}
	body, err := json.Marshal(pending)
	if err != nil {
		return nil, err
	}
	// Both histories retain their backoff/review decisions. Combining work must
	// not accelerate a retry or silently release a deferred source.
	available := max(to.AvailableAt.UnixMilli(), from.AvailableAt.UnixMilli())
	if _, err := dbWrapper.Exec(ctx, "UPDATE source_runs SET pending=?,available_at_ms=?,revision=revision+1,updated_at_ms=? WHERE uuid=?", string(body), available, now.UnixMilli(), to.UUID); err != nil {
		return nil, err
	}
	if _, err := s.Review(ctx, from.UUID, from.Revision, "cancel", now); err != nil {
		return nil, err
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO source_run_consolidations(request_uuid,source_uuid,target_uuid,source_revision,target_revision,policy_sha256,reason,created_at_ms)
 VALUES(?,?,?,?,?,?,?,?)`, input.RequestUUID, from.UUID, to.UUID, from.Revision, to.Revision, to.ExecutionPolicySHA256, input.Reason, now.UnixMilli())
	if err != nil {
		return nil, err
	}
	return &models.SourceRunConsolidation{SourceRunConsolidationInput: input, PolicySHA256: to.ExecutionPolicySHA256, CreatedAt: time.UnixMilli(now.UnixMilli()).UTC()}, nil
}
