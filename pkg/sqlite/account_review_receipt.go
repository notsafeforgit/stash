package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

func accountOwnershipReviewRequest(input models.AccountOwnershipReviewApplyInput) ([]byte, error) {
	if err := validateAccountOwnershipReviewInput(input.AccountOwnershipReviewInput); err != nil {
		return nil, err
	}
	id, err := archiveUUID(input.RequestUUID)
	if err != nil || id != input.RequestUUID || !archive.ValidSHA256(input.Digest) {
		return nil, models.ErrAccountReviewInvalid
	}
	encoded, err := json.Marshal(input)
	if err != nil || len(encoded) > 16384 {
		return nil, models.ErrAccountReviewInvalid
	}
	return encoded, nil
}

func accountOwnershipReviewSignature(input models.AccountOwnershipReviewApplyInput, decision string) (string, error) {
	return sourceSignature("stash-account-ownership-receipt-v1", struct {
		Request      models.AccountOwnershipReviewApplyInput `json:"request"`
		DecisionUUID string                                  `json:"decision_uuid"`
	}{input, decision})
}

func (s *SourceAccountStore) ApplyOwnershipReview(ctx context.Context, input models.AccountOwnershipReviewApplyInput) (*models.AccountOwnershipReview, bool, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, false, err
	}
	encoded, err := accountOwnershipReviewRequest(input)
	if err != nil {
		return nil, false, err
	}
	prior, err := s.OwnershipReview(ctx, input.RequestUUID)
	if err != nil {
		return nil, false, err
	}
	if prior != nil {
		previous, err := accountOwnershipReviewRequest(prior.Request)
		if err != nil {
			return nil, false, err
		}
		if string(previous) != string(encoded) {
			return nil, false, models.ErrAccountReviewReplay
		}
		return prior, true, nil
	}
	preview, err := s.PreviewOwnership(ctx, input.AccountOwnershipReviewInput)
	if err != nil {
		return nil, false, err
	}
	if input.Digest != preview.Digest {
		return nil, false, models.ErrSourceAccountConflict
	}
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return models.ErrAccountReviewInvalid
		}
		return nil
	})
	decision, err := s.DecideOwnership(ctx, models.AccountOwnershipInput{
		AccountUUID: input.AccountUUID, ExpectedAccountRevision: input.AccountRevision, State: input.State,
		PerformerUUID: input.PerformerUUID, ExpectedPerformerRevision: input.PerformerRevision,
		Origin: "review", Reason: input.Reason,
	})
	if err != nil {
		return nil, false, err
	}
	signature, err := accountOwnershipReviewSignature(input, decision.UUID)
	if err != nil {
		return nil, false, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO account_ownership_reviews(request_uuid,account_uuid,decision_uuid,request_json,signature)
 VALUES(?,?,?,?,?)`, input.RequestUUID, input.AccountUUID, decision.UUID, string(encoded), signature); err != nil {
		return nil, false, err
	}
	ret, err := s.OwnershipReview(ctx, input.RequestUUID)
	if err == nil && ret == nil {
		err = models.ErrSourcePayloadCorrupt
	}
	complete = err == nil
	return ret, false, err
}

func (s *SourceAccountStore) OwnershipReview(ctx context.Context, id string) (*models.AccountOwnershipReview, error) {
	if normalized, err := archiveUUID(id); err != nil || normalized != id {
		return nil, models.ErrAccountReviewInvalid
	}
	return readAccountOwnershipReview(func(dest any, query string, args ...any) error { return dbWrapper.Get(ctx, dest, query, args...) }, id)
}

func readAccountOwnershipReview(get enrichmentGet, id string) (*models.AccountOwnershipReview, error) {
	var row struct {
		RequestUUID  string    `db:"request_uuid"`
		AccountUUID  string    `db:"account_uuid"`
		DecisionUUID string    `db:"decision_uuid"`
		RequestJSON  string    `db:"request_json"`
		Signature    string    `db:"signature"`
		CreatedAt    Timestamp `db:"created_at"`
	}
	if err := get(&row, "SELECT * FROM account_ownership_reviews WHERE request_uuid=?", id); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var input models.AccountOwnershipReviewApplyInput
	if err := json.Unmarshal([]byte(row.RequestJSON), &input); err != nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	encoded, err := accountOwnershipReviewRequest(input)
	signature, signatureErr := accountOwnershipReviewSignature(input, row.DecisionUUID)
	if err != nil || signatureErr != nil || string(encoded) != row.RequestJSON || signature != row.Signature ||
		input.RequestUUID != row.RequestUUID || input.AccountUUID != row.AccountUUID {
		return nil, models.ErrSourcePayloadCorrupt
	}
	var decision accountOwnershipRow
	if err := get(&decision, "SELECT * FROM account_performer_decisions WHERE uuid=?", row.DecisionUUID); err != nil {
		return nil, err
	}
	if decision.AccountUUID != input.AccountUUID || decision.State != input.State || decision.Origin != "review" ||
		decision.Reason != input.Reason || decision.Revision-1 != input.AccountRevision ||
		decision.PerformerUUID.Valid != (input.State == models.AccountOwnershipLinked) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	// UUID adoption can rename the decision's performer FK. Keep the original
	// request intact and verify its redirect chain, including after later merges.
	seen := map[string]bool{}
	for expected := input.PerformerUUID; expected != decision.PerformerUUID.String; {
		if expected == "" || seen[expected] || len(seen) >= 1024 {
			return nil, models.ErrSourcePayloadCorrupt
		}
		seen[expected] = true
		var next sql.NullString
		if err := get(&next, "SELECT redirect_to FROM archive_entities WHERE uuid=? AND kind='performer'", expected); err != nil || !next.Valid {
			return nil, models.ErrSourcePayloadCorrupt
		}
		expected = next.String
	}
	return &models.AccountOwnershipReview{RequestUUID: id, DecisionUUID: row.DecisionUUID, Request: input, CreatedAt: row.CreatedAt.Timestamp}, nil
}
