package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/stashapp/stash/pkg/models"
)

type collectionCaptureRow struct {
	CaptureUUID        string    `db:"capture_uuid"`
	CollectionUUID     string    `db:"collection_uuid"`
	CollectionRevision int       `db:"collection_revision"`
	CreatedAt          Timestamp `db:"created_at"`
}

func (r collectionCaptureRow) resolve() models.CollectionCapture {
	return models.CollectionCapture{CaptureUUID: r.CaptureUUID, CollectionUUID: r.CollectionUUID,
		CollectionRevision: r.CollectionRevision, CreatedAt: r.CreatedAt.Timestamp}
}

// RecordCapture retains historical scope, including retired collections. It is
// not authorization to start work under that definition. CreatedAt is output.
func (s *SourceCollectionStore) RecordCapture(ctx context.Context, input models.CollectionCapture) error {
	collection, err := archiveUUID(input.CollectionUUID)
	if err != nil {
		return err
	}
	capture, err := archiveUUID(input.CaptureUUID)
	if err != nil {
		return err
	}
	if input.CollectionRevision <= 0 {
		return errors.New("collection capture requires a revision")
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO source_collection_captures(collection_uuid,collection_revision,capture_uuid)
VALUES(?,?,?) ON CONFLICT(collection_uuid,capture_uuid,collection_revision) DO NOTHING`, collection, input.CollectionRevision, capture)
	return err
}

func (s *SourceCollectionStore) HasCapture(ctx context.Context, input models.CollectionCapture) (bool, error) {
	for _, value := range []*string{&input.CollectionUUID, &input.CaptureUUID} {
		id, err := archiveUUID(*value)
		if err != nil {
			return false, err
		}
		*value = id
	}
	if input.CollectionRevision <= 0 {
		return false, errors.New("collection capture requires a revision")
	}
	var found bool
	err := dbWrapper.Get(ctx, &found, `SELECT EXISTS(SELECT 1 FROM source_collection_captures
WHERE collection_uuid=? AND capture_uuid=? AND collection_revision=?)`, input.CollectionUUID, input.CaptureUUID, input.CollectionRevision)
	return found, err
}

func (s *SourceCollectionStore) CaptureProvenance(ctx context.Context, input models.CollectionCapture) (*models.CollectionCapture, error) {
	for _, value := range []*string{&input.CollectionUUID, &input.CaptureUUID} {
		id, err := archiveUUID(*value)
		if err != nil {
			return nil, err
		}
		*value = id
	}
	if input.CollectionRevision <= 0 {
		return nil, errors.New("collection capture requires a revision")
	}
	var row collectionCaptureRow
	err := dbWrapper.Get(ctx, &row, `SELECT * FROM source_collection_captures
WHERE collection_uuid=? AND capture_uuid=? AND collection_revision<=?
ORDER BY collection_revision DESC LIMIT 1`, input.CollectionUUID, input.CaptureUUID, input.CollectionRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ret := row.resolve()
	return &ret, nil
}

func (s *SourceCollectionStore) Captures(ctx context.Context, value string, after *models.CollectionCaptureCursor, limit int) ([]models.CollectionCapture, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	capture, revision := "", 0
	if after != nil {
		capture, err = archiveUUID(after.CaptureUUID)
		if err != nil {
			return nil, err
		}
		revision = after.CollectionRevision
		if revision <= 0 {
			return nil, errors.New("invalid collection capture cursor revision")
		}
	}
	limit, err = sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	var rows []collectionCaptureRow
	if err := dbWrapper.Select(ctx, &rows, `SELECT * FROM source_collection_captures
WHERE collection_uuid=? AND (capture_uuid,collection_revision)>(?,?)
ORDER BY capture_uuid,collection_revision LIMIT ?`, id, capture, revision, limit); err != nil {
		return nil, err
	}
	ret := make([]models.CollectionCapture, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, row.resolve())
	}
	return ret, nil
}

type collectionMediaIntakeRow struct {
	UUID               string    `db:"uuid"`
	CollectionUUID     string    `db:"collection_uuid"`
	CollectionRevision int       `db:"collection_revision"`
	MediaUUID          string    `db:"media_uuid"`
	SubmittedMediaUUID string    `db:"submitted_media_uuid"`
	Origin             string    `db:"origin"`
	Reason             string    `db:"reason"`
	CreatedAt          Timestamp `db:"created_at"`
}

func (r collectionMediaIntakeRow) resolve() *models.CollectionMediaIntake {
	return &models.CollectionMediaIntake{UUID: r.UUID, CollectionUUID: r.CollectionUUID, CollectionRevision: r.CollectionRevision,
		MediaUUID: r.MediaUUID, SubmittedMediaUUID: r.SubmittedMediaUUID, Origin: r.Origin, Reason: r.Reason, CreatedAt: r.CreatedAt.Timestamp}
}

func collectionMediaIntake(ctx context.Context, id string) (*models.CollectionMediaIntake, error) {
	var row collectionMediaIntakeRow
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM source_collection_media_intake WHERE uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.resolve(), nil
}

// RecordMediaIntake is an immutable, replayable provenance fact. CreatedAt is
// assigned here. An imported source timestamp belongs in its capture evidence.
func (s *SourceCollectionStore) RecordMediaIntake(ctx context.Context, input models.CollectionMediaIntake) (*models.CollectionMediaIntake, error) {
	for _, value := range []*string{&input.UUID, &input.CollectionUUID, &input.MediaUUID} {
		id, err := archiveUUID(*value)
		if err != nil {
			return nil, err
		}
		*value = id
	}
	if input.CollectionRevision <= 0 || !validAccountText(input.Reason, 4096, true) ||
		(input.Origin != "scan" && input.Origin != "ingest" && input.Origin != "review" && input.Origin != "migration") {
		return nil, errors.New("invalid collection media intake")
	}
	current, err := collectionMediaIntake(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if current != nil {
		if current.CollectionUUID != input.CollectionUUID || current.CollectionRevision != input.CollectionRevision ||
			(current.MediaUUID != input.MediaUUID && current.SubmittedMediaUUID != input.MediaUUID) || current.Origin != input.Origin || current.Reason != input.Reason {
			return nil, models.ErrCollectionIntakeReplay
		}
		return current, nil
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_collection_media_intake(uuid,collection_uuid,collection_revision,media_uuid,submitted_media_uuid,origin,reason)
VALUES(?,?,?,?,?,?,?)`, input.UUID, input.CollectionUUID, input.CollectionRevision, input.MediaUUID, input.MediaUUID, input.Origin, input.Reason); err != nil {
		return nil, err
	}
	return collectionMediaIntake(ctx, input.UUID)
}

func (s *SourceCollectionStore) MediaIntake(ctx context.Context, value, after string, limit int) ([]models.CollectionMediaIntake, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	after, limit, err = sourceDefinitionPage(after, limit)
	if err != nil {
		return nil, err
	}
	var rows []collectionMediaIntakeRow
	if err := dbWrapper.Select(ctx, &rows, `SELECT * FROM source_collection_media_intake
WHERE collection_uuid=? AND uuid>? ORDER BY uuid LIMIT ?`, id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.CollectionMediaIntake, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, *row.resolve())
	}
	return ret, nil
}
