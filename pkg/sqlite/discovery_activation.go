package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

func (s *DiscoveryMatchStore) PreviewActivation(ctx context.Context, input models.DiscoveryActivationInput) (*models.DiscoveryActivationPlan, error) {
	input, _, err := archive.PrepareDiscoveryActivation(input)
	if err != nil {
		return nil, err
	}
	get := func(out any, query string, args ...any) error { return dbWrapper.Get(ctx, out, query, args...) }
	if err := verifyDiscoveryListingLegacy(get, input.Listing); err != nil {
		return nil, err
	}
	if _, err := checkDiscoveryRecoveryReady(ctx, input.Listing); err != nil {
		return nil, err
	}
	if err := discoveryListingEligible(ctx, input.Listing, input.Listing.NotBefore); err != nil {
		return nil, err
	}
	var original struct {
		Manifest string `db:"manifest_sha256"`
		Account  string `db:"data_sha256"`
	}
	err = get(&original, `SELECT i.manifest_sha256,a.data_sha256 FROM automation_discovery_imports i
 JOIN automation_snapshot_records a ON a.snapshot_uuid=i.snapshot_uuid
 WHERE i.snapshot_uuid=? AND a.ordinal=?`, input.Listing.Legacy.SnapshotUUID, input.Listing.Legacy.AccountOrdinal)
	if err != nil {
		return nil, err
	}
	if original.Manifest != input.ManifestSHA256 {
		return nil, models.ErrDiscoveryConflict
	}
	_, digest, err := archive.PrepareDiscoveryListing(input.Listing)
	if err != nil {
		return nil, err
	}
	prior, err := (&DiscoveryJobStore{}).Listing(ctx, input.Listing.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil && prior.Digest != digest {
		return nil, models.ErrDiscoveryConflict
	}
	listing := &models.DiscoveryListing{DiscoveryListingInput: input.Listing, Digest: digest}
	plan := &models.DiscoveryActivationPlan{Version: 1, Input: input, ListingSHA256: digest, AccountSHA256: original.Account}
	for _, selected := range input.Targets {
		source, _, err := discoveryMatchSource(get, listing, selected.SourceOrdinal)
		if err != nil {
			return nil, err
		}
		if source.SHA256 != selected.SourceSHA256 {
			return nil, models.ErrDiscoveryConflict
		}
		post, err := (&SourceEvidenceStore{}).FindPost(ctx, source.PostUUID)
		if err != nil {
			return nil, err
		}
		if post == nil || post.State != "active" {
			return nil, models.ErrDiscoveryConflict
		}
		id := discoveryTargetUUID(listing.UUID, selected.SourceOrdinal)
		bound, err := s.Target(ctx, id)
		if err != nil {
			return nil, err
		}
		if bound != nil && (bound.PostUUID != source.PostUUID || bound.PostRevision != post.Revision || bound.SourceSHA256 != selected.SourceSHA256) {
			return nil, models.ErrDiscoveryConflict
		}
		plan.Entries = append(plan.Entries, models.DiscoveryActivationEntry{DiscoveryActivationSelection: selected, TargetUUID: id, PostUUID: source.PostUUID, PostRevision: post.Revision})
	}
	plan.PlanSHA256, err = archive.DiscoveryActivationDigest(*plan)
	return plan, err
}

func (s *DiscoveryMatchStore) Activation(ctx context.Context, id string) (*models.DiscoveryActivation, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrDiscoveryInvalid
	}
	var row discoveryActivationRow
	err := dbWrapper.Get(ctx, &row, "SELECT * FROM discovery_activations WHERE uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	plan, err := row.decode()
	if err != nil {
		return nil, err
	}
	if err := validateDiscoveryActivationBindings(func(out any, query string, args ...any) error { return dbWrapper.Get(ctx, out, query, args...) }, row, plan); err != nil {
		return nil, err
	}
	return &models.DiscoveryActivation{DiscoveryActivationPlan: *plan, CreatedAt: row.CreatedAt.UTC()}, nil
}

func (s *DiscoveryMatchStore) Activate(ctx context.Context, input models.DiscoveryActivationInput, expected string, now time.Time) (*models.DiscoveryActivation, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	input, inputSHA, err := archive.PrepareDiscoveryActivation(input)
	if err != nil || !archive.ValidSHA256(expected) || !validJobTime(now) {
		return nil, models.ErrDiscoveryInvalid
	}
	prior, err := s.Activation(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		_, priorSHA, err := archive.PrepareDiscoveryActivation(prior.Input)
		if err != nil || priorSHA != inputSHA || prior.PlanSHA256 != expected {
			return nil, models.ErrDiscoveryConflict
		}
		return prior, nil
	}
	plan, err := s.PreviewActivation(ctx, input)
	if err != nil {
		return nil, err
	}
	if plan.PlanSHA256 != expected {
		return nil, models.ErrDiscoveryConflict
	}
	complete := discoveryAtomic(ctx)
	listing, err := (&DiscoveryJobStore{}).CreateListing(ctx, input.Listing, now)
	if err != nil {
		return nil, err
	}
	if now.Before(listing.CreatedAt) {
		return nil, models.ErrDiscoveryConflict
	}
	for _, entry := range plan.Entries {
		target, err := s.BindTarget(ctx, models.DiscoveryTargetInput{ListingUUID: listing.UUID, SourceOrdinal: entry.SourceOrdinal, ExpectedSourceSHA256: entry.SourceSHA256, ExpectedPostRevision: entry.PostRevision}, now)
		if err != nil {
			return nil, err
		}
		if target.UUID != entry.TargetUUID || target.PostUUID != entry.PostUUID || now.Before(target.CreatedAt) {
			return nil, models.ErrDiscoveryConflict
		}
	}
	body, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO discovery_activations(uuid,input_sha256,plan_sha256,snapshot_uuid,manifest_sha256,listing_uuid,plan,created_at)
 VALUES(?,?,?,?,?,?,?,?)`, input.UUID, inputSHA, expected, input.Listing.Legacy.SnapshotUUID, input.ManifestSHA256, listing.UUID, string(body), now.UTC())
	if err != nil {
		return nil, err
	}
	for _, entry := range plan.Entries {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO discovery_activation_targets(activation_uuid,target_uuid) VALUES(?,?)", input.UUID, entry.TargetUUID); err != nil {
			return nil, err
		}
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		current, err := s.PreviewActivation(ctx, input)
		if err != nil {
			return err
		}
		if current.PlanSHA256 != expected {
			return models.ErrDiscoveryConflict
		}
		return nil
	})
	result, err := s.Activation(ctx, input.UUID)
	*complete = err == nil
	return result, err
}
