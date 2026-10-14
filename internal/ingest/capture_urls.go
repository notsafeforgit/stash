package ingest

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type CaptureURLResult struct {
	CaptureUUID string                `json:"capture_uuid"`
	PostUUID    string                `json:"post_uuid"`
	Status      string                `json:"status"`
	URL         *models.SourcePostURL `json:"url,omitempty"`
}

// The caller owns the transaction. Only fill a missing source link; repeated
// captures must not accumulate URL evidence or replace an existing link.
func (s *Service) recordCaptureURL(ctx context.Context, capture *models.SourceCapture) (*CaptureURLResult, error) {
	result := &CaptureURLResult{CaptureUUID: capture.UUID, PostUUID: capture.PostUUID, Status: "unavailable"}
	identity, err := s.Repo.SourceEvidence.PostIdentity(ctx, capture.PostUUID)
	if err != nil {
		return nil, err
	}
	if identity == nil || identity.State != "active" {
		result.Status = "inactive"
		return result, nil
	}
	if identity.CanonicalUUID != capture.PostUUID {
		canonical, err := s.Repo.SourceEvidence.FindPost(ctx, identity.CanonicalUUID)
		if err != nil {
			return nil, err
		}
		if canonical == nil || canonical.State != "active" {
			result.Status = "inactive"
			return result, nil
		}
	}
	current, err := s.Repo.SourcePostLinks.CurrentURLs(ctx, capture.PostUUID, "", 1)
	if err != nil {
		return nil, err
	}
	if len(current) != 0 {
		result.Status, result.URL = "preserved", &current[0]
		return result, nil
	}
	if capture.Origin != "gallery-dl" {
		return result, nil
	}
	raw, err := archive.RestoreCapture(capture.Payload)
	if err != nil {
		return nil, err
	}
	link, err := archive.ExtractCapturedPostURL(raw)
	if err != nil || link == nil {
		return result, err
	}
	owner, err := s.Repo.SourceEvidence.FindPostByIdentifier(ctx, link.Post)
	if err != nil {
		return nil, err
	}
	if owner == nil {
		return nil, models.ErrSourcePostConflict
	}
	ownerIdentity, err := s.Repo.SourceEvidence.PostIdentity(ctx, owner.UUID)
	if err != nil {
		return nil, err
	}
	if ownerIdentity == nil || ownerIdentity.CanonicalUUID != identity.CanonicalUUID {
		return nil, models.ErrSourcePostConflict
	}
	observed := capture.CapturedAt
	if observed.IsZero() && capture.RecordedAt != nil {
		observed = *capture.RecordedAt
	}
	details, err := json.Marshal(map[string]string{"capture_uuid": capture.UUID})
	if err != nil {
		return nil, err
	}
	stored, err := s.Repo.SourcePostLinks.ObserveURL(ctx, models.SourcePostURLInput{
		SourcePostEvidence: models.SourcePostEvidence{
			UUID:     uuid.NewSHA1(uuid.MustParse(capture.UUID), []byte("captured-post-url-v1\x00"+link.URL)).String(),
			PostUUID: capture.PostUUID, Origin: "capture", Basis: "captured-post-url-v1", ObservedAt: observed, Details: details,
		}, URL: link.URL,
	})
	if err != nil {
		return nil, err
	}
	result.Status = "recovered"
	result.URL = &models.SourcePostURL{UUID: stored.URLUUID, PostUUID: stored.PostUUID, URL: stored.URL}
	return result, nil
}

// BackfillCaptureURLs is an explicit admin repair using retained captures. It
// performs no remote requests, creates no new captures and changes no media.
func (s *Service) BackfillCaptureURLs(ctx context.Context, ids []string) ([]CaptureURLResult, error) {
	if len(ids) < 1 || len(ids) > 25 {
		return nil, ErrInvalid
	}
	seen := make(map[string]bool)
	for _, id := range ids {
		if !ValidUUID(id) || seen[id] {
			return nil, ErrInvalid
		}
		seen[id] = true
	}
	results := make([]CaptureURLResult, 0, len(ids))
	err := s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		for _, id := range ids {
			capture, err := s.Repo.SourceEvidence.FindCapture(ctx, id)
			if err != nil {
				return err
			}
			if capture == nil {
				return ErrNotFound
			}
			result, err := s.recordCaptureURL(ctx, capture)
			if err != nil {
				return err
			}
			results = append(results, *result)
		}
		return nil
	})
	return results, err
}
