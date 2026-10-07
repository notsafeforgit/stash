package sqlite

import (
	"context"
	"slices"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

const consolidatedPostSelectionsQuery = `SELECT d.* FROM source_post_identities i
JOIN post_attachment_selections s ON s.post_uuid=i.post_uuid
JOIN post_attachment_decisions d ON d.post_uuid=s.post_uuid AND d.uuid=s.decision_uuid
WHERE i.canonical_uuid=? ORDER BY i.post_uuid LIMIT ?`

func consolidatedPostSelectionRows(ctx context.Context, post string) ([]attachmentSelectionRow, error) {
	var rows []attachmentSelectionRow
	if err := dbWrapper.Select(ctx, &rows, consolidatedPostSelectionsQuery, post, maxPostIdentityMembers+1); err != nil {
		return nil, err
	}
	if len(rows) > maxPostIdentityMembers {
		return nil, models.ErrSourcePostIdentityLimit
	}
	return rows, nil
}

// A complete merge may combine compatible selected lists or explicitly select
// one original capture. The primary capture keeps its original owner. Additional
// manifests must come from the current choices included in the merge review.
// There is no independent receipt here: the encompassing merge owns exact retry
// and synchronizes the gallery after all attachment/media choices have settled.
func publishConsolidatedPostSelection(ctx context.Context, input models.AttachmentSelectionInput, consolidation string, expected, manifests []string) (*models.AttachmentSelection, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if (input.Origin != "review" && input.Origin != "migration") ||
		(input.Mode != "automatic" && input.Mode != "pinned" && input.Mode != "disabled") ||
		!validAccountText(input.Reason, 4096, true) {
		return nil, models.ErrAttachmentSelectionConflict
	}
	valid, err := currentPostConsolidation(ctx, input.PostUUID, consolidation)
	if err != nil {
		return nil, err
	}
	if !valid || len(expected) > maxPostIdentityMembers || len(manifests) > archive.MaxSelectionManifests {
		return nil, models.ErrAttachmentSelectionConflict
	}
	post, err := currentSourcePost(ctx, input.PostUUID)
	if err != nil {
		return nil, err
	}
	if post == nil || post.UUID != input.PostUUID || post.Revision != input.ExpectedPostRevision {
		return nil, models.ErrAttachmentSelectionConflict
	}
	if post.State != "active" {
		return nil, models.ErrSourcePostForgotten
	}
	heads, err := consolidatedPostSelectionRows(ctx, input.PostUUID)
	if err != nil {
		return nil, err
	}
	expected = slices.Clone(expected)
	slices.Sort(expected)
	current := make([]string, 0, len(heads))
	for _, head := range heads {
		current = append(current, head.UUID)
	}
	slices.Sort(current)
	if !slices.Equal(current, expected) {
		return nil, models.ErrAttachmentSelectionConflict
	}
	selected, err := prepareConsolidatedPostSelection(ctx, input, heads, manifests)
	if err != nil {
		return nil, err
	}
	finish := postConsolidationCommitGuard(ctx, models.ErrAttachmentSelectionConflict)
	for _, head := range heads {
		if _, err := dbWrapper.Exec(ctx, "DELETE FROM post_attachment_selections WHERE post_uuid=? AND decision_uuid=?", head.PostUUID, head.UUID); err != nil {
			return nil, err
		}
	}
	ret, err := (&SourceAttachmentStore{}).publishSelection(ctx, post, input, selected)
	if err == nil {
		finish()
	}
	return ret, err
}

func prepareConsolidatedPostSelection(ctx context.Context, input models.AttachmentSelectionInput, heads []attachmentSelectionRow, manifests []string) (*models.AttachmentSelection, error) {
	decision := models.AttachmentSelectionDecision{PostUUID: input.PostUUID, Mode: input.Mode, Origin: input.Origin, Reason: input.Reason}
	if input.Mode == "disabled" {
		if input.CaptureUUID != "" || len(manifests) != 0 {
			return nil, models.ErrAttachmentSelectionConflict
		}
		return &models.AttachmentSelection{Decision: decision}, nil
	}
	capture, primary, err := selectionCapture(ctx, input.PostUUID, input.CaptureUUID)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(manifests, primary) || (input.Mode == "pinned" && len(manifests) != 1) {
		return nil, models.ErrAttachmentSelectionConflict
	}
	allowed := map[string]bool{primary: true}
	for _, head := range heads {
		ids, err := selectionManifestIDs(ctx, head)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			allowed[id] = true
		}
		if len(allowed) > archive.MaxSelectionManifests+1 {
			return nil, models.ErrSourcePostIdentityLimit
		}
	}
	manifests = slices.Clone(manifests)
	slices.Sort(manifests)
	for i, id := range manifests {
		if !allowed[id] || (i > 0 && manifests[i-1] == id) {
			return nil, models.ErrAttachmentSelectionConflict
		}
	}
	sources, err := loadSelectionManifests(ctx, input.PostUUID, manifests)
	if err != nil {
		return nil, err
	}
	merged, err := mergeSelection(manifests, sources)
	if err != nil {
		return nil, err
	}
	if len(merged.Conflicts) != 0 {
		return nil, models.ErrAttachmentManifestConflict
	}
	decision.CaptureUUID, decision.ManifestUUIDs = &capture, manifests
	return selectionFromMerge(decision, merged.Manifest, sources), nil
}
