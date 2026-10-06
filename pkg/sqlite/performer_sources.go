package sqlite

import (
	"context"

	"github.com/stashapp/stash/pkg/models"
)

const performerSourceAliasesQuery = `WITH RECURSIVE identities(uuid) AS (
SELECT ? UNION SELECT e.uuid FROM archive_entities e JOIN identities i ON e.redirect_to=i.uuid LIMIT 1025
) SELECT uuid FROM identities ORDER BY uuid`

func performerSourceScope(ctx context.Context, id, after string, limit int) (*models.AccountReviewPerformer, []string, error) {
	if normalized, err := archiveUUID(id); err != nil || normalized != id || limit < 1 || limit > 100 {
		return nil, nil, models.ErrAccountReviewInvalid
	}
	if after != "" {
		if normalized, err := archiveUUID(after); err != nil || normalized != after {
			return nil, nil, models.ErrAccountReviewInvalid
		}
	}
	entity, err := (&ArchiveEntityStore{}).Resolve(ctx, id)
	if err != nil || entity == nil {
		return nil, nil, err
	}
	if entity.Kind != models.ArchivePerformer {
		return nil, nil, models.ErrAccountReviewInvalid
	}
	performer, err := accountReviewPerformer(ctx, entity)
	if err != nil {
		return nil, nil, err
	}
	var ids []string
	if err := dbWrapper.Select(ctx, &ids, performerSourceAliasesQuery, entity.UUID); err != nil {
		return nil, nil, err
	}
	if len(ids) > 1024 {
		return nil, nil, models.ErrPerformerSourceLimit
	}
	return performer, ids, nil
}

func performerSourceAccountsQuery(ids []string, after string, limit int) (string, []any) {
	args := make([]any, 0, len(ids)+2)
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, after, limit)
	return `SELECT a.uuid FROM account_performer_decisions d INDEXED BY account_performer_decisions_performer
CROSS JOIN account_performer_links h INDEXED BY sqlite_autoindex_account_performer_links_1 ON h.account_uuid=d.account_uuid AND h.decision_uuid=d.uuid
CROSS JOIN source_accounts a ON a.uuid=h.account_uuid
WHERE d.performer_uuid IN ` + getInBinding(len(ids)) + ` AND d.state='linked'
AND a.canonical_uuid=a.uuid AND a.uuid>?
GROUP BY a.uuid ORDER BY a.uuid LIMIT ?`, args
}

func (s *SourceAccountStore) PerformerAccounts(ctx context.Context, id, after string, limit int) (*models.PerformerSourceAccounts, error) {
	performer, ids, err := performerSourceScope(ctx, id, after, limit)
	if err != nil || performer == nil {
		return nil, err
	}
	query, args := performerSourceAccountsQuery(ids, after, limit)
	var accounts []string
	if err := dbWrapper.Select(ctx, &accounts, query, args...); err != nil {
		return nil, err
	}
	ret := &models.PerformerSourceAccounts{RequestedUUID: id, Performer: *performer, Accounts: []models.AccountReviewState{}}
	for _, account := range accounts {
		review, err := s.ReviewAccount(ctx, account)
		if err != nil {
			return nil, err
		}
		if review == nil || review.Ownership == nil || review.Ownership.Performer == nil || review.Ownership.Performer.UUID != performer.UUID {
			return nil, models.ErrSourcePayloadCorrupt
		}
		ret.Accounts = append(ret.Accounts, *review)
	}
	return ret, nil
}

func (s *SourceAccountStore) PerformerIdentities(ctx context.Context, id, after string, limit int) (*models.PerformerSourceIdentities, error) {
	performer, ids, err := performerSourceScope(ctx, id, after, limit)
	if err != nil || performer == nil {
		return nil, err
	}
	args := make([]any, 0, len(ids)+2)
	for _, alias := range ids {
		args = append(args, alias)
	}
	args = append(args, after, limit)
	var rows []archiveEntityRow
	if err := dbWrapper.Select(ctx, &rows, `SELECT * FROM archive_entities WHERE uuid IN `+getInBinding(len(ids))+` AND uuid>? ORDER BY uuid LIMIT ?`, args...); err != nil {
		return nil, err
	}
	ret := &models.PerformerSourceIdentities{RequestedUUID: id, Performer: *performer, Identities: []models.PerformerSourceIdentity{}}
	for _, row := range rows {
		entity := row.resolve()
		ret.Identities = append(ret.Identities, models.PerformerSourceIdentity{UUID: entity.UUID, Revision: entity.Revision, State: entity.State,
			OriginalID: entity.OriginalID, RedirectTo: entity.RedirectTo, CreatedAt: entity.CreatedAt, RetiredAt: entity.RetiredAt})
	}
	return ret, nil
}
