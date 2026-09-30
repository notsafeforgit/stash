package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type SourceAccountStore struct{}

type sourceAccountRow struct {
	UUID      string    `db:"uuid"`
	Namespace string    `db:"namespace"`
	Label     string    `db:"label"`
	Revision  int       `db:"revision"`
	CreatedAt Timestamp `db:"created_at"`
}

func (r sourceAccountRow) resolve() *models.SourceAccount {
	return &models.SourceAccount{UUID: r.UUID, Namespace: r.Namespace, Label: r.Label, Revision: r.Revision, CreatedAt: r.CreatedAt.Timestamp}
}

func (s *SourceAccountStore) Create(ctx context.Context, namespace, label string) (*models.SourceAccount, error) {
	if !archive.ValidAccountNamespace(namespace) || !validAccountText(label, 1024, true) {
		return nil, errors.New("invalid source account namespace or label")
	}
	id := uuid.NewString()
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO source_accounts(uuid, namespace, label) VALUES (?, ?, ?)", id, namespace, label); err != nil {
		return nil, err
	}
	return s.Find(ctx, id)
}

func (s *SourceAccountStore) Find(ctx context.Context, value string) (*models.SourceAccount, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	var row sourceAccountRow
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM source_accounts WHERE uuid = ?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.resolve(), nil
}

func accountPage(after string, limit int) (string, int, error) {
	if after != "" {
		var err error
		after, err = archiveUUID(after)
		if err != nil {
			return "", 0, err
		}
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 1000 {
		return "", 0, errors.New("account query limit must be between 1 and 1000")
	}
	return after, limit, nil
}

func (s *SourceAccountStore) Lookup(ctx context.Context, reference models.AccountReference, after string, limit int) ([]*models.SourceAccount, error) {
	ref, err := archive.NormalizeAccountReference(reference)
	if err != nil {
		return nil, err
	}
	after, limit, err = accountPage(after, limit)
	if err != nil {
		return nil, err
	}
	var rows []sourceAccountRow
	if err := dbWrapper.Select(ctx, &rows, `SELECT a.* FROM source_account_identifiers i
JOIN source_accounts a ON a.uuid = i.account_uuid
WHERE i.namespace = ? AND i.kind = ? AND i.value = ? AND i.account_uuid > ?
ORDER BY i.account_uuid LIMIT ?`, ref.Namespace, ref.Kind, ref.Value, after, limit); err != nil {
		return nil, err
	}
	ret := make([]*models.SourceAccount, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, row.resolve())
	}
	return ret, nil
}

type accountIdentifierRow struct {
	UUID        string `db:"uuid"`
	AccountUUID string `db:"account_uuid"`
	Namespace   string `db:"namespace"`
	Kind        string `db:"kind"`
	Value       string `db:"value"`
}

func (r accountIdentifierRow) resolve() *models.AccountIdentifier {
	return &models.AccountIdentifier{UUID: r.UUID, AccountUUID: r.AccountUUID,
		Reference: models.AccountReference{Namespace: r.Namespace, Kind: r.Kind, Value: r.Value}}
}

func validAccountText(value string, limit int, empty bool) bool {
	return (empty || value != "") && len(value) <= limit && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}

func accountEvidenceJSON(value json.RawMessage) (string, error) {
	if len(value) == 0 {
		return "{}", nil
	}
	if len(value) > 65536 || !utf8.Valid(value) || !json.Valid(value) {
		return "", errors.New("account identifier evidence must be a JSON object of at most 64 KiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber() // service IDs must never pass through float64
	object, err := accountEvidenceValue(decoder, 0)
	if err != nil {
		return "", err
	}
	if _, ok := object.(map[string]interface{}); !ok {
		return "", errors.New("account identifier evidence must be a JSON object")
	}
	canonical, err := json.Marshal(object)
	return string(canonical), err
}

func accountEvidenceValue(decoder *json.Decoder, depth int) (interface{}, error) {
	if depth > 64 {
		return nil, errors.New("account identifier evidence exceeds nesting limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		object := make(map[string]interface{})
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, errors.New("invalid account evidence object key")
			}
			if _, duplicate := object[name]; duplicate {
				return nil, errors.New("account evidence object contains duplicate keys")
			}
			object[name], err = accountEvidenceValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
		}
		_, err := decoder.Token()
		return object, err
	case json.Delim('['):
		array := make([]interface{}, 0)
		for decoder.More() {
			value, err := accountEvidenceValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		_, err := decoder.Token()
		return array, err
	default:
		return token, nil
	}
}

func validateAccountEvidence(e models.AccountIdentifierEvidence) (string, error) {
	if !validAccountText(e.Key, 512, false) || !validAccountText(e.Basis, 128, false) || !validAccountText(e.Origin, 128, false) {
		return "", errors.New("account identifier evidence requires a key, basis and origin")
	}
	if e.FirstObserved.IsZero() || e.LastObserved.IsZero() || e.FirstObserved.After(e.LastObserved) ||
		e.FirstObserved.Year() < 1 || e.LastObserved.Year() > 9999 {
		return "", errors.New("invalid account identifier observation interval")
	}
	return accountEvidenceJSON(e.Details)
}

const accountObservationTimeFormat = "2006-01-02T15:04:05.000000000Z"

func (s *SourceAccountStore) ObserveIdentifier(ctx context.Context, value string, reference models.AccountReference, evidence models.AccountIdentifierEvidence) (*models.AccountIdentifier, error) {
	account, err := s.Find(ctx, value)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, models.ErrSourceAccountConflict
	}
	ref, err := archive.NormalizeAccountReference(reference)
	if err != nil {
		return nil, err
	}
	if ref.Namespace != account.Namespace && ref.Namespace != "url" {
		return nil, errors.New("account identifier belongs to a different service namespace")
	}
	details, err := validateAccountEvidence(evidence)
	if err != nil {
		return nil, err
	}
	result, err := dbWrapper.Exec(ctx, `INSERT INTO source_account_identifiers(uuid, account_uuid, namespace, kind, value)
VALUES (?, ?, ?, ?, ?) ON CONFLICT(account_uuid, namespace, kind, value) DO NOTHING`, uuid.NewString(), account.UUID, ref.Namespace, ref.Kind, ref.Value)
	if err != nil {
		return nil, err
	}
	addedIdentifier, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	var identifier accountIdentifierRow
	if err := dbWrapper.Get(ctx, &identifier, `SELECT * FROM source_account_identifiers WHERE account_uuid = ? AND namespace = ? AND kind = ? AND value = ?`, account.UUID, ref.Namespace, ref.Kind, ref.Value); err != nil {
		return nil, err
	}
	var old struct {
		Basis   string `db:"basis"`
		Origin  string `db:"origin"`
		Details string `db:"details"`
	}
	err = dbWrapper.Get(ctx, &old, `SELECT basis, origin, details FROM source_account_identifier_evidence
WHERE identifier_uuid = ? AND evidence_key = ?`, identifier.UUID, evidence.Key)
	newEvidence := errors.Is(err, sql.ErrNoRows)
	if err != nil && !newEvidence {
		return nil, err
	}
	if !newEvidence && (old.Basis != evidence.Basis || old.Origin != evidence.Origin || old.Details != details) {
		return nil, models.ErrAccountEvidenceConflict
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_account_identifier_evidence
(identifier_uuid, evidence_key, basis, origin, details, first_observed, last_observed)
VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(identifier_uuid, evidence_key) DO UPDATE SET
first_observed = min(first_observed, excluded.first_observed), last_observed = max(last_observed, excluded.last_observed)`,
		identifier.UUID, evidence.Key, evidence.Basis, evidence.Origin, details,
		evidence.FirstObserved.UTC().Format(accountObservationTimeFormat), evidence.LastObserved.UTC().Format(accountObservationTimeFormat)); err != nil {
		return nil, err
	}
	// Re-observing identical evidence does not invalidate a pending review. New
	// claims or evidence do, even when the selected ownership is unchanged.
	if addedIdentifier != 0 || newEvidence {
		if _, err := dbWrapper.Exec(ctx, "UPDATE source_accounts SET revision = revision + 1 WHERE uuid = ?", account.UUID); err != nil {
			return nil, err
		}
	}
	return identifier.resolve(), nil
}

func (s *SourceAccountStore) Identifiers(ctx context.Context, value, after string, limit int) ([]*models.AccountIdentifier, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	after, limit, err = accountPage(after, limit)
	if err != nil {
		return nil, err
	}
	var rows []accountIdentifierRow
	if err := dbWrapper.Select(ctx, &rows, "SELECT * FROM source_account_identifiers WHERE account_uuid = ? AND uuid > ? ORDER BY uuid LIMIT ?", id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]*models.AccountIdentifier, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, row.resolve())
	}
	return ret, nil
}

func (s *SourceAccountStore) Evidence(ctx context.Context, value, after string, limit int) ([]models.AccountIdentifierEvidence, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	_, limit, err = accountPage("", limit)
	if err != nil {
		return nil, err
	}
	if !validAccountText(after, 512, true) {
		return nil, errors.New("invalid account evidence cursor")
	}
	var rows []struct {
		Key           string    `db:"evidence_key"`
		Basis         string    `db:"basis"`
		Origin        string    `db:"origin"`
		Details       string    `db:"details"`
		FirstObserved Timestamp `db:"first_observed"`
		LastObserved  Timestamp `db:"last_observed"`
	}
	if err := dbWrapper.Select(ctx, &rows, `SELECT evidence_key, basis, origin, details, first_observed, last_observed
FROM source_account_identifier_evidence WHERE identifier_uuid = ? AND evidence_key > ? ORDER BY evidence_key LIMIT ?`, id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.AccountIdentifierEvidence, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, models.AccountIdentifierEvidence{Key: row.Key, Basis: row.Basis, Origin: row.Origin,
			Details: json.RawMessage(row.Details), FirstObserved: row.FirstObserved.Timestamp, LastObserved: row.LastObserved.Timestamp})
	}
	return ret, nil
}

type accountOwnershipRow struct {
	UUID          string                       `db:"uuid"`
	AccountUUID   string                       `db:"account_uuid"`
	Revision      int                          `db:"revision"`
	State         models.AccountOwnershipState `db:"state"`
	PerformerUUID sql.NullString               `db:"performer_uuid"`
	Origin        string                       `db:"origin"`
	Reason        string                       `db:"reason"`
	CreatedAt     Timestamp                    `db:"created_at"`
}

func (r accountOwnershipRow) resolve() *models.AccountOwnershipDecision {
	ret := &models.AccountOwnershipDecision{UUID: r.UUID, AccountUUID: r.AccountUUID, Revision: r.Revision, State: r.State, Origin: r.Origin, Reason: r.Reason, CreatedAt: r.CreatedAt.Timestamp}
	if r.PerformerUUID.Valid {
		ret.PerformerUUID = &r.PerformerUUID.String
	}
	return ret
}

func (s *SourceAccountStore) Ownership(ctx context.Context, value string) (*models.AccountOwnershipDecision, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	var row accountOwnershipRow
	if err := dbWrapper.Get(ctx, &row, `SELECT d.* FROM account_performer_links l
JOIN account_performer_decisions d ON d.account_uuid = l.account_uuid AND d.uuid = l.decision_uuid
WHERE l.account_uuid = ?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.resolve(), nil
}

func (s *SourceAccountStore) OwnershipHistory(ctx context.Context, value string, afterRevision, limit int) ([]*models.AccountOwnershipDecision, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	_, limit, err = accountPage("", limit)
	if err != nil {
		return nil, err
	}
	if afterRevision < 0 {
		return nil, errors.New("invalid ownership history cursor")
	}
	var rows []accountOwnershipRow
	if err := dbWrapper.Select(ctx, &rows, `SELECT * FROM account_performer_decisions
WHERE account_uuid = ? AND revision > ? ORDER BY revision LIMIT ?`, id, afterRevision, limit); err != nil {
		return nil, err
	}
	ret := make([]*models.AccountOwnershipDecision, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, row.resolve())
	}
	return ret, nil
}

func (s *SourceAccountStore) DecideOwnership(ctx context.Context, input models.AccountOwnershipInput) (*models.AccountOwnershipDecision, error) {
	account, err := s.Find(ctx, input.AccountUUID)
	if err != nil {
		return nil, err
	}
	if account == nil || account.Revision != input.ExpectedAccountRevision {
		return nil, models.ErrSourceAccountConflict
	}
	var performerUUID *string
	switch input.State {
	case models.AccountOwnershipLinked:
		// A performer changed or merged after preview needs a new review. Do not
		// silently follow a redirect while applying a stale decision.
		performer, err := (&ArchiveEntityStore{}).Find(ctx, input.PerformerUUID)
		if err != nil {
			return nil, err
		}
		if performer == nil || performer.Kind != models.ArchivePerformer || performer.State != models.ArchiveEntityActive || performer.Revision != input.ExpectedPerformerRevision {
			return nil, models.ErrSourceAccountConflict
		}
		performerUUID = &performer.UUID
	case models.AccountOwnershipUnlinked, models.AccountOwnershipUndecided:
		if input.PerformerUUID != "" || input.ExpectedPerformerRevision != 0 {
			return nil, errors.New("an unlinked or undecided account cannot select a performer")
		}
	default:
		return nil, errors.New("invalid account ownership choice")
	}
	if (input.Origin != "review" && input.Origin != "profile-url" && input.Origin != "migration") || !validAccountText(input.Reason, 4096, true) {
		return nil, errors.New("invalid account ownership origin or reason")
	}
	if input.Origin == "profile-url" {
		current, err := s.Ownership(ctx, account.UUID)
		if err != nil {
			return nil, err
		}
		if input.State != models.AccountOwnershipLinked || (current != nil && current.State != models.AccountOwnershipUndecided) {
			return nil, models.ErrSourceAccountConflict
		}
	}
	result, err := dbWrapper.Exec(ctx, `UPDATE source_accounts SET revision = revision + 1 WHERE uuid = ? AND revision = ?`, account.UUID, account.Revision)
	if err != nil {
		return nil, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, models.ErrSourceAccountConflict
	}
	id := uuid.NewString()
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO account_performer_decisions(uuid, account_uuid, revision, state, performer_uuid, origin, reason)
VALUES (?, ?, ?, ?, ?, ?, ?)`, id, account.UUID, account.Revision+1, input.State, performerUUID, input.Origin, input.Reason); err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO account_performer_links(account_uuid, decision_uuid) VALUES (?, ?)
ON CONFLICT(account_uuid) DO UPDATE SET decision_uuid = excluded.decision_uuid`, account.UUID, id); err != nil {
		return nil, err
	}
	return s.Ownership(ctx, account.UUID)
}
