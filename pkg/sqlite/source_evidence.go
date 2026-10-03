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
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type SourceEvidenceStore struct{}

const legacyRetentionPolicy = "legacy-retained-v1"

type sourcePostRow struct {
	UUID      string    `db:"uuid"`
	State     string    `db:"state"`
	Revision  int       `db:"revision"`
	CreatedAt Timestamp `db:"created_at"`
}

func (r sourcePostRow) resolve() *models.SourcePost {
	return &models.SourcePost{UUID: r.UUID, State: r.State, Revision: r.Revision, CreatedAt: r.CreatedAt.Timestamp}
}

func validatePostIdentifier(key models.SourcePostIdentifier) error {
	if !archive.ValidAccountNamespace(key.Namespace) || !validAccountText(key.Value, 2048, false) || strings.TrimSpace(key.Value) != key.Value {
		return errors.New("invalid qualified source post identifier")
	}
	return nil
}

func sourcePageLimit(limit int) (int, error) {
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return 0, errors.New("source query limit must be between 1 and 100")
	}
	return limit, nil
}

func (s *SourceEvidenceStore) FindPost(ctx context.Context, value string) (*models.SourcePost, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	var row sourcePostRow
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM source_posts WHERE uuid = ?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.resolve(), nil
}

func (s *SourceEvidenceStore) FindPostByIdentifier(ctx context.Context, key models.SourcePostIdentifier) (*models.SourcePost, error) {
	if err := validatePostIdentifier(key); err != nil {
		return nil, err
	}
	var row sourcePostRow
	if err := dbWrapper.Get(ctx, &row, `SELECT p.* FROM source_post_identifiers i JOIN source_posts p ON p.uuid = i.post_uuid
WHERE i.namespace = ? AND i.value = ?`, key.Namespace, key.Value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.resolve(), nil
}

func (s *SourceEvidenceStore) EnsurePost(ctx context.Context, key models.SourcePostIdentifier, value string) (*models.SourcePost, error) {
	var err error
	if value != "" {
		value, err = archiveUUID(value)
		if err != nil {
			return nil, err
		}
	}
	existing, err := s.FindPostByIdentifier(ctx, key)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if value != "" && value != existing.UUID {
			return nil, models.ErrSourcePostConflict
		}
		return existing, nil
	}
	if value == "" {
		value = uuid.NewString()
	}
	if previous, err := s.FindPost(ctx, value); err != nil {
		return nil, err
	} else if previous != nil {
		return nil, models.ErrSourcePostConflict
	}
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO source_posts(uuid) VALUES (?)", value); err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO source_post_identifiers(namespace, value, post_uuid) VALUES (?, ?, ?)", key.Namespace, key.Value, value); err != nil {
		return nil, err
	}
	return s.FindPost(ctx, value)
}

func (s *SourceEvidenceStore) AddPostIdentifier(ctx context.Context, value string, key models.SourcePostIdentifier, expectedRevision int) error {
	post, err := s.FindPost(ctx, value)
	if err != nil {
		return err
	}
	if post == nil || post.Revision != expectedRevision {
		return models.ErrSourcePostConflict
	}
	if post.State != "active" {
		return models.ErrSourcePostForgotten
	}
	existing, err := s.FindPostByIdentifier(ctx, key)
	if err != nil {
		return err
	}
	if existing != nil {
		if existing.UUID != post.UUID {
			return models.ErrSourcePostConflict
		}
		return nil
	}
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO source_post_identifiers(namespace, value, post_uuid) VALUES (?, ?, ?)", key.Namespace, key.Value, post.UUID); err != nil {
		return err
	}
	_, err = dbWrapper.Exec(ctx, "UPDATE source_posts SET revision = revision + 1 WHERE uuid = ?", post.UUID)
	return err
}

func (s *SourceEvidenceStore) PostIdentifiers(ctx context.Context, value string, after *models.SourcePostIdentifier, limit int) ([]models.SourcePostIdentifier, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	limit, err = sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	cursor := models.SourcePostIdentifier{}
	if after != nil {
		if err := validatePostIdentifier(*after); err != nil {
			return nil, err
		}
		cursor = *after
	}
	var rows []struct {
		Namespace string `db:"namespace"`
		Value     string `db:"value"`
	}
	if err := dbWrapper.Select(ctx, &rows, `SELECT namespace, value FROM source_post_identifiers
WHERE post_uuid = ? AND (namespace, value) > (?, ?) ORDER BY namespace, value LIMIT ?`, id, cursor.Namespace, cursor.Value, limit); err != nil {
		return nil, err
	}
	ret := make([]models.SourcePostIdentifier, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, models.SourcePostIdentifier{Namespace: row.Namespace, Value: row.Value})
	}
	return ret, nil
}

func sourceDigest(body []byte) string {
	hash := sha256.Sum256(body)
	return encodinghex.EncodeToString(hash[:])
}

func sourceSignature(domain string, value interface{}) (string, error) {
	body, err := archive.EncodeSourceJSON(value)
	if err != nil {
		return "", err
	}
	return sourceDigest(append([]byte(domain+"\x00"), body...)), nil
}

func readSourcePayload(ctx context.Context, digest string) (json.RawMessage, error) {
	return readSourcePayloadUsing(func(out any, query string, args ...any) error { return dbWrapper.Get(ctx, out, query, args...) }, digest)
}

func readSourcePayloadUsing(get func(any, string, ...any) error, digest string) (json.RawMessage, error) {
	var row struct {
		Encoding string `db:"encoding"`
		Length   int    `db:"byte_length"`
		Data     []byte `db:"data"`
	}
	if err := get(&row, "SELECT encoding, byte_length, data FROM source_payloads WHERE digest = ?", digest); err != nil {
		return nil, err
	}
	if row.Length < 2 || row.Length > archive.MaxSourcePayloadBytes || len(row.Data) > archive.MaxSourcePayloadBytes {
		return nil, models.ErrSourcePayloadCorrupt
	}
	body := row.Data
	switch row.Encoding {
	case "json":
	case "gzip":
		reader, err := gzip.NewReader(bytes.NewReader(row.Data))
		if err != nil {
			return nil, fmt.Errorf("%w: invalid compressed body", models.ErrSourcePayloadCorrupt)
		}
		body, err = io.ReadAll(io.LimitReader(reader, int64(row.Length)+1))
		closeErr := reader.Close()
		if err != nil || closeErr != nil {
			return nil, fmt.Errorf("%w: compressed body cannot be read", models.ErrSourcePayloadCorrupt)
		}
	default:
		return nil, models.ErrSourcePayloadCorrupt
	}
	if len(body) != row.Length || sourceDigest(body) != digest {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return body, nil
}

func putSourcePayload(ctx context.Context, body json.RawMessage) (string, error) {
	digest := sourceDigest(body)
	var exists bool
	if err := dbWrapper.Get(ctx, &exists, "SELECT EXISTS(SELECT 1 FROM source_payloads WHERE digest = ?)", digest); err != nil {
		return "", err
	}
	if exists {
		stored, err := readSourcePayload(ctx, digest)
		if err != nil {
			return "", err
		}
		if !bytes.Equal(body, stored) {
			return "", models.ErrSourcePayloadCorrupt
		}
		return digest, nil
	}
	encoding, data := "json", []byte(body)
	if len(body) >= 512 {
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if _, err := writer.Write(body); err != nil {
			return "", err
		}
		if err := writer.Close(); err != nil {
			return "", err
		}
		if compressed.Len()+32 < len(body) {
			encoding, data = "gzip", compressed.Bytes()
		}
	}
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO source_payloads(digest, encoding, byte_length, data) VALUES (?, ?, ?, ?)", digest, encoding, len(body), data); err != nil {
		return "", err
	}
	return digest, nil
}

func canonicalCaptureInput(input *models.SourceCaptureInput) (string, string, error) {
	var err error
	input.UUID, err = archiveUUID(input.UUID)
	if err != nil {
		return "", "", err
	}
	input.PostUUID, err = archiveUUID(input.PostUUID)
	if err != nil {
		return "", "", err
	}
	if !validAccountText(input.Origin, 128, false) || !validAccountText(input.Platform, 128, false) || input.CapturedAt.IsZero() || input.CapturedAt.UTC().Year() < 1 || input.CapturedAt.UTC().Year() > 9999 ||
		(input.ExtractorVersion != nil && !validAccountText(*input.ExtractorVersion, 128, true)) {
		return "", "", errors.New("invalid source capture provenance")
	}
	input.CapturedAt = input.CapturedAt.UTC()
	projection := make(map[string]interface{})
	for name, field := range map[string]*string{"title": input.Metadata.Title, "original_text": input.Metadata.OriginalText, "published_at": input.Metadata.PublishedAt, "date_basis": input.Metadata.DateBasis, "language": input.Metadata.Language} {
		if field != nil && !utf8.ValidString(*field) {
			return "", "", errors.New("invalid source metadata text")
		}
		if field != nil {
			projection[name] = *field
		}
	}
	metadata, err := archive.EncodeSourceJSON(projection)
	if err != nil {
		return "", "", err
	}
	if len(metadata) > 262144 {
		return "", "", errors.New("source metadata projection exceeds 256 KiB")
	}
	retained, err := archive.RestoreCapture(&input.Payload)
	if err != nil {
		return "", "", err
	}
	switch input.RetentionPolicy {
	case archive.SourceRetentionVersion:
		clean, err := archive.RetainSourcePayload(retained)
		if err != nil {
			return "", "", err
		}
		if !bytes.Equal(clean, retained) {
			return "", "", errors.New("source capture does not satisfy its declared retention policy")
		}
	case legacyRetentionPolicy:
		// This policy belongs to the trusted catalog import boundary. Network
		// producers must negotiate the current source retention policy instead.
	default:
		return "", "", errors.New("unsupported source retention policy")
	}
	// Partition again from verified evidence so callers cannot manufacture a
	// different post revision by placing arbitrary keys in the attachment patch.
	prepared, err := archive.PrepareRetainedCapture(input.Origin, input.Platform, retained)
	if err != nil {
		return "", "", err
	}
	input.Payload = *prepared
	input.Payload.Refs = slices.Clone(input.Payload.Refs)
	slices.SortFunc(input.Payload.Refs, func(a, b models.SourceProfileReference) int {
		if c := strings.Compare(a.Part, b.Part); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	revisionSignature, err := sourceSignature("stash-post-revision-v1", []interface{}{archive.CaptureStructureVersion, sourceDigest(input.Payload.Shared), json.RawMessage(metadata)})
	if err != nil {
		return "", "", err
	}
	return string(metadata), revisionSignature, nil
}

func captureSignature(input models.SourceCaptureInput, revisionSignature, patchDigest string, refs []models.SourceProfileReference) (string, error) {
	references := make([][]string, 0, len(refs))
	for _, ref := range refs {
		references = append(references, []string{ref.Part, ref.Path, ref.Hash})
	}
	return sourceSignature("stash-source-capture-v1", []interface{}{
		input.PostUUID, revisionSignature, input.Origin, input.Platform, input.CapturedAt.Format(accountObservationTimeFormat),
		input.ExtractorVersion, input.RetentionPolicy, patchDigest, references,
	})
}

func retainSourceProfile(ctx context.Context, profile models.SourceProfileBody) error {
	digest, err := putSourcePayload(ctx, profile.Body)
	if err != nil {
		return err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_profile_bodies(hash,namespace,payload_digest) VALUES(?,?,?) ON CONFLICT(hash) DO NOTHING`, profile.Hash, profile.Namespace, digest); err != nil {
		return err
	}
	var same bool
	if err := dbWrapper.Get(ctx, &same, "SELECT namespace=? AND payload_digest=? FROM source_profile_bodies WHERE hash=?", profile.Namespace, digest, profile.Hash); err != nil {
		return err
	}
	if !same {
		return models.ErrSourcePayloadCorrupt
	}
	return nil
}

func (s *SourceEvidenceStore) RetainProfile(ctx context.Context, namespace string, body json.RawMessage) (*models.SourceProfileBody, error) {
	profile, err := archive.PrepareRetainedProfile(namespace, body)
	if err != nil {
		return nil, err
	}
	if err := retainSourceProfile(ctx, *profile); err != nil {
		return nil, err
	}
	return profile, nil
}

func (s *SourceEvidenceStore) RecordCapture(ctx context.Context, input models.SourceCaptureInput) (*models.SourceCapture, error) {
	metadata, revisionSignature, err := canonicalCaptureInput(&input)
	if err != nil {
		return nil, err
	}
	signature, err := captureSignature(input, revisionSignature, sourceDigest(input.Payload.Patch), input.Payload.Refs)
	if err != nil {
		return nil, err
	}
	var previousSignature string
	err = dbWrapper.Get(ctx, &previousSignature, "SELECT signature FROM source_captures WHERE uuid = ?", input.UUID)
	if err == nil {
		if previousSignature != signature {
			return nil, models.ErrSourceCaptureReplay
		}
		return s.FindCapture(ctx, input.UUID)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	post, err := s.FindPost(ctx, input.PostUUID)
	if err != nil {
		return nil, err
	}
	if post == nil {
		return nil, models.ErrSourcePostConflict
	}
	if post.State != "active" {
		return nil, models.ErrSourcePostForgotten
	}
	bodyDigest, err := putSourcePayload(ctx, input.Payload.Shared)
	if err != nil {
		return nil, err
	}
	patchDigest, err := putSourcePayload(ctx, input.Payload.Patch)
	if err != nil {
		return nil, err
	}
	for _, profile := range input.Payload.Profiles {
		if err := retainSourceProfile(ctx, profile); err != nil {
			return nil, err
		}
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_post_revisions(uuid, post_uuid, signature, body_digest, metadata, structure_version)
VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(post_uuid, signature) DO NOTHING`, uuid.NewString(), post.UUID, revisionSignature, bodyDigest, metadata, archive.CaptureStructureVersion); err != nil {
		return nil, err
	}
	var revisionUUID string
	if err := dbWrapper.Get(ctx, &revisionUUID, "SELECT uuid FROM source_post_revisions WHERE post_uuid = ? AND signature = ? AND body_digest = ? AND metadata = ? AND structure_version = ?", post.UUID, revisionSignature, bodyDigest, metadata, archive.CaptureStructureVersion); err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_captures(uuid, post_uuid, revision_uuid, origin, platform, captured_at, extractor_version, retention_policy, patch_digest, signature)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, input.UUID, post.UUID, revisionUUID, input.Origin, input.Platform,
		input.CapturedAt.Format(accountObservationTimeFormat), input.ExtractorVersion, input.RetentionPolicy, patchDigest, signature); err != nil {
		return nil, err
	}
	for _, ref := range input.Payload.Refs {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO source_capture_profiles(capture_uuid, part, path, profile_hash) VALUES (?, ?, ?, ?)", input.UUID, ref.Part, ref.Path, ref.Hash); err != nil {
			return nil, err
		}
	}
	if _, err := dbWrapper.Exec(ctx, "UPDATE source_posts SET revision = revision + 1 WHERE uuid = ?", post.UUID); err != nil {
		return nil, err
	}
	return s.FindCapture(ctx, input.UUID)
}

type sourceCaptureRow struct {
	UUID              string         `db:"uuid"`
	PostUUID          string         `db:"post_uuid"`
	RevisionUUID      string         `db:"revision_uuid"`
	Origin            string         `db:"origin"`
	Platform          string         `db:"platform"`
	CapturedAt        Timestamp      `db:"captured_at"`
	ExtractorVersion  sql.NullString `db:"extractor_version"`
	RetentionPolicy   string         `db:"retention_policy"`
	PatchDigest       string         `db:"patch_digest"`
	BodyDigest        string         `db:"body_digest"`
	Metadata          string         `db:"metadata"`
	StructureVersion  string         `db:"structure_version"`
	RevisionSignature string         `db:"revision_signature"`
	Signature         string         `db:"signature"`
}

const sourceCaptureColumns = `c.uuid, c.post_uuid, c.revision_uuid, c.origin, c.platform, c.captured_at,
c.extractor_version, c.retention_policy, c.patch_digest, c.signature, r.body_digest, r.metadata, r.structure_version, r.signature AS revision_signature`

func (r sourceCaptureRow) resolve() (*models.SourceCapture, error) {
	ret := &models.SourceCapture{UUID: r.UUID, PostUUID: r.PostUUID, RevisionUUID: r.RevisionUUID,
		Origin: r.Origin, Platform: r.Platform, CapturedAt: r.CapturedAt.Timestamp, RetentionPolicy: r.RetentionPolicy, StructureVersion: r.StructureVersion}
	if r.ExtractorVersion.Valid {
		ret.ExtractorVersion = &r.ExtractorVersion.String
	}
	if err := json.Unmarshal([]byte(r.Metadata), &ret.Metadata); err != nil {
		return nil, err
	}
	return ret, nil
}

func (s *SourceEvidenceStore) FindCapture(ctx context.Context, value string) (*models.SourceCapture, error) {
	return findSourceCapture(
		func(out any, query string, args ...any) error { return dbWrapper.Get(ctx, out, query, args...) },
		func(out any, query string, args ...any) error { return dbWrapper.Select(ctx, out, query, args...) }, value)
}

func findSourceCapture(get func(any, string, ...any) error, selectRows func(any, string, ...any) error, value string) (*models.SourceCapture, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	var row sourceCaptureRow
	if err := get(&row, "SELECT "+sourceCaptureColumns+` FROM source_captures c
JOIN source_post_revisions r ON r.post_uuid = c.post_uuid AND r.uuid = c.revision_uuid WHERE c.uuid = ?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	ret, err := row.resolve()
	if err != nil {
		return nil, err
	}
	if ret.StructureVersion != archive.CaptureStructureVersion {
		return nil, errors.New("unsupported stored source capture structure")
	}
	payload := &models.SourceCapturePayload{Profiles: []models.SourceProfileBody{}, Refs: []models.SourceProfileReference{}}
	payload.Shared, err = readSourcePayloadUsing(get, row.BodyDigest)
	if err != nil {
		return nil, err
	}
	payload.Patch, err = readSourcePayloadUsing(get, row.PatchDigest)
	if err != nil {
		return nil, err
	}
	var refs []struct {
		Part      string `db:"part"`
		Path      string `db:"path"`
		Hash      string `db:"profile_hash"`
		Namespace string `db:"namespace"`
		Digest    string `db:"payload_digest"`
	}
	if err := selectRows(&refs, `SELECT r.part, r.path, r.profile_hash, p.namespace, p.payload_digest
FROM source_capture_profiles r JOIN source_profile_bodies p ON p.hash = r.profile_hash
WHERE r.capture_uuid = ? ORDER BY r.part, r.path LIMIT 1025`, id); err != nil {
		return nil, err
	}
	if len(refs) > 1024 {
		return nil, models.ErrSourcePayloadCorrupt
	}
	loaded := make(map[string]bool)
	for _, ref := range refs {
		payload.Refs = append(payload.Refs, models.SourceProfileReference{Part: ref.Part, Path: ref.Path, Hash: ref.Hash})
		if !loaded[ref.Hash] {
			body, err := readSourcePayloadUsing(get, ref.Digest)
			if err != nil {
				return nil, err
			}
			payload.Profiles = append(payload.Profiles, models.SourceProfileBody{Hash: ref.Hash, Namespace: ref.Namespace, Body: body})
			loaded[ref.Hash] = true
		}
	}
	slices.SortFunc(payload.Profiles, func(a, b models.SourceProfileBody) int { return strings.Compare(a.Hash, b.Hash) })
	if _, err := archive.RestoreCapture(payload); err != nil {
		return nil, fmt.Errorf("%w: %v", models.ErrSourcePayloadCorrupt, err)
	}
	metadata, err := archive.DecodeJSONObject([]byte(row.Metadata), 262144)
	if err != nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	revisionSignature, err := sourceSignature("stash-post-revision-v1", []interface{}{row.StructureVersion, row.BodyDigest, metadata})
	if err != nil {
		return nil, err
	}
	signature, err := captureSignature(models.SourceCaptureInput{PostUUID: ret.PostUUID, Origin: ret.Origin,
		Platform: ret.Platform, CapturedAt: ret.CapturedAt, ExtractorVersion: ret.ExtractorVersion, RetentionPolicy: ret.RetentionPolicy}, revisionSignature, row.PatchDigest, payload.Refs)
	if err != nil {
		return nil, err
	}
	if row.RevisionSignature != revisionSignature || row.Signature != signature {
		return nil, models.ErrSourcePayloadCorrupt
	}
	ret.Payload = payload
	return ret, nil
}

func (s *SourceEvidenceStore) Captures(ctx context.Context, value string, after *models.SourceCaptureCursor, limit int) ([]*models.SourceCapture, error) {
	postUUID, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	limit, err = sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	afterTime, afterUUID := "", ""
	if after != nil {
		afterUUID, err = archiveUUID(after.UUID)
		if err != nil || after.CapturedAt.IsZero() {
			return nil, errors.New("invalid source capture cursor")
		}
		afterTime = after.CapturedAt.UTC().Format(accountObservationTimeFormat)
	}
	var rows []sourceCaptureRow
	if err := dbWrapper.Select(ctx, &rows, "SELECT "+sourceCaptureColumns+` FROM source_captures c
JOIN source_post_revisions r ON r.post_uuid = c.post_uuid AND r.uuid = c.revision_uuid
WHERE c.post_uuid = ? AND (c.captured_at, c.uuid) > (?, ?) ORDER BY c.captured_at, c.uuid LIMIT ?`, postUUID, afterTime, afterUUID, limit); err != nil {
		return nil, err
	}
	ret := make([]*models.SourceCapture, 0, len(rows))
	for _, row := range rows {
		capture, err := row.resolve()
		if err != nil {
			return nil, err
		}
		ret = append(ret, capture)
	}
	return ret, nil
}
