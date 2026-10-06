package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

// A new complete search retains its predecessor's definition and evidence.
// It cannot rewrite a saved cursor or pretend to have observed earlier pages.
func discoveryRecoveryOriginal(get enrichmentGet, input models.DiscoveryListingInput) (*models.DiscoveryListing, error) {
	if input.RecoveryOf == nil {
		return nil, models.ErrDiscoveryInvalid
	}
	original, _, err := discoveryListingAtDigest(get, input.RecoveryOf.ListingUUID, input.RecoveryOf.SHA256)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, models.ErrDiscoveryConflict
		}
		return nil, err
	}
	if original.RecoveryOf != nil || original.Digest != input.RecoveryOf.SHA256 || original.UUID == input.UUID ||
		(original.HistoricalPages == 0 && len(original.InitialCursor) == 0) || input.InitialCursor != nil || input.HistoricalPages != 0 ||
		original.AccountUUID != input.AccountUUID || original.CollectionUUID != input.CollectionUUID || original.ProfileURL != input.ProfileURL ||
		original.Legacy == nil || !reflect.DeepEqual(original.Legacy, input.Legacy) || input.NotBefore.Before(original.NotBefore) {
		return nil, models.ErrDiscoveryConflict
	}
	if err := verifyDiscoveryListingLegacy(get, original.DiscoveryListingInput); err != nil {
		return nil, err
	}
	return original, nil
}

func discoveryReplacement(get enrichmentGet, listing string) (string, error) {
	var replacement string
	err := get(&replacement, "SELECT listing_uuid FROM discovery_listing_recoveries WHERE previous_listing_uuid=?", listing)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return replacement, err
}

// Fetch eligibility is separate from inspection/comparison of retained pages.
func discoveryFetchEligible(ctx context.Context, input models.DiscoveryListingInput, now time.Time) error {
	if err := discoveryListingEligible(ctx, input, now); err != nil {
		return err
	}
	var reviewedAt time.Time
	err := dbWrapper.Get(ctx, &reviewedAt, "SELECT created_at FROM discovery_scope_reviews WHERE listing_uuid=? ORDER BY id DESC LIMIT 1", input.UUID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && now.Before(reviewedAt) {
		return models.ErrDiscoveryConflict
	}
	replacement, err := discoveryReplacement(func(out any, q string, args ...any) error { return dbWrapper.Get(ctx, out, q, args...) }, input.UUID)
	if err != nil {
		return err
	}
	if replacement != "" {
		return models.ErrDiscoveryConflict
	}
	return nil
}

func checkDiscoveryRecoveryReady(ctx context.Context, input models.DiscoveryListingInput) (*models.ArchiveJob, error) {
	if input.RecoveryOf == nil {
		return nil, nil
	}
	replacement, err := discoveryReplacement(func(out any, q string, args ...any) error { return dbWrapper.Get(ctx, out, q, args...) }, input.RecoveryOf.ListingUUID)
	if err != nil {
		return nil, err
	}
	if replacement != "" && replacement != input.UUID {
		return nil, models.ErrDiscoveryConflict
	}
	job, err := (&DiscoveryJobStore{}).Job(ctx, input.RecoveryOf.ListingUUID)
	if err != nil {
		return nil, err
	}
	// A running producer must deliver or checkpoint before its search can be
	// replaced. Queued work can be cancelled atomically by reviewed activation.
	if job != nil && (job.State == "running" || input.NotBefore.Before(job.AvailableAt)) {
		return nil, models.ErrDiscoveryConflict
	}
	return job, nil
}

func discoveryRecoveryTarget(get enrichmentGet, listing *models.DiscoveryListing, ordinal int64) (*models.DiscoveryMatchTarget, error) {
	if listing.RecoveryOf == nil {
		return nil, nil
	}
	var target models.DiscoveryMatchTarget
	if err := get(&target, "SELECT * FROM discovery_match_targets WHERE uuid=?", discoveryTargetUUID(listing.RecoveryOf.ListingUUID, ordinal)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, models.ErrDiscoveryConflict
		}
		return nil, err
	}
	return &target, nil
}
