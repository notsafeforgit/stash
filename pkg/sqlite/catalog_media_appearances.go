package sqlite

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func (w *catalogMediaWork) appearanceFile(ctx context.Context, record *scrape.CatalogSnapshotRecord, claim string) (*models.CatalogMediaRecord, string, error) {
	path, pathOK := catalogMediaText(record.Values["source_relpath"])
	if !pathOK {
		return nil, "invalid_appearance_path", nil
	}
	if path != nil {
		mapped, err := w.mappedRow(ctx, "files", *path)
		if err != nil {
			return nil, "", err
		}
		if mapped != nil {
			if mapped.ClaimUUID == nil || *mapped.ClaimUUID != claim {
				return nil, "appearance_asset_path_disagrees", nil
			}
			if mapped.ObservationUUID == nil {
				return nil, "source_file_requires_review", nil
			}
			return mapped, "", nil
		}
	}
	var candidates []models.CatalogMediaRecord
	err := dbWrapper.Select(ctx, &candidates, "SELECT "+catalogMediaColumns+catalogMediaJoins+" WHERE r.snapshot_uuid=? AND r.claim_uuid=? AND r.observation_uuid IS NOT NULL AND e.source_table='files' ORDER BY r.ordinal LIMIT 2", w.snapshot.UUID, claim)
	if err != nil {
		return nil, "", err
	}
	if len(candidates) > 1 {
		return nil, "ambiguous_asset_locations", nil
	}
	if len(candidates) == 0 {
		return nil, "asset_has_no_file_observation", nil
	}
	return &candidates[0], "", nil
}

func (w *catalogMediaWork) appearance(ctx context.Context, row catalogEvidenceRow, record *scrape.CatalogSnapshotRecord, result *models.CatalogMediaRecord, view map[string]any) error {
	v := record.Values
	postKey, postOK := v["post_key"].(string)
	assetKey, assetOK := v["asset_id"].(string)
	if !postOK || !assetOK {
		return catalogMediaOutcome(result, "review", "invalid_appearance_identity")
	}
	post, reason, err := w.nativePost(ctx, postKey)
	if err != nil {
		return err
	}
	if post != nil {
		result.PostUUID = &post.UUID
	}
	if reason != "" {
		return catalogMediaOutcome(result, "review", reason)
	}
	asset, err := w.mappedRow(ctx, "assets", assetKey)
	if err != nil {
		return err
	}
	if asset == nil || asset.ClaimUUID == nil {
		return catalogMediaOutcome(result, "review", "source_asset_requires_review")
	}
	result.ClaimUUID = asset.ClaimUUID
	file, reason, err := w.appearanceFile(ctx, record, *asset.ClaimUUID)
	if err != nil {
		return err
	}
	if reason != "" {
		return catalogMediaOutcome(result, "review", reason)
	}
	result.ObservationUUID, result.MatchUUID = file.ObservationUUID, file.MatchUUID
	// Legacy positions remain recorded download counters. They do not establish
	// attachment identity, capture membership or source album order.
	details, err := archive.EncodeSourceJSON(map[string]any{
		"snapshot_uuid": w.snapshot.UUID, "source_ordinal": row.Ordinal, "source_sha256": row.SHA256,
		"attachment_key": v["attachment_key"], "source_media_id": v["source_media_id"], "position": v["position"], "source_relpath": v["source_relpath"],
	})
	if err != nil || len(details) > 65536 {
		return catalogMediaOutcome(result, "review", "appearance_details_exceed_native_limit")
	}
	postFile, err := (&SourceFileStore{}).RecordPostEvidence(ctx, models.SourcePostFileEvidence{
		SourcePostEvidence: models.SourcePostEvidence{UUID: w.id(row, "post-file"), PostUUID: post.UUID, Origin: "migration", Basis: "catalog-appearance", ObservedAt: w.stamp, Details: details},
		ObservationUUID:    *file.ObservationUUID,
	})
	if err != nil {
		return err
	}
	result.PostFileUUID = &postFile.UUID
	if file.Outcome == "review" {
		return catalogMediaOutcome(result, "review", "source_file_requires_review")
	}
	if file.MatchUUID == nil {
		return catalogMediaOutcome(result, "unavailable", "library_file_unavailable")
	}
	match, err := sourceFileFind[models.SourceFileMatch](ctx, *file.MatchUUID, "SELECT "+sourceFileMatchColumns+" FROM source_file_matches")
	if err != nil {
		return err
	}
	if match == nil {
		return models.ErrCatalogSnapshotInvalid
	}
	if err := (&SourceFileStore{}).validateMatch(ctx, match); err != nil {
		if errors.Is(err, models.ErrFileGenerationConflict) || errors.Is(err, models.ErrFileContentConflict) || errors.Is(err, models.ErrSourceFileEvidenceInvalid) {
			return catalogMediaOutcome(result, "review", "matched_file_changed")
		}
		return err
	}
	owners, err := (&FileContentStore{}).Owners(ctx, match.FileUUID)
	if err != nil {
		return err
	}
	if len(owners) == 0 {
		return catalogMediaOutcome(result, "unavailable", "library_media_unavailable")
	}
	if len(owners) > 1 {
		ids := []string{}
		for _, owner := range owners {
			ids = append(ids, owner.UUID)
		}
		view["media_candidates"] = ids
		return catalogMediaOutcome(result, "review", "ambiguous_library_media")
	}
	mediaDetails, err := archive.EncodeSourceJSON(map[string]any{"source_post_file_evidence_uuid": postFile.UUID, "source_file_match_uuid": match.UUID})
	if err != nil {
		return err
	}
	media, err := (&SourceAttachmentStore{}).RecordMediaEvidence(ctx, models.SourceMediaEvidence{
		UUID: w.id(row, "media"), PostUUID: post.UUID, MediaUUID: owners[0].UUID, FileUUID: &match.FileUUID, Basis: "legacy", Details: json.RawMessage(mediaDetails),
	})
	if err != nil {
		return err
	}
	result.MediaEvidenceUUID = &media.UUID
	return nil
}
