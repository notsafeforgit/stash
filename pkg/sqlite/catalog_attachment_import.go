package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

type CatalogAttachmentImportStore struct{}

const catalogAttachmentColumns = `r.ordinal,e.source_table,e.source_key,e.data_sha256,r.post_uuid,r.capture_uuid,r.manifest_uuid,r.selection_uuid,r.outcome,r.reason,r.selection_changed`
const catalogAttachmentJoins = ` FROM catalog_attachment_records r JOIN catalog_evidence_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal`

func (s *CatalogAttachmentImportStore) Find(ctx context.Context, id string) (*models.CatalogAttachmentImport, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	ret := &models.CatalogAttachmentImport{}
	if err := dbWrapper.Get(ctx, ret, "SELECT * FROM catalog_attachment_imports WHERE snapshot_uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return ret, nil
}

type catalogAttachmentConflict struct {
	Kind     string `json:"kind"`
	Position *int   `json:"position,omitempty"`
}

type catalogAttachmentContext struct {
	Policy               string                      `json:"policy"`
	Action               string                      `json:"action"`
	EvidencePath         string                      `json:"evidence_path,omitempty"`
	SourceReason         string                      `json:"source_reason,omitempty"`
	EvidenceError        string                      `json:"evidence_error,omitempty"`
	CurrentSelectionUUID *string                     `json:"current_selection_uuid,omitempty"`
	Conflicts            []catalogAttachmentConflict `json:"conflicts"`
	ConflictsTruncated   bool                        `json:"conflicts_truncated"`
}

func importCatalogAttachment(ctx context.Context, row catalogCaptureSource) (*models.CatalogAttachmentRecord, []byte, error) {
	result := &models.CatalogAttachmentRecord{Ordinal: row.Ordinal, PostUUID: row.Post, CaptureUUID: row.Capture}
	view := catalogAttachmentContext{Policy: archive.CapturedAlbumPolicy, Conflicts: []catalogAttachmentConflict{}}
	if row.Capture == nil {
		result.Outcome, result.Reason = "review", "source_evidence_requires_review"
		view.Action, view.SourceReason = "unmapped", row.Reason
	} else if err := mapCatalogAttachment(ctx, result, &view); err != nil {
		return nil, nil, err
	}
	body, err := archive.EncodeSourceJSON(view)
	if err != nil || len(body) > 65536 {
		return nil, nil, models.ErrCatalogSnapshotInvalid
	}
	return result, body, nil
}

func mapCatalogAttachment(ctx context.Context, result *models.CatalogAttachmentRecord, view *catalogAttachmentContext) error {
	evidence, attachments := &SourceEvidenceStore{}, &SourceAttachmentStore{}
	capture, err := evidence.FindCapture(ctx, *result.CaptureUUID)
	if err != nil {
		return err
	}
	if capture == nil || result.PostUUID == nil || capture.PostUUID != *result.PostUUID {
		return models.ErrCatalogSnapshotInvalid
	}
	post, err := evidence.FindPost(ctx, capture.PostUUID)
	if err != nil {
		return err
	}
	if post == nil {
		return models.ErrCatalogSnapshotInvalid
	}
	result.Outcome, result.Reason, view.Action = "unavailable", "missing_supported_attachment_evidence", "unavailable"
	if post.State != "active" {
		result.Reason = "post_forgotten"
		return nil
	}
	raw, err := archive.RestoreCapture(capture.Payload)
	if err != nil {
		return err
	}
	album, err := archive.ExtractCapturedAlbum(raw)
	if err != nil {
		result.Outcome, result.Reason, view.Action = "review", "invalid_attachment_evidence", "invalid"
		view.EvidenceError = err.Error()
		return nil
	}
	if album == nil {
		return nil
	}
	view.EvidencePath = album.EvidencePath
	identified, err := evidence.FindPostByIdentifier(ctx, album.Post)
	if err != nil {
		return err
	}
	if identified == nil || identified.UUID != post.UUID {
		result.Outcome, result.Reason, view.Action = "review", "captured_post_identity_disagrees", "invalid"
		return nil
	}
	input := album.Manifest
	input.CaptureUUID = capture.UUID
	manifest, err := attachments.RecordManifest(ctx, input)
	if errors.Is(err, models.ErrAttachmentManifestReplay) {
		result.Outcome, result.Reason, view.Action = "review", "existing_capture_manifest_disagrees", "conflict"
		return nil
	}
	if err != nil {
		return err
	}
	result.ManifestUUID = &manifest.UUID
	preview, err := attachments.PreviewSelection(ctx, post.UUID, capture.UUID)
	if err != nil {
		return err
	}
	for _, conflict := range preview.Conflicts[:min(len(preview.Conflicts), 128)] {
		view.Conflicts = append(view.Conflicts, catalogAttachmentConflict{Kind: conflict.Kind, Position: conflict.Position})
	}
	view.ConflictsTruncated = len(preview.Conflicts) > len(view.Conflicts)
	if preview.Current != nil {
		view.CurrentSelectionUUID = &preview.Current.Decision.UUID
		result.SelectionUUID = view.CurrentSelectionUUID
	}
	switch {
	case preview.Protected:
		result.Outcome, result.Reason, view.Action = "preserved", "existing_attachment_selection", "protected"
	case len(preview.Conflicts) > 0:
		result.Outcome, result.Reason, view.Action = "review", "attachment_lists_conflict", "conflict"
	case preview.Changed:
		selected, err := attachments.DecideSelection(ctx, models.AttachmentSelectionInput{
			PostUUID: post.UUID, ExpectedPostRevision: preview.PostRevision, CaptureUUID: capture.UUID,
			Mode: "automatic", Origin: "migration", Reason: "Captured catalog attachment evidence",
		})
		if err != nil {
			return err
		}
		result.SelectionUUID, result.SelectionChanged = &selected.Decision.UUID, true
		result.Outcome, result.Reason, view.Action = "mapped", "", "selected"
	default:
		result.Outcome, result.Reason, view.Action = "mapped", "", "unchanged"
	}
	return nil
}

func (s *CatalogAttachmentImportStore) Advance(ctx context.Context, id, expected string, after int64, now time.Time) (*models.CatalogAttachmentImport, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(id) || !archive.ValidSHA256(expected) || after < 0 || !validJobTime(now) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	snapshot, err := (&CatalogSnapshotStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if snapshot == nil || snapshot.ManifestSHA256 != expected || snapshot.State != "received" {
		return nil, models.ErrCatalogSnapshotConflict
	}
	prior, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if (prior == nil && after != 0) || (prior != nil && prior.LastOrdinal != after) {
		return nil, models.ErrCatalogSnapshotConflict
	}
	if prior != nil && prior.State != "running" {
		return prior, nil
	}
	if prior != nil && prior.Policy != archive.CapturedAlbumPolicy {
		return nil, models.ErrCatalogSnapshotConflict
	}
	evidence, err := (&CatalogEvidenceImportStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if evidence == nil || evidence.State == "running" || evidence.ManifestSHA256 != expected {
		return nil, models.ErrCatalogSnapshotConflict
	}
	var body []byte
	if err := dbWrapper.Get(ctx, &body, "SELECT manifest FROM catalog_snapshots WHERE uuid=?", id); err != nil {
		return nil, err
	}
	manifest, err := scrape.PrepareCatalogSnapshot(body, expected)
	if err != nil {
		return nil, err
	}
	complete := snapshotAtomicWrite(ctx)
	stamp := now.UTC().Format(time.RFC3339Nano)
	if prior == nil {
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_attachment_imports(snapshot_uuid,manifest_sha256,policy,state,source_records,created_at,updated_at) VALUES(?,?,?,'running',?,?,?)`, id, expected, archive.CapturedAlbumPolicy, manifest.Captures.Count, stamp, stamp); err != nil {
			return nil, err
		}
		prior, err = s.Find(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	var usedBytes int64
	for count := 0; count < 50 && usedBytes < 16<<20; count++ {
		var row catalogCaptureSource
		err := dbWrapper.Get(ctx, &row, "SELECT ordinal,post_uuid,capture_uuid,reason FROM catalog_evidence_records WHERE snapshot_uuid=? AND ordinal>? AND "+catalogCaptureCandidates+" ORDER BY ordinal LIMIT 1", id, prior.LastOrdinal)
		if errors.Is(err, sql.ErrNoRows) {
			if prior.ProcessedRecords != prior.TotalRecords {
				return nil, models.ErrCatalogSnapshotInvalid
			}
			prior.State = "mapped"
			if prior.ReviewRecords != 0 {
				prior.State = "review"
			}
			break
		}
		if err != nil {
			return nil, err
		}
		if row.Capture != nil {
			size, err := catalogCapturePayloadBytes(ctx, *row.Capture)
			if err != nil {
				return nil, err
			}
			usedBytes += size
		}
		result, view, err := importCatalogAttachment(ctx, row)
		if err != nil {
			return nil, err
		}
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_attachment_records(snapshot_uuid,ordinal,post_uuid,capture_uuid,manifest_uuid,selection_uuid,outcome,reason,selection_changed,context_json) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, row.Ordinal, result.PostUUID, result.CaptureUUID, result.ManifestUUID, result.SelectionUUID, result.Outcome, result.Reason, result.SelectionChanged, string(view)); err != nil {
			return nil, err
		}
		prior.LastOrdinal = row.Ordinal
		prior.ProcessedRecords++
		switch result.Outcome {
		case "mapped":
			prior.MappedRecords++
		case "preserved":
			prior.PreservedRecords++
		case "review":
			prior.ReviewRecords++
		case "unavailable":
			prior.UnavailableRecords++
		}
		if result.SelectionChanged {
			prior.ChangedSelections++
		}
	}
	if _, err := dbWrapper.Exec(ctx, `UPDATE catalog_attachment_imports SET state=?,last_ordinal=?,processed_records=?,mapped_records=?,preserved_records=?,review_records=?,unavailable_records=?,changed_selections=?,updated_at=? WHERE snapshot_uuid=?`, prior.State, prior.LastOrdinal, prior.ProcessedRecords, prior.MappedRecords, prior.PreservedRecords, prior.ReviewRecords, prior.UnavailableRecords, prior.ChangedSelections, stamp, id); err != nil {
		return nil, err
	}
	result, err := s.Find(ctx, id)
	*complete = err == nil
	return result, err
}

func (s *CatalogAttachmentImportStore) Records(ctx context.Context, id string, after int64, limit int) ([]models.CatalogAttachmentRecord, error) {
	if !validSourceRunUUID(id) || after < 0 || limit < 1 || limit > 100 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	columns := strings.Replace(catalogAttachmentColumns, "e.source_key", `CASE WHEN length(CAST(e.source_key AS BLOB))<=8192 THEN e.source_key ELSE '' END AS source_key,length(CAST(e.source_key AS BLOB))>8192 AS key_omitted`, 1)
	result := []models.CatalogAttachmentRecord{}
	err := dbWrapper.Select(ctx, &result, "SELECT "+columns+catalogAttachmentJoins+" WHERE r.snapshot_uuid=? AND r.ordinal>? ORDER BY r.ordinal LIMIT ?", id, after, limit)
	return result, err
}

func (s *CatalogAttachmentImportStore) Record(ctx context.Context, id string, ordinal int64) (*models.CatalogAttachmentRecordDetails, error) {
	if !validSourceRunUUID(id) || ordinal < 1 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	var row struct {
		models.CatalogAttachmentRecord
		Context string `db:"context_json"`
	}
	err := dbWrapper.Get(ctx, &row, "SELECT "+catalogAttachmentColumns+",r.context_json"+catalogAttachmentJoins+" WHERE r.snapshot_uuid=? AND r.ordinal=?", id, ordinal)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &models.CatalogAttachmentRecordDetails{CatalogAttachmentRecord: row.CatalogAttachmentRecord, Context: json.RawMessage(row.Context)}, nil
}
