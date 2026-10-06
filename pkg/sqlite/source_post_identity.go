package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

const maxPostIdentityMembers = 256
const maxPostIdentityIdentifiers = 8192

type sourcePostIdentityRow struct {
	sourcePostRow
	CanonicalUUID string         `db:"canonical_uuid"`
	RedirectTo    sql.NullString `db:"redirect_to"`
}

func (r sourcePostIdentityRow) identity() models.SourcePostIdentity {
	ret := models.SourcePostIdentity{UUID: r.UUID, CanonicalUUID: r.CanonicalUUID, State: r.State, Revision: r.Revision, CreatedAt: r.CreatedAt.Timestamp}
	if r.RedirectTo.Valid {
		ret.RedirectTo = &r.RedirectTo.String
	}
	return ret
}

const postIdentitySelect = `SELECT p.*,i.canonical_uuid,c.destination_uuid AS redirect_to FROM source_posts p
JOIN source_post_identities i ON i.post_uuid=p.uuid
LEFT JOIN source_post_consolidations c ON c.source_uuid=p.uuid`

const postIdentityMembersQuery = postIdentitySelect + " WHERE i.canonical_uuid=? AND i.post_uuid>? ORDER BY i.post_uuid LIMIT ?"

const postIdentityIdentifiersQuery = `SELECT i.post_uuid,i.namespace,i.value FROM source_post_identities p
JOIN source_post_identifiers i ON i.post_uuid=p.post_uuid
WHERE p.canonical_uuid IN (?,?) LIMIT ?`

const postIdentityHistoryQuery = `SELECT * FROM (
SELECT * FROM (SELECT * FROM source_post_consolidations WHERE source_uuid=? AND sequence>? ORDER BY sequence LIMIT ?)
UNION ALL
SELECT * FROM (SELECT * FROM source_post_consolidations WHERE destination_uuid=? AND sequence>? ORDER BY sequence LIMIT ?)
) ORDER BY sequence LIMIT ?`

func (s *SourceEvidenceStore) PostIdentity(ctx context.Context, id string) (*models.SourcePostIdentity, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrSourcePostIdentityInvalid
	}
	var row sourcePostIdentityRow
	if err := dbWrapper.Get(ctx, &row, postIdentitySelect+" WHERE p.uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	ret := row.identity()
	return &ret, nil
}

func postIdentityMembers(ctx context.Context, root, after string, limit int) ([]models.SourcePostIdentity, error) {
	var rows []sourcePostIdentityRow
	if err := dbWrapper.Select(ctx, &rows, postIdentityMembersQuery, root, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.SourcePostIdentity, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, row.identity())
	}
	return ret, nil
}

func (s *SourceEvidenceStore) PostIdentityMembers(ctx context.Context, id, after string, limit int) ([]models.SourcePostIdentity, error) {
	if limit < 1 || limit > 100 || (after != "" && !validSourceRunUUID(after)) {
		return nil, models.ErrSourcePostIdentityInvalid
	}
	identity, err := s.PostIdentity(ctx, id)
	if err != nil {
		return nil, err
	}
	if identity == nil {
		return []models.SourcePostIdentity{}, nil
	}
	return postIdentityMembers(ctx, identity.CanonicalUUID, after, limit)
}

type postIdentityReference struct {
	PostUUID  string `db:"post_uuid"`
	Namespace string `db:"namespace"`
	Value     string `db:"value"`
}

// This is an identity-only guard used inside a larger reviewed transaction.
// It is not an application merge preview: selected albums, attachment/media
// choices and pending publication scope need their own review guards.
type postIdentitySnapshot struct {
	Source      models.SourcePostIdentity
	Destination models.SourcePostIdentity
	Members     []models.SourcePostIdentity
	Identifiers []postIdentityReference
	Signature   string
}

func inspectPostConsolidationIdentity(ctx context.Context, source, destination string) (*postIdentitySnapshot, error) {
	if source == destination {
		return nil, models.ErrSourcePostIdentityInvalid
	}
	s := &SourceEvidenceStore{}
	from, err := s.PostIdentity(ctx, source)
	if err != nil {
		return nil, err
	}
	to, err := s.PostIdentity(ctx, destination)
	if err != nil {
		return nil, err
	}
	if from == nil || to == nil || from.CanonicalUUID != source || to.CanonicalUUID != destination {
		return nil, models.ErrSourcePostIdentityConflict
	}
	ret := &postIdentitySnapshot{Source: *from, Destination: *to, Members: []models.SourcePostIdentity{}, Identifiers: []postIdentityReference{}}
	for _, root := range []string{source, destination} {
		members, err := postIdentityMembers(ctx, root, "", maxPostIdentityMembers+1)
		if err != nil {
			return nil, err
		}
		ret.Members = append(ret.Members, members...)
		if len(ret.Members) > maxPostIdentityMembers {
			return nil, models.ErrSourcePostIdentityLimit
		}
	}
	slices.SortFunc(ret.Members, func(a, b models.SourcePostIdentity) int { return strings.Compare(a.UUID, b.UUID) })
	for _, member := range ret.Members {
		if member.State != "active" {
			return nil, models.ErrSourcePostForgotten
		}
	}
	if err := dbWrapper.Select(ctx, &ret.Identifiers, postIdentityIdentifiersQuery, source, destination, maxPostIdentityIdentifiers+1); err != nil {
		return nil, err
	}
	if len(ret.Identifiers) > maxPostIdentityIdentifiers {
		return nil, models.ErrSourcePostIdentityLimit
	}
	slices.SortFunc(ret.Identifiers, func(a, b postIdentityReference) int {
		if result := strings.Compare(a.Namespace, b.Namespace); result != 0 {
			return result
		}
		if result := strings.Compare(a.Value, b.Value); result != 0 {
			return result
		}
		return strings.Compare(a.PostUUID, b.PostUUID)
	})
	var native *postIdentityReference
	for _, ref := range ret.Identifiers {
		if strings.HasPrefix(ref.Namespace, "legacy:") {
			continue
		}
		if native != nil && (native.Namespace != ref.Namespace || native.Value != ref.Value) {
			return nil, models.ErrSourcePostIdentifierConflict
		}
		current := ref
		native = &current
	}
	ret.Signature, err = sourceSignature("stash-post-identity-snapshot-v1", ret)
	return ret, err
}

type postIdentityConsolidationInput struct {
	UUID              string
	SourceUUID        string
	DestinationUUID   string
	IdentitySignature string
	ReviewSignature   string
	Origin            string
	Reason            string
}

type postConsolidationRow struct {
	UUID                string    `db:"uuid"`
	Sequence            int       `db:"sequence"`
	SourceUUID          string    `db:"source_uuid"`
	DestinationUUID     string    `db:"destination_uuid"`
	SourceRevision      int       `db:"source_revision"`
	DestinationRevision int       `db:"destination_revision"`
	MemberCount         int       `db:"member_count"`
	IdentitySignature   string    `db:"identity_signature"`
	ReviewSignature     string    `db:"review_signature"`
	RequestDigest       string    `db:"request_digest"`
	Origin              string    `db:"origin"`
	Reason              string    `db:"reason"`
	CreatedAt           Timestamp `db:"created_at"`
}

func (r postConsolidationRow) record() models.SourcePostConsolidation {
	return models.SourcePostConsolidation{UUID: r.UUID, Sequence: r.Sequence, SourceUUID: r.SourceUUID, DestinationUUID: r.DestinationUUID,
		SourceRevision: r.SourceRevision, DestinationRevision: r.DestinationRevision, MemberCount: r.MemberCount, IdentitySignature: r.IdentitySignature,
		ReviewSignature: r.ReviewSignature, Origin: r.Origin, Reason: r.Reason, CreatedAt: r.CreatedAt.Timestamp}
}

func postConsolidationRequest(input postIdentityConsolidationInput) (string, error) {
	if !validSourceRunUUID(input.UUID) || !validSourceRunUUID(input.SourceUUID) || !validSourceRunUUID(input.DestinationUUID) || input.SourceUUID == input.DestinationUUID ||
		!archive.ValidSHA256(input.IdentitySignature) || !archive.ValidSHA256(input.ReviewSignature) || !validAccountText(input.Reason, 4096, true) ||
		(input.Origin != "review" && input.Origin != "migration") {
		return "", models.ErrSourcePostIdentityInvalid
	}
	return sourceSignature("stash-post-consolidation-request-v1", input)
}

func postConsolidationReceipt(ctx context.Context, input postIdentityConsolidationInput) (*models.SourcePostConsolidation, error) {
	digest, err := postConsolidationRequest(input)
	if err != nil {
		return nil, err
	}
	var row postConsolidationRow
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM source_post_consolidations WHERE uuid=?", input.UUID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if row.RequestDigest != digest {
		return nil, models.ErrSourcePostConsolidationReplay
	}
	ret := row.record()
	return &ret, nil
}

// Only the encompassing reviewed consolidation may invoke this primitive.
// It deliberately has no repository-interface or HTTP mutation entry point.
// The caller must settle/guard chosen associations and pending publication in
// the same transaction before making this identity change reachable by users.
func publishPostConsolidationIdentity(ctx context.Context, input postIdentityConsolidationInput) (*models.SourcePostConsolidation, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	prior, err := postConsolidationReceipt(ctx, input)
	if err != nil || prior != nil {
		return prior, err
	}
	preview, err := inspectPostConsolidationIdentity(ctx, input.SourceUUID, input.DestinationUUID)
	if err != nil {
		return nil, err
	}
	if preview.Signature != input.IdentitySignature {
		return nil, models.ErrSourcePostIdentityConflict
	}
	digest, err := postConsolidationRequest(input)
	if err != nil {
		return nil, err
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		var unfinished bool
		if err := dbWrapper.Get(ctx, &unfinished, "SELECT EXISTS(SELECT 1 FROM source_post_consolidation_context)"); err != nil {
			return err
		}
		if unfinished {
			return errors.New("unfinished source post consolidation")
		}
		return nil
	})
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO source_post_consolidation_context(request_uuid,source_uuid,destination_uuid) VALUES(?,?,?)", input.UUID, input.SourceUUID, input.DestinationUUID); err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_post_consolidations(uuid,source_uuid,destination_uuid,source_revision,destination_revision,
member_count,identity_signature,review_signature,request_digest,origin,reason) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, input.UUID, input.SourceUUID, input.DestinationUUID,
		preview.Source.Revision, preview.Destination.Revision, len(preview.Members), preview.Signature, input.ReviewSignature, digest, input.Origin, input.Reason); err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, "DELETE FROM source_post_consolidation_context WHERE request_uuid=?", input.UUID); err != nil {
		return nil, err
	}
	return postConsolidationReceipt(ctx, input)
}

func (s *SourceEvidenceStore) PostConsolidationHistory(ctx context.Context, id string, after, limit int) ([]models.SourcePostConsolidation, error) {
	if !validSourceRunUUID(id) || after < 0 || limit < 1 || limit > 100 {
		return nil, models.ErrSourcePostIdentityInvalid
	}
	var rows []postConsolidationRow
	if err := dbWrapper.Select(ctx, &rows, postIdentityHistoryQuery, id, after, limit, id, after, limit, limit); err != nil {
		return nil, err
	}
	ret := make([]models.SourcePostConsolidation, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, row.record())
	}
	return ret, nil
}
