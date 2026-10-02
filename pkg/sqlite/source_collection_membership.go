package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"reflect"

	"github.com/stashapp/stash/pkg/models"
)

type collectionPostMembershipRow struct {
	postLinkRow
	CollectionUUID     string `db:"collection_uuid"`
	CollectionRevision int    `db:"collection_revision"`
}

func (r collectionPostMembershipRow) resolve() *models.CollectionPostMembership {
	return &models.CollectionPostMembership{SourcePostEvidence: r.evidence(), CollectionUUID: r.CollectionUUID, CollectionRevision: r.CollectionRevision}
}

func findCollectionPostMembership(ctx context.Context, id string) (*collectionPostMembershipRow, error) {
	var row collectionPostMembershipRow
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM source_collection_post_evidence WHERE uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func (s *SourceCollectionStore) RecordPostMembership(ctx context.Context, input models.CollectionPostMembership) (*models.CollectionPostMembership, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if err := normalizePostLink(&input.SourcePostEvidence); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(input.CollectionUUID) || input.CollectionRevision < 1 {
		return nil, models.ErrSourcePostEvidenceInvalid
	}
	prior, err := findCollectionPostMembership(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if !reflect.DeepEqual(input, *prior.resolve()) {
			return nil, models.ErrSourcePostEvidenceReplay
		}
		return prior.resolve(), nil
	}
	if _, err := activePostLink(ctx, input.PostUUID); err != nil {
		return nil, err
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO source_collection_post_evidence(uuid,post_uuid,collection_uuid,collection_revision,origin,basis,observed_at,details)
 VALUES(?,?,?,?,?,?,?,?)`, input.UUID, input.PostUUID, input.CollectionUUID, input.CollectionRevision, input.Origin, input.Basis, input.ObservedAt, string(input.Details))
	if err != nil {
		return nil, err
	}
	return &input, nil
}

func collectionPostMemberships(ctx context.Context, column, id, after string, limit int) ([]models.CollectionPostMembership, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrSourcePostEvidenceInvalid
	}
	after, limit, err := sourceDefinitionPage(after, limit)
	if err != nil {
		return nil, err
	}
	var rows []collectionPostMembershipRow
	if err := dbWrapper.Select(ctx, &rows, "SELECT * FROM source_collection_post_evidence WHERE "+column+"=? AND uuid>? ORDER BY uuid LIMIT ?", id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.CollectionPostMembership, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, *row.resolve())
	}
	return ret, nil
}

func (s *SourceCollectionStore) Memberships(ctx context.Context, collection, after string, limit int) ([]models.CollectionPostMembership, error) {
	return collectionPostMemberships(ctx, "collection_uuid", collection, after, limit)
}

func (s *SourceCollectionStore) PostMemberships(ctx context.Context, post, after string, limit int) ([]models.CollectionPostMembership, error) {
	return collectionPostMemberships(ctx, "post_uuid", post, after, limit)
}
