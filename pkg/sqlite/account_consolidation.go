package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	hexEncoding "encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

const maxAccountMembers = 4096
const maxAccountReviewIdentifiers = 8192

func sourceAccountMembers(ctx context.Context, root string) ([]*models.SourceAccount, error) {
	var rows []sourceAccountRow
	err := dbWrapper.Select(ctx, &rows, `SELECT a.*,c.destination_uuid AS redirect_to FROM source_accounts a
LEFT JOIN source_account_consolidations c ON c.source_uuid=a.uuid WHERE a.canonical_uuid=? ORDER BY a.uuid LIMIT 4097`, root)
	if err != nil {
		return nil, err
	}
	if len(rows) > maxAccountMembers {
		return nil, errors.New("source account has more than 4096 consolidated records")
	}
	members := make([]*models.SourceAccount, 0, len(rows))
	for _, row := range rows {
		members = append(members, row.resolve())
	}
	return members, nil
}

func sourceAccountMemberIdentifiers(ctx context.Context, members []*models.SourceAccount, after string, limit int) ([]*models.AccountIdentifier, error) {
	if len(members) == 0 {
		return []*models.AccountIdentifier{}, nil
	}
	args := make([]interface{}, 0, len(members)+2)
	for _, member := range members {
		args = append(args, member.UUID)
	}
	args = append(args, after, limit)
	var rows []accountIdentifierRow
	if err := dbWrapper.Select(ctx, &rows, "SELECT * FROM source_account_identifiers WHERE account_uuid IN "+getInBinding(len(members))+" AND uuid>? ORDER BY uuid LIMIT ?", args...); err != nil {
		return nil, err
	}
	ret := make([]*models.AccountIdentifier, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, row.resolve())
	}
	return ret, nil
}

func accountReviewDigest(value interface{}) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hexEncoding.EncodeToString(digest[:]), nil
}

func (s *SourceAccountStore) PreviewConsolidation(ctx context.Context, source, destination string) (*models.AccountConsolidationPreview, error) {
	from, err := s.Find(ctx, source)
	if err != nil {
		return nil, err
	}
	to, err := s.Find(ctx, destination)
	if err != nil {
		return nil, err
	}
	if from == nil || to == nil || from.UUID == to.UUID || from.RedirectTo != nil || to.RedirectTo != nil || from.CanonicalUUID != from.UUID || to.CanonicalUUID != to.UUID {
		return nil, models.ErrSourceAccountConflict
	}
	if from.Namespace != to.Namespace {
		return nil, errors.New("account consolidation requires the same service namespace; link separate accounts to their performer instead")
	}
	left, err := sourceAccountMembers(ctx, from.UUID)
	if err != nil {
		return nil, err
	}

	right, err := sourceAccountMembers(ctx, to.UUID)
	if err != nil {
		return nil, err
	}
	if len(left)+len(right) > maxAccountMembers {
		return nil, errors.New("account consolidation exceeds 4096 members")
	}
	ret := &models.AccountConsolidationPreview{Source: from, Destination: to, Members: append(left, right...)}
	slices.SortFunc(ret.Members, func(a, b *models.SourceAccount) int { return strings.Compare(a.UUID, b.UUID) })
	ret.Identifiers, err = sourceAccountMemberIdentifiers(ctx, ret.Members, "", maxAccountReviewIdentifiers+1)
	if err != nil {
		return nil, err
	}
	if len(ret.Identifiers) > maxAccountReviewIdentifiers {
		return nil, errors.New("account consolidation exceeds 8192 identifiers")
	}
	ret.SourceOwnership, err = s.Ownership(ctx, from.UUID)
	if err != nil {
		return nil, err
	}
	ret.DestinationOwnership, err = s.Ownership(ctx, to.UUID)
	if err != nil {
		return nil, err
	}
	requested := make(map[string]bool)
	for _, choice := range []*models.AccountOwnershipDecision{ret.SourceOwnership, ret.DestinationOwnership} {
		if choice != nil && choice.PerformerUUID != nil {
			requested[*choice.PerformerUUID] = true
		}
	}
	resolved, err := sourceGalleryIdentities(ctx, requested)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	for _, p := range resolved {
		if p.Kind != models.ArchivePerformer {
			return nil, errors.New("account ownership has the wrong identity kind")
		}
		if !seen[p.UUID] {
			ret.Performers = append(ret.Performers, p)
			seen[p.UUID] = true
		}
	}
	slices.SortFunc(ret.Performers, func(a, b *models.ArchiveEntity) int { return strings.Compare(a.UUID, b.UUID) })
	selectOwnership := func(choice *models.AccountOwnershipDecision) *models.AccountOwnershipSelection {
		if choice == nil || choice.State == models.AccountOwnershipUndecided {
			return &models.AccountOwnershipSelection{State: models.AccountOwnershipUndecided}
		}
		result := &models.AccountOwnershipSelection{State: choice.State}
		if choice.State == models.AccountOwnershipLinked {
			p := resolved[*choice.PerformerUUID]
			if p.State != models.ArchiveEntityActive {
				return nil
			}
			result.PerformerUUID, result.PerformerRevision = p.UUID, p.Revision
		}
		return result
	}
	a, b := selectOwnership(ret.SourceOwnership), selectOwnership(ret.DestinationOwnership)
	if a != nil && b != nil {
		switch {
		case *a == *b:
			ret.DefaultOwnership = a
		case a.State == models.AccountOwnershipUndecided:
			ret.DefaultOwnership = b
		case b.State == models.AccountOwnershipUndecided:
			ret.DefaultOwnership = a
		}
	}
	values := make(map[string]map[string]bool)
	for _, id := range ret.Identifiers {
		// Only opaque stable-ID claims conflict. Several historical handles and
		// profile locators are normal; they never justify consolidation by name.
		stable := stableAccountReference(id.Reference)
		if !stable {
			continue
		}
		key := id.Reference.Namespace + "\x00" + id.Reference.Kind
		if values[key] == nil {
			values[key] = make(map[string]bool)
		}
		values[key][id.Reference.Value] = true
	}
	for key, items := range values {
		if len(items) < 2 {
			continue
		}
		parts := strings.SplitN(key, "\x00", 2)
		conflict := models.AccountIdentifierConflict{Namespace: parts[0], Kind: parts[1]}
		for value := range items {
			conflict.Values = append(conflict.Values, value)
		}
		slices.Sort(conflict.Values)
		ret.IdentifierConflicts = append(ret.IdentifierConflicts, conflict)
	}
	slices.SortFunc(ret.IdentifierConflicts, func(a, b models.AccountIdentifierConflict) int {
		return strings.Compare(a.Namespace+"\x00"+a.Kind, b.Namespace+"\x00"+b.Kind)
	})
	ret.Signature, err = accountReviewDigest(ret)
	return ret, err
}

type accountConsolidationRow struct {
	UUID                string    `db:"uuid"`
	Sequence            int       `db:"sequence"`
	SourceUUID          string    `db:"source_uuid"`
	DestinationUUID     string    `db:"destination_uuid"`
	SourceRevision      int       `db:"source_revision"`
	DestinationRevision int       `db:"destination_revision"`
	OwnershipUUID       string    `db:"ownership_decision_uuid"`
	Signature           string    `db:"signature"`
	RequestDigest       string    `db:"request_digest"`
	Origin              string    `db:"origin"`
	Reason              string    `db:"reason"`
	AcceptedConflicts   bool      `db:"accepted_identifier_conflicts"`
	CreatedAt           Timestamp `db:"created_at"`
}

func (r accountConsolidationRow) resolve() *models.AccountConsolidation {
	return &models.AccountConsolidation{UUID: r.UUID, Sequence: r.Sequence, SourceUUID: r.SourceUUID, DestinationUUID: r.DestinationUUID,
		SourceRevision: r.SourceRevision, DestinationRevision: r.DestinationRevision, OwnershipDecisionUUID: r.OwnershipUUID, Signature: r.Signature,
		Origin: r.Origin, Reason: r.Reason, AcceptedIdentifierConflicts: r.AcceptedConflicts, CreatedAt: r.CreatedAt.Timestamp}
}

func accountConsolidationRequest(input models.AccountConsolidationInput) (models.AccountConsolidationInput, string, error) {
	var err error
	input.SourceUUID, err = archiveUUID(input.SourceUUID)
	if err != nil {
		return input, "", err
	}
	input.DestinationUUID, err = archiveUUID(input.DestinationUUID)
	if err != nil {
		return input, "", err
	}
	if (input.Origin != "review" && input.Origin != "migration") || !validAccountText(input.Reason, 4096, true) {
		return input, "", errors.New("account consolidation requires review or migration and a valid reason")
	}
	if input.OwnershipMode != "preserve" && input.OwnershipMode != "choose" {
		return input, "", errors.New("choose how account ownership will be resolved")
	}
	if input.OwnershipMode == "preserve" && input.Ownership != (models.AccountOwnershipSelection{}) {
		return input, "", errors.New("preserving ownership does not accept a replacement choice")
	}
	if len(input.Signature) != 64 {
		return input, "", models.ErrSourceAccountConflict
	}
	input.UUID = ""
	digest, err := accountReviewDigest(input)
	return input, digest, err
}

func findAccountConsolidationRequest(ctx context.Context, id, digest string) (*models.AccountConsolidation, error) {
	var old accountConsolidationRow
	err := dbWrapper.Get(ctx, &old, "SELECT * FROM source_account_consolidations WHERE uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if old.RequestDigest != digest {
		return nil, models.ErrAccountConsolidationReplay
	}
	return old.resolve(), nil
}

func (s *SourceAccountStore) Consolidate(ctx context.Context, input models.AccountConsolidationInput) (*models.AccountConsolidation, error) {
	id := input.UUID
	input, digest, err := accountConsolidationRequest(input)
	if err != nil {
		return nil, err
	}
	if id == "" {
		id = uuid.NewString()
	} else {
		id, err = archiveUUID(id)
		if err != nil {
			return nil, err
		}
	}
	old, err := findAccountConsolidationRequest(ctx, id, digest)
	if err != nil || old != nil {
		return old, err
	}
	preview, err := s.PreviewConsolidation(ctx, input.SourceUUID, input.DestinationUUID)
	if err != nil {
		return nil, err
	}
	if preview.Signature != input.Signature {
		return nil, models.ErrSourceAccountConflict
	}
	if len(preview.IdentifierConflicts) > 0 && !input.AcceptIdentifierConflicts {
		return nil, models.ErrAccountIdentifierResolution
	}
	selected := input.Ownership
	if input.OwnershipMode == "preserve" {
		if preview.DefaultOwnership == nil {
			return nil, models.ErrAccountOwnershipResolution
		}
		selected = *preview.DefaultOwnership
	}
	var ret *models.AccountConsolidation
	err = withAccountConsolidation(ctx, preview.Source.UUID, preview.Destination.UUID, func() error {
		decision, err := s.DecideOwnership(ctx, models.AccountOwnershipInput{AccountUUID: preview.Destination.UUID, ExpectedAccountRevision: preview.Destination.Revision,
			State: selected.State, PerformerUUID: selected.PerformerUUID, ExpectedPerformerRevision: selected.PerformerRevision, Origin: input.Origin, Reason: input.Reason})
		if err != nil {
			return err
		}
		result, err := dbWrapper.Exec(ctx, "UPDATE source_accounts SET revision=revision+1 WHERE uuid=? AND revision=?", preview.Source.UUID, preview.Source.Revision)
		if err := checkArchiveIdentityUpdate(result, err); err != nil {
			return err
		}
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_account_consolidations(uuid,source_uuid,destination_uuid,source_revision,destination_revision,
 ownership_decision_uuid,signature,request_digest,origin,reason,accepted_identifier_conflicts) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			id, preview.Source.UUID, preview.Destination.UUID, preview.Source.Revision+1, preview.Destination.Revision+1, decision.UUID, preview.Signature, digest, input.Origin, input.Reason, input.AcceptIdentifierConflicts); err != nil {
			return err
		}
		var row accountConsolidationRow
		if err := dbWrapper.Get(ctx, &row, "SELECT * FROM source_account_consolidations WHERE uuid=?", id); err != nil {
			return err
		}
		ret = row.resolve()
		return syncAccountProfileURLs(ctx, preview.Destination.UUID)
	})
	return ret, err
}

func (s *SourceAccountStore) ConsolidationHistory(ctx context.Context, value string, after, limit int) ([]models.AccountConsolidation, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	limit, err = sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	if after < 0 {
		return nil, errors.New("invalid consolidation history cursor")
	}
	var rows []accountConsolidationRow
	if err := dbWrapper.Select(ctx, &rows, `SELECT * FROM source_account_consolidations WHERE (source_uuid=? OR destination_uuid=?)
 AND sequence>? ORDER BY sequence LIMIT ?`, id, id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.AccountConsolidation, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, *row.resolve())
	}
	return ret, nil
}

func validateAccountConsolidationCommit(ctx context.Context) error {
	var unfinished bool
	if err := dbWrapper.Get(ctx, &unfinished, "SELECT EXISTS(SELECT 1 FROM source_account_consolidation_context)"); err != nil {
		return err
	}
	if unfinished {
		return errors.New("unfinished source account consolidation")
	}
	return nil
}

func withAccountConsolidation(ctx context.Context, source, destination string, fn func() error) error {
	if _, err := getTx(ctx); err != nil {
		return err
	}
	txn.AddPreCommitHook(ctx, validateAccountConsolidationCommit)
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO source_account_consolidation_context(source_uuid,destination_uuid) VALUES(?,?)", source, destination); err != nil {
		return err
	}
	if err := fn(); err != nil {
		return err
	}
	_, err := dbWrapper.Exec(ctx, "DELETE FROM source_account_consolidation_context WHERE source_uuid=?", source)
	return err
}
