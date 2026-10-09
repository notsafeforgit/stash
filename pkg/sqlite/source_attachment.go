package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type SourceAttachmentStore struct{}

type sourceAttachmentRow struct {
	UUID      string `db:"uuid"`
	PostUUID  string `db:"post_uuid"`
	Namespace string `db:"namespace"`
	Value     string `db:"value"`
	Revision  int    `db:"revision"`
}

func (r sourceAttachmentRow) resolve() *models.SourceAttachment {
	return &models.SourceAttachment{UUID: r.UUID, PostUUID: r.PostUUID, Revision: r.Revision,
		Reference: models.SourcePostIdentifier{Namespace: r.Namespace, Value: r.Value}}
}

func (s *SourceAttachmentStore) Find(ctx context.Context, value string) (*models.SourceAttachment, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	var row sourceAttachmentRow
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM source_attachments WHERE uuid = ?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.resolve(), nil
}

func (s *SourceAttachmentStore) Lookup(ctx context.Context, post string, ref models.SourcePostIdentifier) (*models.SourceAttachment, error) {
	id, err := archiveUUID(post)
	if err != nil {
		return nil, err
	}
	if err := validatePostIdentifier(ref); err != nil {
		return nil, err
	}
	var row sourceAttachmentRow
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM source_attachments WHERE post_uuid = ? AND namespace = ? AND value = ?", id, ref.Namespace, ref.Value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.resolve(), nil
}

type attachmentManifestRow struct {
	UUID          string        `db:"uuid"`
	PostUUID      string        `db:"post_uuid"`
	Version       string        `db:"version"`
	Signature     string        `db:"signature"`
	Complete      bool          `db:"complete"`
	DeclaredAlbum bool          `db:"declared_album"`
	ExpectedCount sql.NullInt64 `db:"expected_count"`
	EntryCount    int           `db:"entry_count"`
}

func (r attachmentManifestRow) resolve() *models.SourceAttachmentManifest {
	ret := &models.SourceAttachmentManifest{UUID: r.UUID, PostUUID: r.PostUUID, Version: r.Version, Signature: r.Signature,
		Complete: r.Complete, DeclaredAlbum: r.DeclaredAlbum, EntryCount: r.EntryCount}
	if r.ExpectedCount.Valid {
		count := int(r.ExpectedCount.Int64)
		ret.ExpectedCount = &count
	}
	return ret
}

func attachmentManifestEntries(ctx context.Context, id string, after, limit int) ([]models.SourceAttachmentManifestEntry, error) {
	var rows []struct {
		sourceAttachmentRow
		Position  int    `db:"position"`
		MediaKind string `db:"media_kind"`
	}
	if err := dbWrapper.Select(ctx, &rows, `SELECT a.*, e.position, e.media_kind FROM source_attachment_entries e
JOIN source_attachments a ON a.uuid = e.attachment_uuid AND a.post_uuid = e.post_uuid
WHERE e.manifest_uuid = ? AND e.position > ? ORDER BY e.position LIMIT ?`, id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.SourceAttachmentManifestEntry, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, models.SourceAttachmentManifestEntry{Position: row.Position, MediaKind: row.MediaKind, Attachment: *row.resolve()})
	}
	return ret, nil
}

func (s *SourceAttachmentStore) FindManifest(ctx context.Context, value string) (*models.SourceAttachmentManifest, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	var row attachmentManifestRow
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM source_attachment_manifests WHERE uuid = ?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	ret := row.resolve()
	entries, err := attachmentManifestEntries(ctx, id, -1, archive.MaxManifestEntries+1)
	if err != nil {
		return nil, err
	}
	if _, err := validatedAttachmentManifest(ret, entries); err != nil {
		return nil, err
	}
	return ret, nil
}

func validatedAttachmentManifest(ret *models.SourceAttachmentManifest, entries []models.SourceAttachmentManifestEntry) (models.SourceAttachmentManifestInput, error) {
	input := models.SourceAttachmentManifestInput{Complete: ret.Complete, DeclaredAlbum: ret.DeclaredAlbum, ExpectedCount: ret.ExpectedCount}
	if ret.Version != archive.AttachmentManifestVersion || len(entries) != ret.EntryCount {
		return input, models.ErrSourcePayloadCorrupt
	}
	for _, entry := range entries {
		input.Entries = append(input.Entries, models.SourceAttachmentEntry{Position: entry.Position, MediaKind: entry.MediaKind, Reference: entry.Attachment.Reference})
	}
	input, err := archive.NormalizeAttachmentManifest(input)
	if err != nil {
		return input, fmt.Errorf("%w: %v", models.ErrSourcePayloadCorrupt, err)
	}
	signature, err := sourceSignature("stash-source-attachments-v1", archive.AttachmentManifestSignatureInput(input))
	if err != nil {
		return input, err
	}
	if signature != ret.Signature {
		return input, models.ErrSourcePayloadCorrupt
	}
	return input, nil
}

func (s *SourceAttachmentStore) ManifestForCapture(ctx context.Context, value string) (*models.SourceAttachmentManifest, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	var manifest string
	if err := dbWrapper.Get(ctx, &manifest, "SELECT manifest_uuid FROM source_capture_attachment_manifests WHERE capture_uuid = ?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return s.FindManifest(ctx, manifest)
}

func (s *SourceAttachmentStore) ManifestEntries(ctx context.Context, value string, after, limit int) ([]models.SourceAttachmentManifestEntry, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	limit, err = sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	if after < -1 || after >= archive.MaxManifestPositions {
		return nil, errors.New("invalid source attachment position cursor")
	}
	return attachmentManifestEntries(ctx, id, after, limit)
}

func (s *SourceAttachmentStore) RecordManifest(ctx context.Context, input models.SourceAttachmentManifestInput) (*models.SourceAttachmentManifest, error) {
	input, err := archive.NormalizeAttachmentManifest(input)
	if err != nil {
		return nil, err
	}
	input.CaptureUUID, err = archiveUUID(input.CaptureUUID)
	if err != nil {
		return nil, err
	}
	signature, err := sourceSignature("stash-source-attachments-v1", archive.AttachmentManifestSignatureInput(input))
	if err != nil {
		return nil, err
	}
	if existing, err := s.ManifestForCapture(ctx, input.CaptureUUID); err != nil {
		return nil, err
	} else if existing != nil {
		if existing.Signature != signature {
			return nil, models.ErrAttachmentManifestReplay
		}
		return existing, nil
	}
	var post struct {
		UUID  string `db:"uuid"`
		State string `db:"state"`
	}
	if err := dbWrapper.Get(ctx, &post, `SELECT p.uuid, p.state FROM source_captures c
JOIN source_posts p ON p.uuid = c.post_uuid WHERE c.uuid = ?`, input.CaptureUUID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, models.ErrSourceAttachmentConflict
		}
		return nil, err
	}
	if post.State != "active" {
		return nil, models.ErrSourcePostForgotten
	}
	manifest, err := s.recordManifestForPost(ctx, post.UUID, input, func(string, models.SourcePostIdentifier) string { return uuid.NewString() })
	if err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO source_capture_attachment_manifests(capture_uuid, post_uuid, manifest_uuid) VALUES (?, ?, ?)", input.CaptureUUID, post.UUID, manifest.UUID); err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, "UPDATE source_posts SET revision = revision + 1 WHERE uuid = ?", post.UUID); err != nil {
		return nil, err
	}
	return manifest, nil
}

// recordManifestForPost stores an immutable list independently of its witness.
// Captured lists and filename recovery both use this writer; recovery does not
// claim that a remote capture supplied a list it never contained.
func (s *SourceAttachmentStore) recordManifestForPost(ctx context.Context, post string, input models.SourceAttachmentManifestInput, attachmentID func(string, models.SourcePostIdentifier) string) (*models.SourceAttachmentManifest, error) {
	input, err := archive.NormalizeAttachmentManifest(input)
	if err != nil {
		return nil, err
	}
	signature, err := sourceSignature("stash-source-attachments-v1", archive.AttachmentManifestSignatureInput(input))
	if err != nil {
		return nil, err
	}
	var manifestID string
	err = dbWrapper.Get(ctx, &manifestID, "SELECT uuid FROM source_attachment_manifests WHERE post_uuid = ? AND signature = ?", post, signature)
	if errors.Is(err, sql.ErrNoRows) {
		manifestID = uuid.NewString()
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_attachment_manifests(uuid, post_uuid, version, signature, complete, declared_album, expected_count, entry_count)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, manifestID, post, archive.AttachmentManifestVersion, signature, input.Complete, input.DeclaredAlbum, input.ExpectedCount, len(input.Entries)); err != nil {
			return nil, err
		}
		for _, entry := range input.Entries {
			if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_attachments(uuid, post_uuid, namespace, value)
VALUES (?, ?, ?, ?) ON CONFLICT(post_uuid, namespace, value) DO NOTHING`, attachmentID(post, entry.Reference), post, entry.Reference.Namespace, entry.Reference.Value); err != nil {
				return nil, err
			}
			attachment, err := s.Lookup(ctx, post, entry.Reference)
			if err != nil {
				return nil, err
			}
			if attachment == nil {
				return nil, models.ErrSourcePayloadCorrupt
			}
			if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_attachment_entries(manifest_uuid, post_uuid, position, attachment_uuid, media_kind)
VALUES (?, ?, ?, ?, ?)`, manifestID, post, entry.Position, attachment.UUID, entry.MediaKind); err != nil {
				return nil, err
			}
		}
	} else if err != nil {
		return nil, err
	}
	return s.FindManifest(ctx, manifestID)
}

func activeAttachmentPost(ctx context.Context, attachment *models.SourceAttachment) error {
	post, err := (&SourceEvidenceStore{}).FindPost(ctx, attachment.PostUUID)
	if err != nil {
		return err
	}
	if post == nil || post.State != "active" {
		return models.ErrSourcePostForgotten
	}
	return nil
}
