package sqlite

import (
	"context"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func validateAccountConsolidationReview(input models.AccountConsolidationReviewInput) error {
	for _, value := range []string{input.SourceUUID, input.DestinationUUID} {
		if id, err := archiveUUID(value); err != nil || id != value {
			return models.ErrAccountConsolidationReviewInvalid
		}
	}
	if input.SourceUUID == input.DestinationUUID || !validAccountText(input.Reason, 4096, true) {
		return models.ErrAccountConsolidationReviewInvalid
	}
	switch input.OwnershipMode {
	case "preserve":
		if input.Ownership != nil {
			return models.ErrAccountConsolidationReviewInvalid
		}
	case "choose":
		if input.Ownership == nil {
			return models.ErrAccountConsolidationReviewInvalid
		}
		choice := input.Ownership
		if err := validateAccountOwnershipReviewInput(models.AccountOwnershipReviewInput{AccountUUID: input.SourceUUID, AccountRevision: 1,
			State: choice.State, PerformerUUID: choice.PerformerUUID, PerformerRevision: choice.PerformerRevision}); err != nil {
			return models.ErrAccountConsolidationReviewInvalid
		}
	default:
		return models.ErrAccountConsolidationReviewInvalid
	}
	return nil
}

func accountConsolidationReviewRequest(input models.AccountConsolidationReviewApplyInput) (models.AccountConsolidationInput, string, error) {
	var domain models.AccountConsolidationInput
	if err := validateAccountConsolidationReview(input.AccountConsolidationReviewInput); err != nil {
		return domain, "", err
	}
	if id, err := archiveUUID(input.RequestUUID); err != nil || id != input.RequestUUID || !archive.ValidSHA256(input.Digest) {
		return domain, "", models.ErrAccountConsolidationReviewInvalid
	}
	domain = models.AccountConsolidationInput{SourceUUID: input.SourceUUID, DestinationUUID: input.DestinationUUID, Signature: input.Digest,
		OwnershipMode: input.OwnershipMode, AcceptIdentifierConflicts: input.AcceptIdentifierConflicts, Origin: "review", Reason: input.Reason}
	if input.Ownership != nil {
		domain.Ownership = models.AccountOwnershipSelection{State: input.Ownership.State, PerformerUUID: input.Ownership.PerformerUUID, PerformerRevision: input.Ownership.PerformerRevision}
	}
	// Keep the original domain serialization. Changing field tags or including
	// UUID here would break recovery of historical consolidation requests.
	domain, digest, err := accountConsolidationRequest(domain)
	domain.UUID = input.RequestUUID
	return domain, digest, err
}

func (s *SourceAccountStore) PreviewConsolidationReview(ctx context.Context, input models.AccountConsolidationReviewInput) (*models.AccountConsolidationReviewPreview, error) {
	if err := validateAccountConsolidationReview(input); err != nil {
		return nil, err
	}
	from, err := s.ReviewAccount(ctx, input.SourceUUID)
	if err != nil {
		return nil, err
	}
	to, err := s.ReviewAccount(ctx, input.DestinationUUID)
	if err != nil {
		return nil, err
	}
	if from == nil || to == nil || from.CanonicalUUID != from.UUID || to.CanonicalUUID != to.UUID {
		return nil, models.ErrSourceAccountConflict
	}
	if from.Namespace != to.Namespace {
		return nil, models.ErrAccountConsolidationReviewInvalid
	}
	preview, err := s.PreviewConsolidation(ctx, from.UUID, to.UUID)
	if err != nil {
		return nil, err
	}
	ret := &models.AccountConsolidationReviewPreview{Input: input, Source: *from, Destination: *to,
		MemberCount: len(preview.Members), IdentifierCount: len(preview.Identifiers), Digest: preview.Signature,
		IdentifierConflicts: []models.AccountConsolidationConflict{}, Blockers: []string{}}
	for _, conflict := range preview.IdentifierConflicts {
		ret.IdentifierConflicts = append(ret.IdentifierConflicts, models.AccountConsolidationConflict(conflict))
	}
	if len(ret.IdentifierConflicts) != 0 && !input.AcceptIdentifierConflicts {
		ret.Blockers = append(ret.Blockers, "identifiers")
	}
	ret.Ownership = input.Ownership
	if input.OwnershipMode == "preserve" && preview.DefaultOwnership != nil {
		choice := preview.DefaultOwnership
		ret.Ownership = &models.AccountConsolidationChoice{State: choice.State, PerformerUUID: choice.PerformerUUID, PerformerRevision: choice.PerformerRevision}
	}
	if ret.Ownership == nil {
		ret.Blockers = append(ret.Blockers, "ownership")
	} else if ret.Ownership.State == models.AccountOwnershipLinked {
		entity, err := (&ArchiveEntityStore{}).Find(ctx, ret.Ownership.PerformerUUID)
		if err != nil {
			return nil, err
		}
		if entity == nil || entity.Kind != models.ArchivePerformer || entity.State != models.ArchiveEntityActive || entity.Revision != ret.Ownership.PerformerRevision {
			return nil, models.ErrSourceAccountConflict
		}
		ret.Performer, err = accountReviewPerformer(ctx, entity)
		if err != nil {
			return nil, err
		}
	}
	// The component digest binds both accounts, identifiers and current owners.
	// An explicitly chosen performer also carries its independently checked
	// revision. Exact request recovery binds the whole choice, reason and ack.
	ret.Ready = len(ret.Blockers) == 0
	return ret, nil
}

func (s *SourceAccountStore) ConsolidationReview(ctx context.Context, input models.AccountConsolidationReviewApplyInput) (*models.AccountConsolidationReview, error) {
	domain, digest, err := accountConsolidationReviewRequest(input)
	if err != nil {
		return nil, err
	}
	event, err := findAccountConsolidationRequest(ctx, input.RequestUUID, digest)
	if err != nil || event == nil {
		return nil, err
	}
	if event.SourceUUID != domain.SourceUUID || event.DestinationUUID != domain.DestinationUUID || event.Signature != domain.Signature ||
		event.Origin != domain.Origin || event.Reason != domain.Reason || event.AcceptedIdentifierConflicts != domain.AcceptIdentifierConflicts {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return &models.AccountConsolidationReview{Request: input, Consolidation: event.ReviewRecord()}, nil
}

func (s *SourceAccountStore) ApplyConsolidationReview(ctx context.Context, input models.AccountConsolidationReviewApplyInput) (*models.AccountConsolidationReview, bool, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, false, err
	}
	prior, err := s.ConsolidationReview(ctx, input)
	if err != nil || prior != nil {
		return prior, prior != nil, err
	}
	preview, err := s.PreviewConsolidationReview(ctx, input.AccountConsolidationReviewInput)
	if err != nil {
		return nil, false, err
	}
	if preview.Digest != input.Digest {
		return nil, false, models.ErrSourceAccountConflict
	}
	domain, _, err := accountConsolidationReviewRequest(input)
	if err != nil {
		return nil, false, err
	}
	event, err := s.Consolidate(ctx, domain)
	if err != nil {
		return nil, false, err
	}
	return &models.AccountConsolidationReview{Request: input, Consolidation: event.ReviewRecord()}, false, nil
}
