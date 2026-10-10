package sqlite

import (
	"context"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// Knowing who published a post does not subscribe to that account or create
// ownership review work. Only current, direct source associations and ownership
// choices enter the tracked scope. Resolve consolidated accounts, but ignore
// historical source aliases so a replaced association cannot re-enrol an author.
const accountReviewTracked = `(EXISTS(SELECT 1 FROM account_performer_links o WHERE o.account_uuid=a.uuid)
 OR EXISTS(SELECT 1 FROM source_accounts x
 JOIN source_collection_revisions r ON r.account_uuid=x.uuid
 JOIN source_collections c ON c.uuid=r.collection_uuid AND c.revision=r.revision
 WHERE x.canonical_uuid=a.uuid
 AND NOT EXISTS(SELECT 1 FROM source_collection_aliases z WHERE z.alias_uuid=c.uuid)))`

func (s *SourceAccountStore) ReviewAccounts(ctx context.Context, input models.AccountReviewFilter) ([]models.AccountReviewState, error) {
	if input.Limit == 0 {
		input.Limit = 25
	}
	if input.Limit < 1 || input.Limit > 100 || !validAccountText(input.Query, 256, true) ||
		(input.Namespace != "" && !archive.ValidAccountNamespace(input.Namespace)) {
		return nil, models.ErrAccountReviewInvalid
	}
	if input.After != "" {
		id, err := archiveUUID(input.After)
		if err != nil || id != input.After {
			return nil, models.ErrAccountReviewInvalid
		}
	}
	query := `SELECT a.uuid FROM source_accounts a WHERE a.canonical_uuid=a.uuid AND a.uuid>?`
	args := []any{input.After}
	switch input.Scope {
	case "", "tracked":
		query += " AND " + accountReviewTracked
	case "all":
	default:
		return nil, models.ErrAccountReviewInvalid
	}
	if input.Namespace != "" {
		query += " AND a.namespace=?"
		args = append(args, input.Namespace)
	}
	if input.Ownership != "" {
		switch input.Ownership {
		case models.AccountOwnershipLinked, models.AccountOwnershipUnlinked, models.AccountOwnershipUndecided:
		default:
			return nil, models.ErrAccountReviewInvalid
		}
		query += ` AND coalesce((SELECT d.state FROM account_performer_links h
 JOIN account_performer_decisions d ON d.uuid=h.decision_uuid WHERE h.account_uuid=a.uuid),'undecided')=?`
		args = append(args, input.Ownership)
	}
	if input.Query != "" {
		pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(input.Query) + "%"
		query += ` AND (a.label LIKE ? ESCAPE '\' OR EXISTS(SELECT 1 FROM source_account_identifiers i
 WHERE i.canonical_uuid=a.uuid AND i.value LIKE ? ESCAPE '\'))`
		args = append(args, pattern, pattern)
	}
	query += " ORDER BY a.uuid LIMIT ?"
	args = append(args, input.Limit)
	var ids []string
	if err := dbWrapper.Select(ctx, &ids, query, args...); err != nil {
		return nil, err
	}
	ret := make([]models.AccountReviewState, 0, len(ids))
	for _, id := range ids {
		state, err := s.ReviewAccount(ctx, id)
		if err != nil {
			return nil, err
		}
		if state == nil {
			return nil, models.ErrSourcePayloadCorrupt
		}
		ret = append(ret, *state)
	}
	return ret, nil
}

func (s *SourceAccountStore) ReviewAccount(ctx context.Context, id string) (*models.AccountReviewState, error) {
	if normalized, err := archiveUUID(id); err != nil || normalized != id {
		return nil, models.ErrAccountReviewInvalid
	}
	account, err := s.Find(ctx, id)
	if err != nil || account == nil {
		return nil, err
	}
	ret := &models.AccountReviewState{UUID: account.UUID, Namespace: account.Namespace, Label: account.Label,
		Revision: account.Revision, CanonicalUUID: account.CanonicalUUID, RedirectTo: account.RedirectTo,
		Identifiers: []models.AccountReviewIdentifier{}}
	if err := dbWrapper.Get(ctx, &ret.Tracked, "SELECT "+accountReviewTracked+" FROM source_accounts a WHERE a.uuid=?", account.CanonicalUUID); err != nil {
		return nil, err
	}
	decision, err := s.Ownership(ctx, id)
	if err != nil {
		return nil, err
	}
	if decision != nil {
		ret.Ownership, err = accountReviewOwnership(ctx, decision)
		if err != nil {
			return nil, err
		}
	}
	// A card carries a bounded summary; identifiers and their source evidence
	// have separate pagination. Never inspect every catalog to render a link.
	identifiers, err := s.Identifiers(ctx, id, "", 9)
	if err != nil {
		return nil, err
	}
	ret.MoreIdentifiers = len(identifiers) > 8
	for _, identifier := range identifiers[:min(len(identifiers), 8)] {
		ret.Identifiers = append(ret.Identifiers, models.AccountReviewIdentifier{UUID: identifier.UUID,
			AccountUUID: identifier.AccountUUID, Reference: identifier.Reference})
	}
	return ret, nil
}

func accountReviewOwnership(ctx context.Context, decision *models.AccountOwnershipDecision) (*models.AccountReviewOwnership, error) {
	ret := &models.AccountReviewOwnership{DecisionUUID: decision.UUID, Revision: decision.Revision,
		State: decision.State, PerformerUUID: decision.PerformerUUID, Origin: decision.Origin,
		Reason: decision.Reason, CreatedAt: decision.CreatedAt}
	if decision.PerformerUUID != nil {
		entity, err := (&ArchiveEntityStore{}).Resolve(ctx, *decision.PerformerUUID)
		if err != nil {
			return nil, err
		}
		ret.Performer, err = accountReviewPerformer(ctx, entity)
		if err != nil {
			return nil, err
		}
	}
	return ret, nil
}

func (s *SourceAccountStore) ReviewOwnershipHistory(ctx context.Context, id string, after, limit int) ([]models.AccountReviewOwnership, error) {
	if normalized, err := archiveUUID(id); err != nil || normalized != id || after < 0 || limit < 1 || limit > 100 {
		return nil, models.ErrAccountReviewInvalid
	}
	decisions, err := s.OwnershipHistory(ctx, id, after, limit)
	if err != nil {
		return nil, err
	}
	ret := make([]models.AccountReviewOwnership, 0, len(decisions))
	for _, decision := range decisions {
		item, err := accountReviewOwnership(ctx, decision)
		if err != nil {
			return nil, err
		}
		ret = append(ret, *item)
	}
	return ret, nil
}

func accountReviewPerformer(ctx context.Context, entity *models.ArchiveEntity) (*models.AccountReviewPerformer, error) {
	if entity == nil || entity.Kind != models.ArchivePerformer {
		return nil, models.ErrSourcePayloadCorrupt
	}
	ret := &models.AccountReviewPerformer{UUID: entity.UUID, State: entity.State, Revision: entity.Revision, LocalID: entity.LocalID}
	if entity.State == models.ArchiveEntityActive {
		if entity.LocalID == nil {
			return nil, models.ErrSourcePayloadCorrupt
		}
		var name struct{ Name, Disambiguation string }
		if err := dbWrapper.Get(ctx, &name, `SELECT n.name,coalesce(p.disambiguation,'') AS disambiguation FROM performers p
 JOIN performer_names n ON n.performer_id=p.id AND n.position=0 WHERE p.id=?`, *entity.LocalID); err != nil {
			return nil, err
		}
		ret.Name, ret.Disambiguation = name.Name, name.Disambiguation
	}
	return ret, nil
}

func validateAccountOwnershipReviewInput(input models.AccountOwnershipReviewInput) error {
	id, err := archiveUUID(input.AccountUUID)
	if err != nil || id != input.AccountUUID || input.AccountRevision < 1 || !validAccountText(input.Reason, 4096, true) {
		return models.ErrAccountReviewInvalid
	}
	switch input.State {
	case models.AccountOwnershipLinked:
		id, err := archiveUUID(input.PerformerUUID)
		if err != nil || id != input.PerformerUUID || input.PerformerRevision < 1 {
			return models.ErrAccountReviewInvalid
		}
	case models.AccountOwnershipUnlinked, models.AccountOwnershipUndecided:
		if input.PerformerUUID != "" || input.PerformerRevision != 0 {
			return models.ErrAccountReviewInvalid
		}
	default:
		return models.ErrAccountReviewInvalid
	}
	return nil
}

func (s *SourceAccountStore) PreviewOwnership(ctx context.Context, input models.AccountOwnershipReviewInput) (*models.AccountOwnershipPreview, error) {
	if err := validateAccountOwnershipReviewInput(input); err != nil {
		return nil, err
	}
	account, err := s.ReviewAccount(ctx, input.AccountUUID)
	if err != nil {
		return nil, err
	}
	if account == nil || account.RedirectTo != nil || account.CanonicalUUID != account.UUID || account.Revision != input.AccountRevision {
		return nil, models.ErrSourceAccountConflict
	}
	ret := &models.AccountOwnershipPreview{Input: input, Account: *account}
	if input.State == models.AccountOwnershipLinked {
		entity, err := (&ArchiveEntityStore{}).Find(ctx, input.PerformerUUID)
		if err != nil {
			return nil, err
		}
		if entity == nil || entity.Kind != models.ArchivePerformer || entity.State != models.ArchiveEntityActive || entity.Revision != input.PerformerRevision {
			return nil, models.ErrSourceAccountConflict
		}
		ret.Performer, err = accountReviewPerformer(ctx, entity)
		if err != nil {
			return nil, err
		}
	}
	// Evidence timestamps may advance without a new identity claim. Bind the
	// reviewed revisions and selected ownership, not those incidental clocks.
	current := ""
	var owner *models.AccountReviewPerformer
	if account.Ownership != nil {
		current = account.Ownership.DecisionUUID
		owner = account.Ownership.Performer
	}
	ret.Digest, err = sourceSignature("stash-account-ownership-preview-v1", struct {
		Input           models.AccountOwnershipReviewInput `json:"input"`
		CurrentDecision string                             `json:"current_decision"`
		CurrentOwner    *models.AccountReviewPerformer     `json:"current_owner,omitempty"`
	}{input, current, owner})
	return ret, err
}
