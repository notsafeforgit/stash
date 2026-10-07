package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/stashapp/stash/pkg/models"
)

type CapturePublisherStore struct{}

type capturePublisherRow struct {
	UUID                 string    `db:"uuid"`
	CaptureUUID          string    `db:"capture_uuid"`
	Revision             int       `db:"revision"`
	State                string    `db:"state"`
	AccountUUID          *string   `db:"account_uuid"`
	CanonicalAccountUUID *string   `db:"canonical_account_uuid"`
	Origin               string    `db:"origin"`
	Policy               string    `db:"policy"`
	Reason               string    `db:"reason"`
	RequestDigest        string    `db:"request_digest"`
	CreatedAt            Timestamp `db:"created_at"`
}

const capturePublisherSelect = `SELECT d.*,a.canonical_uuid AS canonical_account_uuid FROM capture_publisher_decisions d
LEFT JOIN source_accounts a ON a.uuid=d.account_uuid`

func (r capturePublisherRow) resolve() *models.CapturePublisherDecision {
	return &models.CapturePublisherDecision{UUID: r.UUID, CaptureUUID: r.CaptureUUID, Revision: r.Revision, State: r.State,
		AccountUUID: r.AccountUUID, CanonicalAccountUUID: r.CanonicalAccountUUID, Origin: r.Origin, Policy: r.Policy, Reason: r.Reason, CreatedAt: r.CreatedAt.Timestamp}
}

func findCapturePublisherRequest(ctx context.Context, id string) (*capturePublisherRow, error) {
	var row capturePublisherRow
	if err := dbWrapper.Get(ctx, &row, capturePublisherSelect+" WHERE d.uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func (s *CapturePublisherStore) Current(ctx context.Context, value string) (*models.CapturePublisherDecision, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	var row capturePublisherRow
	if err := dbWrapper.Get(ctx, &row, capturePublisherSelect+` JOIN capture_publisher_heads h ON h.decision_uuid=d.uuid AND h.capture_uuid=d.capture_uuid
WHERE h.capture_uuid=?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.resolve(), nil
}

func (s *CapturePublisherStore) History(ctx context.Context, value string, after, limit int) ([]models.CapturePublisherDecision, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	if after < 0 {
		return nil, errors.New("invalid publisher history cursor")
	}
	limit, err = sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	var rows []capturePublisherRow
	if err := dbWrapper.Select(ctx, &rows, capturePublisherSelect+" WHERE d.capture_uuid=? AND d.revision>? ORDER BY d.revision LIMIT ?", id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.CapturePublisherDecision, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, *row.resolve())
	}
	return ret, nil
}

// PostAccounts reports all selected publishers, not a guessed single owner or
// depicted performer. Account consolidation is resolved without rewriting history.
// CROSS JOIN keeps the selected post's capture range outermost; a small table's
// planner statistics must not turn this into a library-wide decision scan.
func (s *CapturePublisherStore) PostAccounts(ctx context.Context, value, after string, limit int) ([]*models.SourceAccount, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	after, limit, err = sourceDefinitionPage(after, limit)
	if err != nil {
		return nil, err
	}
	var rows []sourceAccountRow
	if err := dbWrapper.Select(ctx, &rows, `SELECT DISTINCT r.* FROM source_post_identities i
CROSS JOIN source_captures c INDEXED BY source_captures_scope ON c.post_uuid=i.post_uuid
CROSS JOIN capture_publisher_heads h ON h.capture_uuid=c.uuid
CROSS JOIN capture_publisher_decisions d ON d.uuid=h.decision_uuid AND d.state='linked'
CROSS JOIN source_accounts a ON a.uuid=d.account_uuid CROSS JOIN source_accounts r ON r.uuid=a.canonical_uuid
WHERE i.canonical_uuid=(SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=?) AND r.uuid>? ORDER BY r.uuid LIMIT ?`, id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]*models.SourceAccount, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, row.resolve())
	}
	return ret, nil
}
