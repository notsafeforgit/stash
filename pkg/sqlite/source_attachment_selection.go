package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type attachmentSelectionRow struct {
	UUID          string         `db:"uuid"`
	PostUUID      string         `db:"post_uuid"`
	Revision      int            `db:"revision"`
	Mode          string         `db:"mode"`
	Origin        string         `db:"origin"`
	Reason        string         `db:"reason"`
	CaptureUUID   sql.NullString `db:"capture_uuid"`
	ManifestCount int            `db:"manifest_count"`
	Signature     string         `db:"signature"`
	CreatedAt     Timestamp      `db:"created_at"`
}

func (s *SourceAttachmentStore) SelectedPosts(ctx context.Context, after string, limit int) ([]models.SelectedSourcePost, error) {
	if after != "" {
		id, err := archiveUUID(after)
		if err != nil || id != after {
			return nil, errors.New("invalid selected-post cursor")
		}
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		PostUUID      string `db:"post_uuid"`
		PostState     string `db:"post_state"`
		SelectionUUID string `db:"selection_uuid"`
		Mode          string `db:"mode"`
	}
	if err := dbWrapper.Select(ctx, &rows, `SELECT s.post_uuid,p.state AS post_state,s.decision_uuid AS selection_uuid,d.mode
FROM post_attachment_selections s JOIN source_posts p ON p.uuid=s.post_uuid
JOIN post_attachment_decisions d ON d.uuid=s.decision_uuid
WHERE s.post_uuid>? ORDER BY s.post_uuid LIMIT ?`, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.SelectedSourcePost, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, models.SelectedSourcePost{PostUUID: row.PostUUID, PostState: row.PostState, SelectionUUID: row.SelectionUUID, Mode: row.Mode})
	}
	return ret, nil
}

func (r attachmentSelectionRow) resolve(ids []string) models.AttachmentSelectionDecision {
	ret := models.AttachmentSelectionDecision{UUID: r.UUID, PostUUID: r.PostUUID, Revision: r.Revision, Mode: r.Mode,
		Origin: r.Origin, Reason: r.Reason, ManifestUUIDs: ids, CreatedAt: r.CreatedAt.Timestamp}
	if r.CaptureUUID.Valid {
		ret.CaptureUUID = &r.CaptureUUID.String
	}
	return ret
}

func selectionRow(ctx context.Context, post string) (*attachmentSelectionRow, error) {
	var row attachmentSelectionRow
	if err := dbWrapper.Get(ctx, &row, `SELECT d.* FROM post_attachment_selections s
JOIN post_attachment_decisions d ON d.post_uuid = s.post_uuid AND d.uuid = s.decision_uuid WHERE s.post_uuid = ?`, post); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func selectionManifestIDs(ctx context.Context, row attachmentSelectionRow) ([]string, error) {
	var ids []string
	if err := dbWrapper.Select(ctx, &ids, "SELECT manifest_uuid FROM post_attachment_decision_manifests WHERE decision_uuid = ? ORDER BY manifest_uuid LIMIT ?", row.UUID, archive.MaxSelectionManifests+1); err != nil {
		return nil, err
	}
	if len(ids) != row.ManifestCount || len(ids) > archive.MaxSelectionManifests {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return ids, nil
}

type selectionManifest struct {
	input   models.SourceAttachmentManifestInput
	entries []models.SourceAttachmentManifestEntry
}

// Load one bounded set with two indexed queries, not a query per capture or a
// scan of all posts. Header counts bound the entry query before allocating it.
func loadSelectionManifests(ctx context.Context, post string, ids []string) (map[string]selectionManifest, error) {
	ret := make(map[string]selectionManifest)
	if len(ids) == 0 {
		return ret, nil
	}
	if len(ids) > archive.MaxSelectionManifests+1 {
		return nil, errors.New("too many attachment source lists")
	}
	args := make([]interface{}, 0, len(ids)+1)
	args = append(args, post)
	for _, id := range ids {
		args = append(args, id)
	}
	var headers []struct {
		attachmentManifestRow
		InScope bool `db:"in_scope"`
	}
	if err := dbWrapper.Select(ctx, &headers, selectionManifestHeadersQuery+getInBinding(len(ids)), args...); err != nil {
		return nil, err
	}
	if len(headers) != len(ids) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	total := 0
	for _, row := range headers {
		if !row.InScope {
			return nil, models.ErrAttachmentSelectionConflict
		}
		total += row.EntryCount
		if total > archive.MaxSelectionSourceEntries {
			return nil, errors.New("attachment selection exceeds its source-entry budget")
		}
	}
	var rows []struct {
		sourceAttachmentRow
		ManifestUUID string `db:"manifest_uuid"`
		Position     int    `db:"position"`
		MediaKind    string `db:"media_kind"`
	}
	query := `SELECT a.*, e.manifest_uuid, e.position, e.media_kind FROM source_attachment_entries e
JOIN source_attachments a ON a.uuid = e.attachment_uuid AND a.post_uuid = e.post_uuid
WHERE e.manifest_uuid IN ` + getInBinding(len(ids)) + ` ORDER BY e.manifest_uuid, e.position LIMIT ?`
	args = append(args[1:], archive.MaxSelectionSourceEntries+1)
	if err := dbWrapper.Select(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	if len(rows) != total {
		return nil, models.ErrSourcePayloadCorrupt
	}
	for _, row := range rows {
		item := ret[row.ManifestUUID]
		item.entries = append(item.entries, models.SourceAttachmentManifestEntry{Position: row.Position, MediaKind: row.MediaKind, Attachment: *row.resolve()})
		ret[row.ManifestUUID] = item
	}
	for _, row := range headers {
		item := ret[row.UUID]
		var err error
		item.input, err = validatedAttachmentManifest(row.resolve(), item.entries)
		if err != nil {
			return nil, err
		}
		ret[row.UUID] = item
	}
	return ret, nil
}

func mergeSelection(ids []string, sources map[string]selectionManifest) (*archive.AttachmentManifestMerge, error) {
	if len(ids) == 0 {
		return &archive.AttachmentManifestMerge{}, nil
	}
	inputs := make([]models.SourceAttachmentManifestInput, 0, len(ids))
	for _, id := range ids {
		inputs = append(inputs, sources[id].input)
	}
	return archive.MergeAttachmentManifests(inputs)
}

func selectionSignature(mode string, ids []string, input models.SourceAttachmentManifestInput) (string, error) {
	ids = slices.Clone(ids)
	slices.Sort(ids)
	if ids == nil {
		ids = []string{}
	}
	return sourceSignature("stash-attachment-selection-v1", []interface{}{mode, ids, archive.AttachmentManifestSignatureInput(input)})
}

func selectionFromMerge(decision models.AttachmentSelectionDecision, merged models.SourceAttachmentManifestInput, sources map[string]selectionManifest) *models.AttachmentSelection {
	attachments := make(map[models.SourcePostIdentifier]models.SourceAttachment)
	// A preview also loads candidate lists that may not contribute to this
	// selection. They must not change the attachment behind an existing choice.
	// Consolidated members can retain distinct UUIDs for the same qualified key;
	// choose a stable representative using only the selected evidence.
	for _, id := range decision.ManifestUUIDs {
		for _, entry := range sources[id].entries {
			previous, exists := attachments[entry.Attachment.Reference]
			if !exists || entry.Attachment.UUID < previous.UUID {
				attachments[entry.Attachment.Reference] = entry.Attachment
			}
		}
	}
	ret := &models.AttachmentSelection{Decision: decision, Complete: merged.Complete, DeclaredAlbum: merged.DeclaredAlbum, ExpectedCount: merged.ExpectedCount,
		Entries: make([]models.SourceAttachmentManifestEntry, 0, len(merged.Entries))}
	for _, entry := range merged.Entries {
		ret.Entries = append(ret.Entries, models.SourceAttachmentManifestEntry{Position: entry.Position, MediaKind: entry.MediaKind, Attachment: attachments[entry.Reference]})
	}
	return ret
}

func readSelection(row *attachmentSelectionRow, ids []string, sources map[string]selectionManifest) (*models.AttachmentSelection, error) {
	if row == nil {
		return nil, nil
	}
	merged, err := mergeSelection(ids, sources)
	if err != nil {
		return nil, err
	}
	if len(merged.Conflicts) > 0 {
		return nil, models.ErrSourcePayloadCorrupt
	}
	signature, err := selectionSignature(row.Mode, ids, merged.Manifest)
	if err != nil {
		return nil, err
	}
	if signature != row.Signature {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return selectionFromMerge(row.resolve(ids), merged.Manifest, sources), nil
}

func (s *SourceAttachmentStore) Selection(ctx context.Context, value string) (*models.AttachmentSelection, error) {
	post, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	row, err := selectionRow(ctx, post)
	if err != nil || row == nil {
		return nil, err
	}
	ids, err := selectionManifestIDs(ctx, *row)
	if err != nil {
		return nil, err
	}
	sources, err := loadSelectionManifests(ctx, post, ids)
	if err != nil {
		return nil, err
	}
	return readSelection(row, ids, sources)
}

func selectionCapture(ctx context.Context, post, value string) (string, string, error) {
	capture, err := archiveUUID(value)
	if err != nil {
		return "", "", err
	}
	var manifest string
	if err := dbWrapper.Get(ctx, &manifest, selectionCaptureQuery, post, capture); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", models.ErrAttachmentSelectionConflict
		}
		return "", "", err
	}
	return capture, manifest, nil
}

func (s *SourceAttachmentStore) PreviewSelection(ctx context.Context, postID, captureID string) (*models.AttachmentSelectionPreview, error) {
	post, err := (&SourceEvidenceStore{}).FindPost(ctx, postID)
	if err != nil {
		return nil, err
	}
	if post == nil {
		return nil, models.ErrAttachmentSelectionConflict
	}
	if post.State != "active" {
		return nil, models.ErrSourcePostForgotten
	}
	capture, candidate, err := selectionCapture(ctx, post.UUID, captureID)
	if err != nil {
		return nil, err
	}
	row, err := selectionRow(ctx, post.UUID)
	if err != nil {
		return nil, err
	}
	var currentIDs []string
	if row != nil {
		currentIDs, err = selectionManifestIDs(ctx, *row)
		if err != nil {
			return nil, err
		}
	}
	ids := slices.Clone(currentIDs)
	if !slices.Contains(ids, candidate) {
		ids = append(ids, candidate)
	}
	sources, err := loadSelectionManifests(ctx, post.UUID, ids)
	if err != nil {
		return nil, err
	}
	current, err := readSelection(row, currentIDs, sources)
	if err != nil {
		return nil, err
	}
	ret := &models.AttachmentSelectionPreview{PostUUID: post.UUID, PostRevision: post.Revision, CaptureUUID: capture, Current: current,
		Protected: row != nil && row.Mode != "automatic"}
	merged, err := mergeSelection(ids, sources)
	if err != nil {
		return nil, err
	}
	ret.Conflicts = merged.Conflicts
	if len(ret.Conflicts) > 0 {
		return ret, nil
	}
	selected := make([]string, 0, len(merged.Contributors))
	for _, index := range merged.Contributors {
		selected = append(selected, ids[index])
	}
	slices.Sort(selected)
	ret.Proposed = selectionFromMerge(models.AttachmentSelectionDecision{PostUUID: post.UUID, Mode: "automatic", Origin: "ingest", CaptureUUID: &capture, ManifestUUIDs: selected}, merged.Manifest, sources)
	signature, err := selectionSignature("automatic", selected, merged.Manifest)
	if err != nil {
		return nil, err
	}
	ret.Changed = row == nil || row.Signature != signature
	return ret, nil
}

func (s *SourceAttachmentStore) prepareSelectionChoice(ctx context.Context, input models.AttachmentSelectionInput) (*models.SourcePost, *models.AttachmentSelection, bool, error) {
	if (input.Mode != "automatic" && input.Mode != "pinned" && input.Mode != "disabled") ||
		(input.Origin != "ingest" && input.Origin != "review" && input.Origin != "migration") || !validAccountText(input.Reason, 4096, true) ||
		(input.Origin == "ingest" && input.Mode != "automatic") {
		return nil, nil, false, errors.New("invalid attachment selection choice")
	}
	post, err := (&SourceEvidenceStore{}).FindPost(ctx, input.PostUUID)
	if err != nil {
		return nil, nil, false, err
	}
	if post == nil || post.Revision != input.ExpectedPostRevision {
		return nil, nil, false, models.ErrAttachmentSelectionConflict
	}
	if post.State != "active" {
		return nil, nil, false, models.ErrSourcePostForgotten
	}
	var selected *models.AttachmentSelection
	switch {
	case input.Origin == "ingest" || (input.Origin == "migration" && input.Mode == "automatic"):
		preview, err := s.PreviewSelection(ctx, post.UUID, input.CaptureUUID)
		if err != nil {
			return nil, nil, false, err
		}
		if preview.Protected {
			return nil, nil, false, models.ErrAttachmentSelectionProtected
		}
		if len(preview.Conflicts) > 0 {
			return nil, nil, false, models.ErrAttachmentManifestConflict
		}
		if !preview.Changed {
			return post, preview.Current, true, nil
		}
		selected = preview.Proposed
	case input.Mode == "disabled":
		if input.CaptureUUID != "" {
			return nil, nil, false, errors.New("disabled attachment selection cannot select a capture")
		}
		selected = &models.AttachmentSelection{}
	default:
		capture, manifest, err := selectionCapture(ctx, post.UUID, input.CaptureUUID)
		if err != nil {
			return nil, nil, false, err
		}
		sources, err := loadSelectionManifests(ctx, post.UUID, []string{manifest})
		if err != nil {
			return nil, nil, false, err
		}
		selected = selectionFromMerge(models.AttachmentSelectionDecision{CaptureUUID: &capture, ManifestUUIDs: []string{manifest}}, sources[manifest].input, sources)
	}
	selected.Decision.PostUUID, selected.Decision.Mode, selected.Decision.Origin, selected.Decision.Reason = post.UUID, input.Mode, input.Origin, input.Reason
	return post, selected, false, nil
}

func selectedAttachmentSignature(selected *models.AttachmentSelection) (string, error) {
	merged := models.SourceAttachmentManifestInput{Complete: selected.Complete, DeclaredAlbum: selected.DeclaredAlbum, ExpectedCount: selected.ExpectedCount}
	for _, entry := range selected.Entries {
		merged.Entries = append(merged.Entries, models.SourceAttachmentEntry{Position: entry.Position, MediaKind: entry.MediaKind, Reference: entry.Attachment.Reference})
	}
	return selectionSignature(selected.Decision.Mode, selected.Decision.ManifestUUIDs, merged)
}

func (s *SourceAttachmentStore) DecideSelection(ctx context.Context, input models.AttachmentSelectionInput) (*models.AttachmentSelection, error) {
	post, selected, unchanged, err := s.prepareSelectionChoice(ctx, input)
	if err != nil || unchanged {
		return selected, err
	}
	signature, err := selectedAttachmentSignature(selected)
	if err != nil {
		return nil, err
	}
	result, err := dbWrapper.Exec(ctx, "UPDATE source_posts SET revision = revision + 1 WHERE uuid = ? AND revision = ?", post.UUID, input.ExpectedPostRevision)
	if err != nil {
		return nil, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, models.ErrAttachmentSelectionConflict
	}
	id := uuid.NewString()
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO post_attachment_decisions(uuid, post_uuid, revision, mode, origin, reason, capture_uuid, manifest_count, signature)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, post.UUID, post.Revision+1, input.Mode, input.Origin, input.Reason, selected.Decision.CaptureUUID, len(selected.Decision.ManifestUUIDs), signature); err != nil {
		return nil, err
	}
	for _, manifest := range selected.Decision.ManifestUUIDs {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO post_attachment_decision_manifests(post_uuid, decision_uuid, manifest_uuid) VALUES (?, ?, ?)", post.UUID, id, manifest); err != nil {
			return nil, err
		}
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO post_attachment_selections(post_uuid, decision_uuid) VALUES (?, ?)
ON CONFLICT(post_uuid) DO UPDATE SET decision_uuid = excluded.decision_uuid`, post.UUID, id); err != nil {
		return nil, err
	}
	return s.Selection(ctx, post.UUID)
}

func (s *SourceAttachmentStore) SelectionHistory(ctx context.Context, value string, after, limit int) ([]models.AttachmentSelectionDecision, error) {
	post, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	limit, err = sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	if after < 0 {
		return nil, errors.New("invalid attachment selection cursor")
	}
	var rows []attachmentSelectionRow
	if err := dbWrapper.Select(ctx, &rows, "SELECT * FROM post_attachment_decisions WHERE post_uuid = ? AND revision > ? ORDER BY revision LIMIT ?", post, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.AttachmentSelectionDecision, 0, len(rows))
	for _, row := range rows {
		ids, err := selectionManifestIDs(ctx, row)
		if err != nil {
			return nil, err
		}
		ret = append(ret, row.resolve(ids))
	}
	return ret, nil
}
