package sqlite

import (
	"context"
	"database/sql"
	encodinghex "encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type IngestStore struct{}

type ingestProducerRow struct {
	UUID      string    `db:"uuid"`
	Label     string    `db:"label"`
	CreatedAt Timestamp `db:"created_at"`
}

func (r ingestProducerRow) resolve() *models.IngestProducer {
	return &models.IngestProducer{UUID: r.UUID, Label: r.Label, CreatedAt: r.CreatedAt.Timestamp}
}

func (s *IngestStore) CreateProducer(ctx context.Context, label string) (*models.IngestProducer, error) {
	if !validAccountText(label, 256, false) || strings.TrimSpace(label) != label {
		return nil, errors.New("invalid ingestion producer label")
	}
	id := uuid.NewString()
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO ingest_producers(uuid,label) VALUES(?,?)", id, label); err != nil {
		return nil, err
	}
	return s.FindProducer(ctx, id)
}

func (s *IngestStore) FindProducer(ctx context.Context, id string) (*models.IngestProducer, error) {
	if _, err := archiveUUID(id); err != nil {
		return nil, err
	}
	var row ingestProducerRow
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM ingest_producers WHERE uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.resolve(), nil
}

func ingestPage(after string, limit int) (int, error) {
	if after != "" {
		if _, err := archiveUUID(after); err != nil {
			return 0, err
		}
	}
	return sourcePageLimit(limit)
}

func (s *IngestStore) Producers(ctx context.Context, after string, limit int) ([]models.IngestProducer, error) {
	limit, err := ingestPage(after, limit)
	if err != nil {
		return nil, err
	}
	var rows []ingestProducerRow
	if err := dbWrapper.Select(ctx, &rows, "SELECT * FROM ingest_producers WHERE uuid>? ORDER BY uuid LIMIT ?", after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.IngestProducer, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, *row.resolve())
	}
	return ret, nil
}

type ingestCredentialRow struct {
	UUID         string     `db:"uuid"`
	ProducerUUID string     `db:"producer_uuid"`
	SecretHash   string     `db:"secret_hash"`
	ExpiresAt    *Timestamp `db:"expires_at"`
	Revoked      bool       `db:"revoked"`
	CreatedAt    Timestamp  `db:"created_at"`
}

type ingestScopeRow struct {
	CollectionUUID string  `db:"collection_uuid"`
	RootUUID       *string `db:"root_uuid"`
}

func (s *IngestStore) FindCredential(ctx context.Context, id string) (*models.IngestCredential, error) {
	if _, err := archiveUUID(id); err != nil {
		return nil, err
	}
	var row ingestCredentialRow
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM ingest_credentials WHERE uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var scopes []ingestScopeRow
	if err := dbWrapper.Select(ctx, &scopes, "SELECT collection_uuid,root_uuid FROM ingest_credential_scopes WHERE credential_uuid=? ORDER BY collection_uuid", id); err != nil {
		return nil, err
	}
	ret := &models.IngestCredential{UUID: row.UUID, ProducerUUID: row.ProducerUUID, SecretHash: row.SecretHash, Revoked: row.Revoked, CreatedAt: row.CreatedAt.Timestamp, Scopes: make([]models.IngestScope, 0, len(scopes))}
	if row.ExpiresAt != nil {
		ret.ExpiresAt = &row.ExpiresAt.Timestamp
	}
	for _, scope := range scopes {
		ret.Scopes = append(ret.Scopes, models.IngestScope{CollectionUUID: scope.CollectionUUID, RootUUID: scope.RootUUID})
	}
	ret.RootUUIDs = make([]string, 0)
	if err := dbWrapper.Select(ctx, &ret.RootUUIDs, "SELECT root_uuid FROM ingest_credential_roots WHERE credential_uuid=? ORDER BY root_uuid", id); err != nil {
		return nil, err
	}
	return ret, nil
}

func validIngestDigest(digest string) bool {
	decoded, err := encodinghex.DecodeString(digest)
	return err == nil && len(decoded) == 32 && strings.ToLower(digest) == digest
}

func (s *IngestStore) IssueCredential(ctx context.Context, input models.IngestCredential) (*models.IngestCredential, error) {
	for _, id := range []string{input.UUID, input.ProducerUUID} {
		if _, err := archiveUUID(id); err != nil {
			return nil, err
		}
	}
	if !validIngestDigest(input.SecretHash) || input.Revoked || len(input.Scopes)+len(input.RootUUIDs) < 1 || len(input.Scopes)+len(input.RootUUIDs) > 128 ||
		(input.ExpiresAt != nil && (!input.ExpiresAt.After(time.Now()) || input.ExpiresAt.Year() > 9999)) {
		return nil, errors.New("invalid ingestion credential")
	}
	seen := make(map[string]bool)
	for _, scope := range input.Scopes {
		collection, err := (&SourceCollectionStore{}).Find(ctx, scope.CollectionUUID)
		if err != nil {
			return nil, err
		}
		if collection == nil || collection.State != "active" || seen[scope.CollectionUUID] {
			return nil, models.ErrSourceDefinitionConflict
		}
		var historicalRoot bool
		if err := dbWrapper.Get(ctx, &historicalRoot, "SELECT EXISTS(SELECT 1 FROM source_collection_revisions WHERE collection_uuid=? AND root_uuid IS ?)", scope.CollectionUUID, scope.RootUUID); err != nil {
			return nil, err
		}
		if !historicalRoot {
			return nil, models.ErrSourceDefinitionConflict
		}
		seen[scope.CollectionUUID] = true
		if scope.RootUUID != nil {
			root, err := (&MediaRootStore{}).Find(ctx, *scope.RootUUID)
			if err != nil {
				return nil, err
			}
			if root == nil || root.State != "active" {
				return nil, models.ErrSourceDefinitionConflict
			}
		}
	}
	seenRoots := make(map[string]bool)
	for _, id := range input.RootUUIDs {
		root, err := (&MediaRootStore{}).Find(ctx, id)
		if err != nil {
			return nil, err
		}
		if root == nil || root.State != "active" || seenRoots[id] {
			return nil, models.ErrSourceDefinitionConflict
		}
		seenRoots[id] = true
	}
	var expires interface{}
	if input.ExpiresAt != nil {
		expires = input.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO ingest_credentials(uuid,producer_uuid,secret_hash,expires_at) VALUES(?,?,?,?)", input.UUID, input.ProducerUUID, input.SecretHash, expires); err != nil {
		return nil, err
	}
	for _, scope := range input.Scopes {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO ingest_credential_scopes(credential_uuid,collection_uuid,root_uuid) VALUES(?,?,?)", input.UUID, scope.CollectionUUID, scope.RootUUID); err != nil {
			return nil, err
		}
	}
	for _, root := range input.RootUUIDs {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO ingest_credential_roots(credential_uuid,root_uuid) VALUES(?,?)", input.UUID, root); err != nil {
			return nil, err
		}
	}
	return s.FindCredential(ctx, input.UUID)
}

func (s *IngestStore) Credentials(ctx context.Context, producer, after string, limit int) ([]models.IngestCredential, error) {
	if _, err := archiveUUID(producer); err != nil {
		return nil, err
	}
	limit, err := ingestPage(after, limit)
	if err != nil {
		return nil, err
	}
	var ids []string
	if err := dbWrapper.Select(ctx, &ids, "SELECT uuid FROM ingest_credentials WHERE producer_uuid=? AND uuid>? ORDER BY uuid LIMIT ?", producer, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.IngestCredential, 0, len(ids))
	for _, id := range ids {
		row, err := s.FindCredential(ctx, id)
		if err != nil {
			return nil, err
		}
		ret = append(ret, *row)
	}
	return ret, nil
}

func (s *IngestStore) RevokeCredential(ctx context.Context, id string) error {
	if _, err := archiveUUID(id); err != nil {
		return err
	}
	_, err := dbWrapper.Exec(ctx, "UPDATE ingest_credentials SET revoked=1 WHERE uuid=?", id)
	return err
}

type ingestReceiptRow struct {
	ProducerUUID       string         `db:"producer_uuid"`
	EventUUID          string         `db:"event_uuid"`
	Digest             string         `db:"digest"`
	CredentialUUID     string         `db:"credential_uuid"`
	CollectionUUID     string         `db:"collection_uuid"`
	CollectionRevision int            `db:"collection_revision"`
	RootUUID           *string        `db:"root_uuid"`
	RunUUID            string         `db:"run_uuid"`
	Kind               string         `db:"kind"`
	PostUUID           sql.NullString `db:"post_uuid"`
	CaptureUUID        sql.NullString `db:"capture_uuid"`
	JobUUID            sql.NullString `db:"job_uuid"`
	Result             string         `db:"result"`
	CommittedAt        Timestamp      `db:"committed_at"`
}

func (s *IngestStore) FindReceipt(ctx context.Context, producer, event string) (*models.IngestReceipt, error) {
	for _, id := range []string{producer, event} {
		if _, err := archiveUUID(id); err != nil {
			return nil, err
		}
	}
	var row ingestReceiptRow
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM ingest_receipts WHERE producer_uuid=? AND event_uuid=?", producer, event); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &models.IngestReceipt{ProducerUUID: row.ProducerUUID, EventUUID: row.EventUUID, Digest: row.Digest, CredentialUUID: row.CredentialUUID, CollectionUUID: row.CollectionUUID, CollectionRevision: row.CollectionRevision, RootUUID: row.RootUUID, RunUUID: row.RunUUID, Kind: row.Kind, PostUUID: row.PostUUID.String, CaptureUUID: row.CaptureUUID.String, JobUUID: row.JobUUID.String, Result: []byte(row.Result), CommittedAt: row.CommittedAt.Timestamp}, nil
}

func (s *IngestStore) RecordReceipt(ctx context.Context, input models.IngestReceipt) (*models.IngestReceipt, error) {
	for _, id := range []string{input.ProducerUUID, input.EventUUID, input.CredentialUUID, input.CollectionUUID, input.RunUUID} {
		if _, err := archiveUUID(id); err != nil {
			return nil, err
		}
	}
	for _, id := range []string{input.PostUUID, input.CaptureUUID, input.JobUUID} {
		if id != "" {
			if _, err := archiveUUID(id); err != nil {
				return nil, err
			}
		}
	}
	validKind := input.Kind == "source.capture" && input.CaptureUUID != "" && input.JobUUID == "" ||
		input.Kind == "file.completed" && input.JobUUID != "" && input.RootUUID != nil
	if !validIngestDigest(input.Digest) || !validKind || (input.PostUUID == "") != (input.CaptureUUID == "") || input.CollectionRevision < 1 {
		return nil, errors.New("invalid ingestion receipt")
	}
	if _, err := archive.DecodeJSONObject(input.Result, 16384); err != nil {
		return nil, err
	}
	previous, err := s.FindReceipt(ctx, input.ProducerUUID, input.EventUUID)
	if err != nil {
		return nil, err
	}
	if previous != nil {
		if previous.Digest != input.Digest {
			return nil, models.ErrIngestReplay
		}
		return previous, nil
	}
	optional := func(value string) interface{} {
		if value == "" {
			return nil
		}
		return value
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO ingest_receipts(producer_uuid,event_uuid,digest,credential_uuid,collection_uuid,collection_revision,root_uuid,run_uuid,kind,post_uuid,capture_uuid,job_uuid,result)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, input.ProducerUUID, input.EventUUID, input.Digest, input.CredentialUUID, input.CollectionUUID, input.CollectionRevision, input.RootUUID, input.RunUUID, input.Kind, optional(input.PostUUID), optional(input.CaptureUUID), optional(input.JobUUID), string(input.Result)); err != nil {
		return nil, err
	}
	return s.FindReceipt(ctx, input.ProducerUUID, input.EventUUID)
}
