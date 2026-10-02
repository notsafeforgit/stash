package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type SourcePostLinksStore struct{}

type postLinkRow struct {
	UUID                 string    `db:"uuid"`
	PostUUID             string    `db:"post_uuid"`
	Origin               string    `db:"origin"`
	Basis                string    `db:"basis"`
	ObservedAt           Timestamp `db:"observed_at"`
	Details              string    `db:"details"`
	Digest               string    `db:"request_digest"`
	URLUUID              string    `db:"url_uuid"`
	URL                  string    `db:"url"`
	Namespace            string    `db:"namespace"`
	Value                string    `db:"value"`
	AccountUUID          string    `db:"account_uuid"`
	CanonicalAccountUUID string    `db:"canonical_account_uuid"`
}

func (r postLinkRow) evidence() models.SourcePostEvidence {
	return models.SourcePostEvidence{UUID: r.UUID, PostUUID: r.PostUUID, Origin: r.Origin, Basis: r.Basis, ObservedAt: r.ObservedAt.Timestamp, Details: json.RawMessage(r.Details)}
}
func (r postLinkRow) url() *models.SourcePostURLObservation {
	return &models.SourcePostURLObservation{SourcePostEvidence: r.evidence(), URLUUID: r.URLUUID, URL: r.URL}
}
func (r postLinkRow) identifier() *models.SourcePostIdentifierObservation {
	return &models.SourcePostIdentifierObservation{SourcePostEvidence: r.evidence(), Identifier: models.SourcePostIdentifier{Namespace: r.Namespace, Value: r.Value}}
}
func (r postLinkRow) account() *models.SourcePostAccountClaim {
	return &models.SourcePostAccountClaim{SourcePostAccountClaimInput: models.SourcePostAccountClaimInput{SourcePostEvidence: r.evidence(), AccountUUID: r.AccountUUID}, CanonicalAccountUUID: r.CanonicalAccountUUID}
}

func normalizePostLink(e *models.SourcePostEvidence) error {
	for _, v := range []*string{&e.UUID, &e.PostUUID} {
		if id, err := archiveUUID(*v); err != nil || id != *v {
			return models.ErrSourcePostEvidenceInvalid
		}
	}
	if (e.Origin != "capture" && e.Origin != "migration" && e.Origin != "review") || !validAccountText(e.Basis, 128, false) || strings.TrimSpace(e.Basis) != e.Basis ||
		e.ObservedAt.IsZero() || e.ObservedAt.UTC().Year() < 1 || e.ObservedAt.UTC().Year() > 9999 {
		return models.ErrSourcePostEvidenceInvalid
	}
	e.ObservedAt = e.ObservedAt.UTC()
	if len(e.Details) == 0 {
		e.Details = json.RawMessage(`{}`)
	}
	data, err := archive.DecodeJSONObject(e.Details, 65536)
	if err != nil {
		return models.ErrSourcePostEvidenceInvalid
	}
	e.Details, err = archive.EncodeSourceJSON(data)
	if err != nil || len(e.Details) > 65536 {
		return models.ErrSourcePostEvidenceInvalid
	}
	return nil
}

func activePostLink(ctx context.Context, id string) (*models.SourcePost, error) {
	post, err := (&SourceEvidenceStore{}).FindPost(ctx, id)
	if err != nil {
		return nil, err
	}
	if post == nil {
		return nil, models.ErrSourcePostConflict
	}
	if post.State != "active" {
		return nil, models.ErrSourcePostForgotten
	}
	return post, nil
}

func postLinkWrite(ctx context.Context) (*bool, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return errors.New("source post evidence write did not finish atomically")
		}
		return nil
	})
	return &complete, nil
}

const postURLObservationSelect = `SELECT e.*,u.post_uuid,u.url FROM source_post_url_evidence e JOIN source_post_urls u ON u.uuid=e.url_uuid`
const postAccountClaimSelect = `SELECT e.*,a.canonical_uuid AS canonical_account_uuid FROM source_post_account_claims e JOIN source_accounts a ON a.uuid=e.account_uuid`

func postLinkPrevious(ctx context.Context, query, id, digest string) (*postLinkRow, error) {
	var row postLinkRow
	if err := dbWrapper.Get(ctx, &row, query+" WHERE e.uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if row.Digest != digest {
		return nil, models.ErrSourcePostEvidenceReplay
	}
	return &row, nil
}

func (s *SourcePostLinksStore) ObserveURL(ctx context.Context, input models.SourcePostURLInput) (*models.SourcePostURLObservation, error) {
	if err := normalizePostLink(&input.SourcePostEvidence); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(input.URL)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") ||
		!validAccountText(input.URL, 8192, false) || strings.TrimSpace(input.URL) != input.URL {
		return nil, models.ErrSourcePostEvidenceInvalid
	}
	digest, err := sourceSignature("stash-post-url-evidence-v1", input)
	if err != nil {
		return nil, err
	}
	prior, err := postLinkPrevious(ctx, postURLObservationSelect, input.UUID, digest)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		return prior.url(), nil
	}
	if _, err := activePostLink(ctx, input.PostUUID); err != nil {
		return nil, err
	}
	complete, err := postLinkWrite(ctx)
	if err != nil {
		return nil, err
	}
	urlUUID := uuid.NewSHA1(uuid.MustParse(input.PostUUID), []byte("source-post-url\x00"+input.URL)).String()
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_post_urls(uuid,post_uuid,url) VALUES(?,?,?) ON CONFLICT(post_uuid,url) DO NOTHING`, urlUUID, input.PostUUID, input.URL); err != nil {
		return nil, err
	}
	if err := dbWrapper.Get(ctx, &urlUUID, `SELECT uuid FROM source_post_urls WHERE post_uuid=? AND url=?`, input.PostUUID, input.URL); err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_post_url_evidence(uuid,url_uuid,origin,basis,observed_at,details,request_digest) VALUES(?,?,?,?,?,?,?)`, input.UUID, urlUUID, input.Origin, input.Basis, input.ObservedAt.Format(accountObservationTimeFormat), string(input.Details), digest); err != nil {
		return nil, err
	}
	row, err := postLinkPrevious(ctx, postURLObservationSelect, input.UUID, digest)
	if err != nil {
		return nil, err
	}
	*complete = true
	return row.url(), nil
}

func (s *SourcePostLinksStore) ObserveIdentifier(ctx context.Context, input models.SourcePostIdentifierInput) (*models.SourcePostIdentifierObservation, error) {
	if err := normalizePostLink(&input.SourcePostEvidence); err != nil {
		return nil, err
	}
	if err := validatePostIdentifier(input.Identifier); err != nil || input.ExpectedPostRevision < 1 {
		return nil, models.ErrSourcePostEvidenceInvalid
	}
	digest, err := sourceSignature("stash-post-identifier-evidence-v1", input)
	if err != nil {
		return nil, err
	}
	prior, err := postLinkPrevious(ctx, "SELECT e.* FROM source_post_identifier_evidence e", input.UUID, digest)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		return prior.identifier(), nil
	}
	post, err := activePostLink(ctx, input.PostUUID)
	if err != nil {
		return nil, err
	}
	store := &SourceEvidenceStore{}
	existing, err := store.FindPostByIdentifier(ctx, input.Identifier)
	if err != nil {
		return nil, err
	}
	if post.Revision != input.ExpectedPostRevision || (existing != nil && existing.UUID != post.UUID) {
		return nil, models.ErrSourcePostConflict
	}
	complete, err := postLinkWrite(ctx)
	if err != nil {
		return nil, err
	}
	if err := store.AddPostIdentifier(ctx, input.PostUUID, input.Identifier, input.ExpectedPostRevision); err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_post_identifier_evidence(uuid,post_uuid,namespace,value,origin,basis,observed_at,details,request_digest) VALUES(?,?,?,?,?,?,?,?,?)`, input.UUID, input.PostUUID, input.Identifier.Namespace, input.Identifier.Value, input.Origin, input.Basis, input.ObservedAt.Format(accountObservationTimeFormat), string(input.Details), digest); err != nil {
		return nil, err
	}
	row, err := postLinkPrevious(ctx, "SELECT e.* FROM source_post_identifier_evidence e", input.UUID, digest)
	if err != nil {
		return nil, err
	}
	*complete = true
	return row.identifier(), nil
}

func (s *SourcePostLinksStore) ClaimAccount(ctx context.Context, input models.SourcePostAccountClaimInput) (*models.SourcePostAccountClaim, error) {
	if err := normalizePostLink(&input.SourcePostEvidence); err != nil {
		return nil, err
	}
	if id, err := archiveUUID(input.AccountUUID); err != nil || id != input.AccountUUID {
		return nil, models.ErrSourcePostEvidenceInvalid
	}
	digest, err := sourceSignature("stash-post-account-claim-v1", input)
	if err != nil {
		return nil, err
	}
	prior, err := postLinkPrevious(ctx, postAccountClaimSelect, input.UUID, digest)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		return prior.account(), nil
	}
	if _, err := activePostLink(ctx, input.PostUUID); err != nil {
		return nil, err
	}
	account, err := (&SourceAccountStore{}).Resolve(ctx, input.AccountUUID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, models.ErrSourceAccountConflict
	}
	allowed, err := publisherNamespaceAllowed(ctx, input.PostUUID, account.Namespace)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, models.ErrSourcePostConflict
	}
	complete, err := postLinkWrite(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_post_account_claims(uuid,post_uuid,account_uuid,origin,basis,observed_at,details,request_digest) VALUES(?,?,?,?,?,?,?,?)`, input.UUID, input.PostUUID, input.AccountUUID, input.Origin, input.Basis, input.ObservedAt.Format(accountObservationTimeFormat), string(input.Details), digest); err != nil {
		return nil, err
	}
	row, err := postLinkPrevious(ctx, postAccountClaimSelect, input.UUID, digest)
	if err != nil {
		return nil, err
	}
	*complete = true
	return row.account(), nil
}

func postLinkPage(value, after string, limit int) (string, string, int, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return "", "", 0, err
	}
	after, limit, err = sourceDefinitionPage(after, limit)
	return id, after, limit, err
}

func (s *SourcePostLinksStore) URLs(ctx context.Context, post, after string, limit int) ([]models.SourcePostURL, error) {
	post, after, limit, err := postLinkPage(post, after, limit)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		UUID     string `db:"uuid"`
		PostUUID string `db:"post_uuid"`
		URL      string `db:"url"`
	}
	if err := dbWrapper.Select(ctx, &rows, "SELECT * FROM source_post_urls WHERE post_uuid=? AND uuid>? ORDER BY uuid LIMIT ?", post, after, limit); err != nil {
		return nil, err
	}
	result := make([]models.SourcePostURL, 0, len(rows))
	for _, r := range rows {
		result = append(result, models.SourcePostURL{UUID: r.UUID, PostUUID: r.PostUUID, URL: r.URL})
	}
	return result, nil
}

func (s *SourcePostLinksStore) URLEvidence(ctx context.Context, urlUUID, after string, limit int) ([]models.SourcePostURLObservation, error) {
	id, after, limit, err := postLinkPage(urlUUID, after, limit)
	if err != nil {
		return nil, err
	}
	var rows []postLinkRow
	if err := dbWrapper.Select(ctx, &rows, postURLObservationSelect+" WHERE e.url_uuid=? AND e.uuid>? ORDER BY e.uuid LIMIT ?", id, after, limit); err != nil {
		return nil, err
	}
	result := make([]models.SourcePostURLObservation, 0, len(rows))
	for _, row := range rows {
		result = append(result, *row.url())
	}
	return result, nil
}

func (s *SourcePostLinksStore) IdentifierEvidence(ctx context.Context, post, after string, limit int) ([]models.SourcePostIdentifierObservation, error) {
	id, after, limit, err := postLinkPage(post, after, limit)
	if err != nil {
		return nil, err
	}
	var rows []postLinkRow
	if err := dbWrapper.Select(ctx, &rows, "SELECT e.* FROM source_post_identifier_evidence e WHERE post_uuid=? AND uuid>? ORDER BY uuid LIMIT ?", id, after, limit); err != nil {
		return nil, err
	}
	result := make([]models.SourcePostIdentifierObservation, 0, len(rows))
	for _, row := range rows {
		result = append(result, *row.identifier())
	}
	return result, nil
}

func (s *SourcePostLinksStore) AccountClaims(ctx context.Context, post, after string, limit int) ([]models.SourcePostAccountClaim, error) {
	id, after, limit, err := postLinkPage(post, after, limit)
	if err != nil {
		return nil, err
	}
	var rows []postLinkRow
	if err := dbWrapper.Select(ctx, &rows, postAccountClaimSelect+" WHERE e.post_uuid=? AND e.uuid>? ORDER BY e.uuid LIMIT ?", id, after, limit); err != nil {
		return nil, err
	}
	result := make([]models.SourcePostAccountClaim, 0, len(rows))
	for _, row := range rows {
		result = append(result, *row.account())
	}
	return result, nil
}
