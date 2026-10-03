package sqlite

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

const maxPublisherCandidates = 100

func stableAccountReference(ref models.AccountReference) bool {
	return ref.Kind == "id" || (ref.Kind == "secUid" && ref.Namespace == "native:tiktok") ||
		(ref.Kind == "user" && strings.HasPrefix(ref.Namespace, "mirror:"))
}

func publisherNamespaceAllowed(ctx context.Context, post, namespace string) (bool, error) {
	var allowed bool
	err := dbWrapper.Get(ctx, &allowed, `SELECT
NOT EXISTS(SELECT 1 FROM source_post_identifiers WHERE post_uuid=? AND namespace NOT LIKE 'legacy:%')
OR EXISTS(SELECT 1 FROM source_post_identifiers WHERE post_uuid=? AND namespace=?)`, post, post, namespace)
	return allowed, err
}

// Check only the claimed identifier kinds on the selected canonical account.
// Historical aliases of an already consolidated account remain valid matches.
func publisherIdentifierConflict(ctx context.Context, account string, identity *models.CapturedAccount) (bool, error) {
	if identity == nil {
		return false, nil
	}
	for _, claim := range identity.Identifiers {
		ref := claim.Reference
		if !stableAccountReference(ref) {
			continue
		}
		var conflicts bool
		if err := dbWrapper.Get(ctx, &conflicts, `SELECT
EXISTS(SELECT 1 FROM source_account_identifiers WHERE canonical_uuid=? AND namespace=? AND kind=?)
AND NOT EXISTS(SELECT 1 FROM source_account_identifiers WHERE canonical_uuid=? AND namespace=? AND kind=? AND value=?)`,
			account, ref.Namespace, ref.Kind, account, ref.Namespace, ref.Kind, ref.Value); err != nil {
			return false, err
		}
		if conflicts {
			return true, nil
		}
	}
	return false, nil
}

func (s *CapturePublisherStore) Preview(ctx context.Context, value, target string) (*models.CapturePublisherPreview, error) {
	capture, err := (&SourceEvidenceStore{}).FindCapture(ctx, value)
	if err != nil {
		return nil, err
	}
	if capture == nil {
		return nil, models.ErrCapturePublisherConflict
	}
	post, err := (&SourceEvidenceStore{}).FindPost(ctx, capture.PostUUID)
	if err != nil {
		return nil, err
	}
	if post == nil {
		return nil, models.ErrSourcePostConflict
	}
	ret := &models.CapturePublisherPreview{CaptureUUID: capture.UUID, PostUUID: post.UUID, PostState: post.State,
		Candidates: []models.CapturePublisherCandidate{}, Conflicts: []string{}}
	ret.Current, err = s.Current(ctx, capture.UUID)
	if err != nil {
		return nil, err
	}
	accounts := &SourceAccountStore{}
	if target != "" {
		ret.Target, err = accounts.Resolve(ctx, target)
		if err != nil {
			return nil, err
		}
		if ret.Target == nil {
			return nil, models.ErrSourceAccountConflict
		}
	}
	raw, err := archive.RestoreCapture(capture.Payload)
	if err != nil {
		return nil, err
	}
	ret.Identity, err = archive.ExtractCapturedAccount(raw)
	if err != nil {
		ret.IdentityError = err.Error()
	}
	if err := populatePublisherCandidates(ctx, ret); err != nil {
		return nil, err
	}
	if capture.CapturedAt.IsZero() {
		ret.Conflicts = append(ret.Conflicts, "observation_time_unrecorded")
		ret.Action = "review"
	}
	if ret.Target != nil {
		allowed, err := publisherNamespaceAllowed(ctx, post.UUID, ret.Target.Namespace)
		if err != nil {
			return nil, err
		}
		if !allowed || (ret.Identity != nil && ret.Identity.Namespace != ret.Target.Namespace) {
			ret.Conflicts = append(ret.Conflicts, "target_namespace_mismatch")
		}
		conflicts, err := publisherIdentifierConflict(ctx, ret.Target.UUID, ret.Identity)
		if err != nil {
			return nil, err
		}
		if conflicts {
			ret.Conflicts = append(ret.Conflicts, "target_stable_id_conflict")
		}
	}
	if ret.Current != nil && ret.Current.State != "undecided" {
		ret.Action = "preserve"
	}
	if post.State != "active" {
		ret.Conflicts = append(ret.Conflicts, "post_forgotten")
		ret.Action = "unavailable"
	}
	// Repeated observations elsewhere on the account do not change this
	// publisher choice. Sign the relevant identity/matches, not an account-wide
	// observation counter or creation timestamp. Candidate membership, canonical
	// identities, labels, contradictions, and explicit choices remain covered.
	snapshot := *ret
	snapshot.Candidates = slices.Clone(ret.Candidates)
	for i := range snapshot.Candidates {
		snapshot.Candidates[i].Account.Revision = 0
		snapshot.Candidates[i].Account.CreatedAt = time.Time{}
	}
	if ret.Target != nil {
		selected := *ret.Target
		selected.Revision, selected.CreatedAt = 0, time.Time{}
		snapshot.Target = &selected
	}
	ret.Signature, err = sourceSignature("stash-capture-publisher-preview-v1", snapshot)
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func populatePublisherCandidates(ctx context.Context, ret *models.CapturePublisherPreview) error {
	ret.Action = "unavailable"
	if ret.IdentityError != "" {
		ret.Conflicts = append(ret.Conflicts, "invalid_captured_identity")
		return nil
	}
	if ret.Identity == nil {
		ret.Conflicts = append(ret.Conflicts, "missing_captured_identity")
		return nil
	}
	if ret.Identity.Policy != archive.CapturedAccountPolicy {
		return errors.New("unsupported captured account policy")
	}
	allowed, err := publisherNamespaceAllowed(ctx, ret.PostUUID, ret.Identity.Namespace)
	if err != nil {
		return err
	}
	if !allowed {
		ret.Conflicts = append(ret.Conflicts, "captured_namespace_mismatch")
		ret.Action = "review"
		return nil
	}
	candidates := map[string]*models.CapturePublisherCandidate{}
	stable := map[string]bool{}
	stableAmbiguous := false
	accounts := &SourceAccountStore{}
	for _, claim := range ret.Identity.Identifiers {
		matches, err := accounts.Lookup(ctx, claim.Reference, "", maxPublisherCandidates+1)
		if err != nil {
			return err
		}
		isStable := stableAccountReference(claim.Reference)
		if isStable && len(matches) > 1 {
			stableAmbiguous = true
		}
		for _, match := range matches {
			if isStable {
				stable[match.UUID] = true
			}
			candidate := candidates[match.UUID]
			if candidate == nil {
				if len(candidates) >= maxPublisherCandidates {
					ret.CandidatesTruncated = true
					continue
				}
				candidate = &models.CapturePublisherCandidate{Account: *match, Matches: []models.AccountReference{}}
				candidates[match.UUID] = candidate
			}
			candidate.Matches = append(candidate.Matches, claim.Reference)
		}
		if len(matches) > maxPublisherCandidates {
			ret.CandidatesTruncated = true
		}
	}
	for _, candidate := range candidates {
		ret.Candidates = append(ret.Candidates, *candidate)
	}
	slices.SortFunc(ret.Candidates, func(a, b models.CapturePublisherCandidate) int {
		return strings.Compare(a.Account.UUID, b.Account.UUID)
	})
	ret.StableIDMatches = len(stable) > 0
	if stableAmbiguous || len(stable) > 1 {
		ret.Action = "review"
		ret.Conflicts = append(ret.Conflicts, "ambiguous_stable_id")
		return nil
	}
	if len(stable) == 1 {
		for id := range stable {
			conflicts, err := publisherIdentifierConflict(ctx, id, ret.Identity)
			if err != nil {
				return err
			}
			if conflicts {
				ret.Action = "review"
				ret.Conflicts = append(ret.Conflicts, "stable_id_conflict")
				return nil
			}
			ret.Action, ret.AccountUUID = "link", &id
		}
		return nil
	}
	if len(candidates) > 0 || ret.CandidatesTruncated {
		ret.Action = "review"
		ret.Conflicts = append(ret.Conflicts, "locator_only_candidates")
		return nil
	}
	ret.Action = "create"
	return nil
}
