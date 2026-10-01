package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/stashapp/stash/pkg/models"
)

func normalizePublisherInput(input *models.CapturePublisherInput) error {
	for _, value := range []*string{&input.UUID, &input.CaptureUUID} {
		id, err := archiveUUID(*value)
		if err != nil {
			return err
		}
		*value = id
	}
	if len(input.ExpectedSignature) != 64 || !validAccountText(input.Reason, 4096, true) {
		return errors.New("invalid publisher review signature or reason")
	}
	if input.Action == "link" {
		id, err := archiveUUID(input.AccountUUID)
		if err != nil {
			return err
		}
		input.AccountUUID = id
	} else if input.AccountUUID != "" {
		return errors.New("only an explicit publisher link accepts an account UUID")
	}
	switch input.Action {
	case "automatic":
		if input.Origin != "" && input.Origin != "capture" {
			return errors.New("automatic publisher choices use capture origin")
		}
		input.Origin = "capture"
	case "link", "create", "unlink", "inherit":
		if input.Origin != "review" && input.Origin != "migration" {
			return errors.New("explicit publisher choice requires review or migration origin")
		}
	default:
		return errors.New("invalid publisher action")
	}
	return nil
}

func publisherCanCreate(preview *models.CapturePublisherPreview) bool {
	if preview.Identity == nil || preview.IdentityError != "" || preview.StableIDMatches || slices.Contains(preview.Conflicts, "captured_namespace_mismatch") {
		return false
	}
	return true
}

func publisherApplyTarget(preview *models.CapturePublisherPreview, input models.CapturePublisherInput) (state string, account *string, create bool, err error) {
	state = "linked"
	switch input.Action {
	case "automatic":
		switch preview.Action {
		case "link":
			account = preview.AccountUUID
		case "create":
			create = true
		default:
			err = models.ErrCapturePublisherConflict
		}
	case "create":
		if !publisherCanCreate(preview) {
			err = models.ErrCapturePublisherConflict
		} else {
			create = true
		}
	case "link":
		if preview.Target == nil || slices.Contains(preview.Conflicts, "target_namespace_mismatch") || slices.Contains(preview.Conflicts, "target_stable_id_conflict") || slices.Contains(preview.Conflicts, "captured_namespace_mismatch") {
			err = models.ErrCapturePublisherConflict
		} else {
			account = &preview.Target.UUID
		}
	case "unlink":
		state = "unlinked"
	case "inherit":
		state = "undecided"
	}
	return
}

func (s *CapturePublisherStore) Apply(ctx context.Context, input models.CapturePublisherInput) (*models.CapturePublisherDecision, error) {
	if err := normalizePublisherInput(&input); err != nil {
		return nil, err
	}
	digest, err := sourceSignature("stash-capture-publisher-request-v1", input)
	if err != nil {
		return nil, err
	}
	previous, err := findCapturePublisherRequest(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if previous != nil {
		if previous.RequestDigest != digest {
			return nil, models.ErrCapturePublisherReplay
		}
		return previous.resolve(), nil
	}
	preview, err := s.Preview(ctx, input.CaptureUUID, input.AccountUUID)
	if err != nil {
		return nil, err
	}
	if preview.Signature != input.ExpectedSignature || preview.PostState != "active" {
		return nil, models.ErrCapturePublisherConflict
	}
	state, account, create, err := publisherApplyTarget(preview, input)
	if err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO capture_publisher_write_context(request_uuid,capture_uuid) VALUES(?,?)", input.UUID, input.CaptureUUID); err != nil {
		return nil, err
	}
	accounts := &SourceAccountStore{}
	if create {
		label := preview.Identity.Label
		if label == "" {
			label = preview.Identity.Namespace + " account"
		}
		created, err := accounts.Create(ctx, preview.Identity.Namespace, label)
		if err != nil {
			return nil, err
		}
		account = &created.UUID
	}
	policy := "explicit-publisher-v1"
	var claims []publisherRecordedClaim
	if state == "linked" && preview.Identity != nil {
		policy = preview.Identity.Policy
		claims, err = recordPublisherClaims(ctx, *account, input.CaptureUUID, preview.Identity)
		if err != nil {
			return nil, err
		}
	}
	revision := 1
	if preview.Current != nil {
		revision = preview.Current.Revision + 1
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO capture_publisher_decisions(uuid,capture_uuid,revision,state,account_uuid,origin,policy,reason,request_digest)
VALUES(?,?,?,?,?,?,?,?,?)`, input.UUID, input.CaptureUUID, revision, state, account, input.Origin, policy, input.Reason, digest); err != nil {
		return nil, err
	}
	for _, claim := range claims {
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO capture_publisher_claims(decision_uuid,identifier_uuid,evidence_key) VALUES(?,?,?)`, input.UUID, claim.identifier, claim.key); err != nil {
			return nil, err
		}
	}
	if _, err := dbWrapper.Exec(ctx, "DELETE FROM capture_publisher_write_context WHERE request_uuid=?", input.UUID); err != nil {
		return nil, err
	}
	return s.Current(ctx, input.CaptureUUID)
}

type publisherRecordedClaim struct{ identifier, key string }

func recordPublisherClaims(ctx context.Context, account, captureUUID string, identity *models.CapturedAccount) ([]publisherRecordedClaim, error) {
	capture, err := (&SourceEvidenceStore{}).FindCapture(ctx, captureUUID)
	if err != nil {
		return nil, err
	}
	if capture == nil {
		return nil, models.ErrCapturePublisherConflict
	}
	accounts := &SourceAccountStore{}
	claims := make([]publisherRecordedClaim, 0, len(identity.Identifiers))
	for _, claim := range identity.Identifiers {
		key, err := sourceSignature("stash-captured-account-claim-v1", []interface{}{identity.Policy, captureUUID, claim})
		if err != nil {
			return nil, err
		}
		details, err := json.Marshal(map[string]string{"capture_uuid": captureUUID, "policy": identity.Policy, "path": claim.Path})
		if err != nil {
			return nil, err
		}
		identifier, err := accounts.ObserveIdentifier(ctx, account, claim.Reference, models.AccountIdentifierEvidence{
			Key: key, Basis: claim.Basis, Origin: capture.Origin, Details: details, FirstObserved: capture.CapturedAt, LastObserved: capture.CapturedAt})
		if err != nil {
			return nil, err
		}
		claims = append(claims, publisherRecordedClaim{identifier: identifier.UUID, key: key})
	}
	return claims, nil
}
