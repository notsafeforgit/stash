package sqlite

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	encodinghex "encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type SourceDocumentStore struct{}

const documentColumns = `d.uuid,d.content_sha256,c.byte_size,d.encoding,d.parser,d.parse_status,CAST(d.warnings AS BLOB) AS warnings,CAST(d.parsed AS BLOB) AS parsed`
const documentSourceColumns = `uuid,document_uuid,collection_uuid,collection_revision,relative_path,post_uuid,captured_at,origin,CAST(details AS BLOB) AS details,recorded_at`
const documentClaimColumns = `uuid,source_uuid,collection_uuid,relative_path,observed_at,origin,CAST(details AS BLOB) AS details,recorded_at`

type documentContentRow struct {
	Hash     string `db:"content_sha256"`
	ByteSize int64  `db:"byte_size"`
	Encoding string `db:"storage_encoding"`
	Data     []byte `db:"data"`
}

func decodeDocumentContent(row documentContentRow) ([]byte, error) {
	if !archive.ValidSHA256(row.Hash) || row.ByteSize < 0 || row.ByteSize > archive.MaxDocumentBytes || len(row.Data) > archive.MaxDocumentBytes {
		return nil, models.ErrSourcePayloadCorrupt
	}
	data := row.Data
	switch row.Encoding {
	case "gzip":
		reader, err := gzip.NewReader(bytes.NewReader(row.Data))
		if err != nil {
			return nil, models.ErrSourcePayloadCorrupt
		}
		data, err = io.ReadAll(io.LimitReader(reader, row.ByteSize+1))
		closeErr := reader.Close()
		if err != nil || closeErr != nil {
			return nil, models.ErrSourcePayloadCorrupt
		}
	case "raw":
	default:
		return nil, models.ErrSourcePayloadCorrupt
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) != row.ByteSize || encodinghex.EncodeToString(sum[:]) != row.Hash {
		return nil, models.ErrSourcePayloadCorrupt
	}
	if data == nil {
		data = []byte{}
	}
	return data, nil
}

func documentAtomicWrite(ctx context.Context) *bool {
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return models.ErrSourceDocumentConflict
		}
		return nil
	})
	return &complete
}

func (s *SourceDocumentStore) Retain(ctx context.Context, input models.SourceDocumentInput) (*models.SourceDocument, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	doc, err := archive.PrepareSourceDocument(input)
	if err != nil {
		return nil, err
	}
	content, err := s.Content(ctx, doc.ContentSHA256)
	if err != nil {
		return nil, err
	}
	if content != nil && !bytes.Equal(content, input.Content) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	complete := documentAtomicWrite(ctx)
	if content == nil {
		data, encoding := input.Content, "raw"
		if data == nil {
			data = []byte{}
		}
		var compressed bytes.Buffer
		writer, err := gzip.NewWriterLevel(&compressed, gzip.BestSpeed)
		if err != nil {
			return nil, err
		}
		if _, err := writer.Write(data); err != nil {
			return nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		if compressed.Len() < len(data) {
			data, encoding = compressed.Bytes(), "gzip"
		}
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO source_document_contents(content_sha256,byte_size,storage_encoding,data) VALUES(?,?,?,?)", doc.ContentSHA256, doc.ByteSize, encoding, data); err != nil {
			return nil, err
		}
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_documents(uuid,content_sha256,encoding,parser,parse_status,warnings,parsed)
VALUES(?,?,?,?,?,?,?) ON CONFLICT(uuid) DO NOTHING`, doc.UUID, doc.ContentSHA256, doc.Encoding, doc.Parser, doc.ParseStatus, string(doc.Warnings), string(doc.Parsed)); err != nil {
		return nil, err
	}
	stored, err := s.Find(ctx, doc.UUID)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(doc, stored) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	*complete = true
	return stored, nil
}

func (s *SourceDocumentStore) Find(ctx context.Context, id string) (*models.SourceDocument, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrSourceDocumentInvalid
	}
	var doc models.SourceDocument
	err := dbWrapper.Get(ctx, &doc, "SELECT "+documentColumns+" FROM source_documents d JOIN source_document_contents c ON c.content_sha256=d.content_sha256 WHERE d.uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	expected, err := archive.DocumentIdentity(doc)
	if err != nil || expected != id {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return &doc, nil
}

func (s *SourceDocumentStore) Content(ctx context.Context, hash string) ([]byte, error) {
	if !archive.ValidSHA256(hash) {
		return nil, models.ErrSourceDocumentInvalid
	}
	var row documentContentRow
	err := dbWrapper.Get(ctx, &row, "SELECT * FROM source_document_contents WHERE content_sha256=?", hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeDocumentContent(row)
}

func documentOrigin(value string) bool {
	return value == "capture" || value == "migration" || value == "review"
}

func documentDetails(raw *json.RawMessage) error {
	value, err := accountEvidenceJSON(*raw)
	if err != nil {
		return models.ErrSourceDocumentInvalid
	}
	*raw = json.RawMessage(value)
	return nil
}

func documentFind[T any](ctx context.Context, id, query string) (*T, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrSourceDocumentInvalid
	}
	var ret T
	err := dbWrapper.Get(ctx, &ret, query+" WHERE uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &ret, err
}

func (s *SourceDocumentStore) Source(ctx context.Context, id string) (*models.SourceDocumentSource, error) {
	return documentFind[models.SourceDocumentSource](ctx, id, "SELECT "+documentSourceColumns+" FROM source_document_sources")
}

func (s *SourceDocumentStore) activeSourcePost(ctx context.Context, post *string) error {
	if post == nil {
		return nil
	}
	if !validSourceRunUUID(*post) {
		return models.ErrSourceDocumentInvalid
	}
	current, err := (&SourceEvidenceStore{}).FindPost(ctx, *post)
	if err != nil {
		return err
	}
	if current == nil {
		return models.ErrSourceDocumentInvalid
	}
	if current.State != "active" {
		return models.ErrSourcePostForgotten
	}
	return nil
}

func (s *SourceDocumentStore) RecordSource(ctx context.Context, input models.SourceDocumentSource) (*models.SourceDocumentSource, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(input.UUID) || !validSourceRunUUID(input.DocumentUUID) || !validSourceRunUUID(input.CollectionUUID) ||
		(input.PostUUID != nil && !validSourceRunUUID(*input.PostUUID)) || input.CollectionRevision < 1 ||
		!archive.ValidDocumentPath(input.RelativePath) || !archive.ValidDocumentSourceTime(input.CapturedAt) || !documentOrigin(input.Origin) {
		return nil, models.ErrSourceDocumentInvalid
	}
	if err := documentDetails(&input.Details); err != nil {
		return nil, err
	}
	prior, err := s.Source(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		input.RecordedAt = prior.RecordedAt
		if !reflect.DeepEqual(prior, &input) {
			return nil, models.ErrSourceDocumentReplay
		}
		return prior, nil
	}
	if err := s.activeSourcePost(ctx, input.PostUUID); err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_document_sources(uuid,document_uuid,collection_uuid,collection_revision,relative_path,post_uuid,captured_at,origin,details)
VALUES(?,?,?,?,?,?,?,?,?)`, input.UUID, input.DocumentUUID, input.CollectionUUID, input.CollectionRevision, input.RelativePath, input.PostUUID, input.CapturedAt, input.Origin, string(input.Details)); err != nil {
		return nil, err
	}
	return s.Source(ctx, input.UUID)
}

func documentPage(after string, limit int) (int, error) {
	if after != "" && !validSourceRunUUID(after) {
		return 0, models.ErrSourceDocumentInvalid
	}
	return sourcePageLimit(limit)
}

func documentLocation(collection, path string) bool {
	return validSourceRunUUID(collection) && archive.ValidDocumentPath(path)
}

func (s *SourceDocumentStore) PostSources(ctx context.Context, post, after string, limit int) ([]models.SourceDocumentSource, error) {
	if !validSourceRunUUID(post) {
		return nil, models.ErrSourceDocumentInvalid
	}
	limit, err := documentPage(after, limit)
	if err != nil {
		return nil, err
	}
	ret := []models.SourceDocumentSource{}
	err = dbWrapper.Select(ctx, &ret, "SELECT "+documentSourceColumns+" FROM source_document_sources WHERE post_uuid=? AND uuid>? ORDER BY uuid LIMIT ?", post, after, limit)
	return ret, err
}

func (s *SourceDocumentStore) LocationSources(ctx context.Context, collection, path, after string, limit int) ([]models.SourceDocumentSource, error) {
	if !documentLocation(collection, path) {
		return nil, models.ErrSourceDocumentInvalid
	}
	limit, err := documentPage(after, limit)
	if err != nil {
		return nil, err
	}
	ret := []models.SourceDocumentSource{}
	err = dbWrapper.Select(ctx, &ret, "SELECT "+documentSourceColumns+" FROM source_document_sources WHERE collection_uuid=? AND relative_path=? AND uuid>? ORDER BY uuid LIMIT ?", collection, path, after, limit)
	return ret, err
}

func (s *SourceDocumentStore) RecordHeadClaim(ctx context.Context, input models.SourceDocumentHeadClaim) (*models.SourceDocumentHeadClaim, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(input.UUID) || !validSourceRunUUID(input.SourceUUID) || !documentLocation(input.CollectionUUID, input.RelativePath) ||
		!documentOrigin(input.Origin) || !archive.ValidDocumentSourceTime(input.ObservedAt) {
		return nil, models.ErrSourceDocumentInvalid
	}
	if err := documentDetails(&input.Details); err != nil {
		return nil, err
	}
	prior, err := s.HeadClaim(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		input.RecordedAt = prior.RecordedAt
		if !reflect.DeepEqual(prior, &input) {
			return nil, models.ErrSourceDocumentReplay
		}
		return prior, nil
	}
	source, err := s.Source(ctx, input.SourceUUID)
	if err != nil {
		return nil, err
	}
	if source == nil || source.CollectionUUID != input.CollectionUUID || source.RelativePath != input.RelativePath {
		return nil, models.ErrSourceDocumentInvalid
	}
	if err := s.activeSourcePost(ctx, source.PostUUID); err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_document_head_claims(uuid,source_uuid,collection_uuid,relative_path,observed_at,origin,details) VALUES(?,?,?,?,?,?,?)`,
		input.UUID, input.SourceUUID, input.CollectionUUID, input.RelativePath, input.ObservedAt, input.Origin, string(input.Details)); err != nil {
		return nil, err
	}
	return s.HeadClaim(ctx, input.UUID)
}

func (s *SourceDocumentStore) HeadClaim(ctx context.Context, id string) (*models.SourceDocumentHeadClaim, error) {
	return documentFind[models.SourceDocumentHeadClaim](ctx, id, "SELECT "+documentClaimColumns+" FROM source_document_head_claims")
}

func (s *SourceDocumentStore) HeadClaims(ctx context.Context, collection, path, after string, limit int) ([]models.SourceDocumentHeadClaim, error) {
	if !documentLocation(collection, path) {
		return nil, models.ErrSourceDocumentInvalid
	}
	limit, err := documentPage(after, limit)
	if err != nil {
		return nil, err
	}
	ret := []models.SourceDocumentHeadClaim{}
	err = dbWrapper.Select(ctx, &ret, "SELECT "+documentClaimColumns+" FROM source_document_head_claims WHERE collection_uuid=? AND relative_path=? AND uuid>? ORDER BY uuid LIMIT ?", collection, path, after, limit)
	return ret, err
}

func (s *SourceDocumentStore) Head(ctx context.Context, collection, path string) (*models.SourceDocumentHead, error) {
	if !documentLocation(collection, path) {
		return nil, models.ErrSourceDocumentInvalid
	}
	var ret models.SourceDocumentHead
	err := dbWrapper.Get(ctx, &ret, `SELECT d.* FROM source_document_heads h JOIN source_document_head_decisions d ON d.uuid=h.decision_uuid WHERE h.collection_uuid=? AND h.relative_path=?`, collection, path)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &ret, err
}

func (s *SourceDocumentStore) DecideHead(ctx context.Context, input models.SourceDocumentHeadInput) (*models.SourceDocumentHead, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !documentLocation(input.CollectionUUID, input.RelativePath) || input.ExpectedRevision < 0 || !documentOrigin(input.Origin) || !validAccountText(input.Reason, 4096, true) {
		return nil, models.ErrSourceDocumentInvalid
	}
	prior, err := s.Head(ctx, input.CollectionUUID, input.RelativePath)
	if err != nil {
		return nil, err
	}
	revision := 0
	if prior != nil {
		revision = prior.Revision
	}
	if revision != input.ExpectedRevision || (prior != nil && input.Origin != "review" && (prior.Origin == "review" || prior.Origin == "migration" || input.Origin == "migration")) {
		return nil, models.ErrSourceDocumentConflict
	}
	var sourceID, claimID *string
	switch input.State {
	case "linked":
		source, err := s.Source(ctx, input.SourceUUID)
		if err != nil {
			return nil, err
		}
		if source == nil || source.CollectionUUID != input.CollectionUUID || source.RelativePath != input.RelativePath {
			return nil, models.ErrSourceDocumentInvalid
		}
		if err := s.activeSourcePost(ctx, source.PostUUID); err != nil {
			return nil, err
		}
		sourceID = &source.UUID
		if input.ClaimUUID != "" {
			claim, err := s.HeadClaim(ctx, input.ClaimUUID)
			if err != nil {
				return nil, err
			}
			if claim == nil || claim.SourceUUID != source.UUID {
				return nil, models.ErrSourceDocumentInvalid
			}
			claimID = &claim.UUID
		}
	case "unlinked":
		if input.SourceUUID != "" || input.ClaimUUID != "" {
			return nil, models.ErrSourceDocumentInvalid
		}
	default:
		return nil, models.ErrSourceDocumentInvalid
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_document_head_decisions(uuid,collection_uuid,relative_path,revision,state,source_uuid,claim_uuid,origin,reason) VALUES(?,?,?,?,?,?,?,?,?)`,
		uuid.NewString(), input.CollectionUUID, input.RelativePath, revision+1, input.State, sourceID, claimID, input.Origin, input.Reason); err != nil {
		return nil, err
	}
	return s.Head(ctx, input.CollectionUUID, input.RelativePath)
}

func (s *SourceDocumentStore) HeadHistory(ctx context.Context, collection, path string, after, limit int) ([]models.SourceDocumentHead, error) {
	if !documentLocation(collection, path) || after < 0 {
		return nil, models.ErrSourceDocumentInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	ret := []models.SourceDocumentHead{}
	err = dbWrapper.Select(ctx, &ret, "SELECT * FROM source_document_head_decisions WHERE collection_uuid=? AND relative_path=? AND revision>? ORDER BY revision LIMIT ?", collection, path, after, limit)
	return ret, err
}
