package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func (w *catalogMediaWork) asset(ctx context.Context, row catalogEvidenceRow, record *scrape.CatalogSnapshotRecord, result *models.CatalogMediaRecord) error {
	v := record.Values
	ref, refOK := v["asset_id"].(string)
	created, createdOK := v["created_at"].(string)
	algorithm, algorithmOK := catalogMediaText(v["digest_algorithm"])
	digest, digestOK := catalogMediaText(v["digest"])
	size, sizeOK := catalogMediaInt(v["byte_size"])
	if !refOK || !createdOK || !algorithmOK || !digestOK || !sizeOK {
		return catalogMediaOutcome(result, "review", "invalid_source_asset")
	}
	details, err := w.details(row)
	if err != nil {
		return err
	}
	claim, err := (&SourceFileStore{}).RecordContentClaim(ctx, models.SourceContentClaim{
		UUID: w.id(row, "claim"), CollectionUUID: w.snapshot.CollectionUUID, CollectionRevision: w.progress.CollectionRevision,
		ReferenceNamespace: "legacy:catalog:" + w.snapshot.SourceUUID + ":" + w.snapshot.CatalogID, ReferenceValue: ref,
		DigestAlgorithm: algorithm, Digest: digest, Size: size, SourceCreatedAt: created, Origin: "migration", ObservedAt: w.stamp, Details: details,
	})
	if errors.Is(err, models.ErrSourceFileEvidenceInvalid) {
		return catalogMediaOutcome(result, "review", "invalid_source_asset")
	}
	if err != nil {
		return err
	}
	result.ClaimUUID = &claim.UUID
	return nil
}

func (w *catalogMediaWork) fileObservation(row catalogEvidenceRow, record *scrape.CatalogSnapshotRecord) (*models.SourceFileObservation, error) {
	v := record.Values
	path, pathOK := v["relpath"].(string)
	state, stateOK := v["state"].(string)
	first, firstOK := v["first_observed"].(string)
	role, roleOK := v["role"].(string)
	if _, exists := v["role"]; !exists {
		role, roleOK = "local", true
	}
	size, sizeOK := catalogMediaInt(v["byte_size"])
	modified, modifiedOK := catalogMediaInt(v["mtime_ns"])
	survivor, survivorOK := catalogMediaText(v["survivor_relpath"])
	if !pathOK || !stateOK || !firstOK || !roleOK || !sizeOK || !modifiedOK || !survivorOK ||
		!archive.ValidRootRelativePath(path, false) || (survivor != nil && !archive.ValidRootRelativePath(*survivor, false)) ||
		!sourceFileTimestamp(first) || (size != nil && *size < 0) ||
		(state != "present" && state != "pending" && state != "missing" && state != "deduplicated") ||
		(role != "local" && role != "converted-source" && role != "source-media-reference") {
		return nil, models.ErrSourceFileEvidenceInvalid
	}
	details, err := w.details(row)
	if err != nil {
		return nil, err
	}
	return &models.SourceFileObservation{
		UUID: w.id(row, "observation"), CollectionUUID: w.snapshot.CollectionUUID, CollectionRevision: w.progress.CollectionRevision,
		RootUUID: w.progress.RootUUID, RootRevision: w.progress.RootRevision, RelativePath: path, State: state, Role: role,
		Size: size, ModifiedAtNS: modified, SourceFirstObserved: first, SurvivorPath: survivor, Origin: "migration", ObservedAt: w.stamp, Details: details,
	}, nil
}

type catalogFileCandidate struct {
	UUID       string `db:"uuid" json:"file_uuid"`
	Generation int64  `db:"generation" json:"generation"`
}

func catalogFilesAt(ctx context.Context, path string) ([]catalogFileCandidate, error) {
	ret := []catalogFileCandidate{}
	err := dbWrapper.Select(ctx, &ret, `SELECT e.uuid,f.generation FROM folders p JOIN files f ON f.parent_folder_id=p.id
 JOIN archive_entities e ON e.file_id=f.id WHERE p.path=? COLLATE BINARY AND f.basename=? COLLATE BINARY
 AND e.kind='file' AND e.state='active' ORDER BY e.uuid LIMIT 2`, filepath.Dir(path), filepath.Base(path))
	return ret, err
}

func (w *catalogMediaWork) chooseFile(ctx context.Context, observation *models.SourceFileObservation, claim *models.SourceContentClaim, view map[string]any) (*catalogFileCandidate, string, string, error) {
	if observation.State == "pending" {
		return nil, "", "source_file_pending", nil
	}
	paths := [][2]string{}
	if observation.State == "present" {
		paths = append(paths, [2]string{"exact-path", observation.RelativePath})
	}
	if observation.SurvivorPath != nil {
		paths = append(paths, [2]string{"survivor-path", *observation.SurvivorPath})
	}
	for _, path := range paths {
		candidates, err := catalogFilesAt(ctx, filepath.Join(w.progress.LibraryRootPath, filepath.FromSlash(path[1])))
		if err != nil {
			return nil, "", "", err
		}
		if len(candidates) == 0 {
			continue
		}
		view["file_candidates"] = candidates
		if len(candidates) > 1 {
			return nil, "", "ambiguous_library_path", nil
		}
		return &candidates[0], path[0], "", nil
	}
	if claim != nil && claim.DigestAlgorithm != nil && *claim.DigestAlgorithm == "sha256" && claim.Digest != nil && archive.ValidSHA256(*claim.Digest) {
		content, err := (&FileContentStore{}).FindBySHA256(ctx, *claim.Digest)
		if err != nil {
			return nil, "", "", err
		}
		if content != nil {
			locations, err := (&FileContentStore{}).Locations(ctx, content.UUID, "", 2)
			if err != nil {
				return nil, "", "", err
			}
			if len(locations) > 1 {
				return nil, "", "ambiguous_verified_content_locations", nil
			}
			if len(locations) == 1 {
				return &catalogFileCandidate{locations[0].FileUUID, locations[0].Generation}, "verified-content", "", nil
			}
		}
	}
	return nil, "", "library_file_unavailable", nil
}

func catalogZipObservation(observation *models.SourceFileObservation, root, zipPath string) error {
	relative, err := filepath.Rel(root, zipPath)
	if err != nil || !archive.ValidRootRelativePath(filepath.ToSlash(relative), false) {
		return models.ErrSourceFileEvidenceInvalid
	}
	archivePath := filepath.ToSlash(relative)
	prefix := archivePath + "/"
	if !strings.HasPrefix(observation.RelativePath, prefix) {
		return models.ErrSourceFileEvidenceInvalid
	}
	if observation.SurvivorPath != nil && !strings.HasPrefix(*observation.SurvivorPath, prefix) {
		return models.ErrSourceFileEvidenceInvalid
	}
	observation.ArchivePath, observation.RelativePath = &archivePath, strings.TrimPrefix(observation.RelativePath, prefix)
	if observation.SurvivorPath != nil {
		survivor := strings.TrimPrefix(*observation.SurvivorPath, prefix)
		observation.SurvivorPath = &survivor
	}
	return nil
}

func (w *catalogMediaWork) matchInput(ctx context.Context, row catalogEvidenceRow, observation *models.SourceFileObservation, candidate *catalogFileCandidate, basis string) (*models.SourceFileMatch, error) {
	_, file, err := sourceMatchedFile(ctx, candidate.UUID, candidate.Generation)
	if err != nil {
		return nil, err
	}
	details, err := w.details(row)
	if err != nil {
		return nil, err
	}
	input := &models.SourceFileMatch{UUID: w.id(row, "match"), ObservationUUID: observation.UUID, FileUUID: candidate.UUID, Generation: candidate.Generation, Basis: basis, Origin: "migration", Details: details}
	if basis == "exact-path" || basis == "survivor-path" {
		input.LibraryRootPath = w.progress.LibraryRootPath
	}
	if file.Base().ZipFileID != nil {
		zipID, err := (&ArchiveEntityStore{}).FindByLocalID(ctx, models.ArchiveFile, int(*file.Base().ZipFileID))
		if err != nil {
			return nil, err
		}
		if zipID == nil {
			return nil, models.ErrSourceFileEvidenceInvalid
		}
		zips, err := NewFileStore().Find(ctx, *file.Base().ZipFileID)
		if err != nil {
			return nil, err
		}
		zip := zips[0].Base()
		input.ArchiveFileUUID, input.ArchiveGeneration = &zipID.UUID, &zip.Generation
		if input.LibraryRootPath != "" {
			if err := catalogZipObservation(observation, w.progress.LibraryRootPath, zip.Path); err != nil {
				return nil, err
			}
		}
	}
	return input, nil
}

func (w *catalogMediaWork) file(ctx context.Context, row catalogEvidenceRow, record *scrape.CatalogSnapshotRecord, result *models.CatalogMediaRecord, view map[string]any) error {
	observation, err := w.fileObservation(row, record)
	if errors.Is(err, models.ErrSourceFileEvidenceInvalid) {
		return catalogMediaOutcome(result, "review", "invalid_source_file")
	}
	if err != nil {
		return err
	}
	assetID, ok := catalogMediaText(record.Values["asset_id"])
	if !ok {
		return catalogMediaOutcome(result, "review", "invalid_source_asset_reference")
	}
	var claim *models.SourceContentClaim
	if assetID != nil {
		mapped, err := w.mappedRow(ctx, "assets", *assetID)
		if err != nil {
			return err
		}
		if mapped == nil || mapped.ClaimUUID == nil {
			result.Outcome, result.Reason = "review", "source_asset_requires_review"
		} else {
			claim, err = (&SourceFileStore{}).ContentClaim(ctx, *mapped.ClaimUUID)
			if err != nil {
				return err
			}
			if claim == nil {
				return models.ErrCatalogSnapshotInvalid
			}
			observation.ContentClaimUUID, result.ClaimUUID = &claim.UUID, &claim.UUID
		}
	}
	var input *models.SourceFileMatch
	if result.Outcome != "review" {
		candidate, basis, reason, err := w.chooseFile(ctx, observation, claim, view)
		if err != nil {
			return err
		}
		switch {
		case candidate != nil:
			input, err = w.matchInput(ctx, row, observation, candidate, basis)
			if errors.Is(err, models.ErrSourceFileEvidenceInvalid) {
				result.Outcome, result.Reason = "review", "archive_member_scope_conflicts"
			} else if err != nil {
				return err
			}
		case strings.HasPrefix(reason, "ambiguous_"):
			result.Outcome, result.Reason = "review", reason
		default:
			result.Outcome, result.Reason = "unavailable", reason
		}
	}
	stored, err := (&SourceFileStore{}).RecordObservation(ctx, *observation)
	if err != nil {
		return err
	}
	result.ObservationUUID = &stored.UUID
	if input != nil {
		matched, err := (&SourceFileStore{}).RecordMatch(ctx, *input)
		if errors.Is(err, models.ErrSourceFileEvidenceInvalid) || errors.Is(err, models.ErrFileContentConflict) || errors.Is(err, models.ErrFileGenerationConflict) {
			return catalogMediaOutcome(result, "review", "library_file_metadata_conflicts")
		}
		if err != nil {
			return err
		}
		result.MatchUUID = &matched.UUID
	}
	return nil
}
