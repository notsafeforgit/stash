package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type SourceFileStore struct{}

const sourceContentColumns = `uuid,collection_uuid,collection_revision,reference_namespace,reference_value,digest_algorithm,digest,size,source_created_at,origin,observed_at,CAST(details AS BLOB) AS details,created_at`
const sourceFileObservationColumns = `uuid,content_claim_uuid,collection_uuid,collection_revision,root_uuid,root_revision,relative_path,archive_path,state,role,size,modified_at_ns,source_first_observed,survivor_path,origin,observed_at,CAST(details AS BLOB) AS details,created_at`
const sourceFileMatchColumns = `uuid,observation_uuid,file_uuid,generation,archive_file_uuid,archive_generation,library_root_path,basis,origin,CAST(details AS BLOB) AS details,created_at`

func sourceFileIDs(values ...*string) error {
	for _, value := range values {
		if value == nil {
			continue
		}
		id, err := archiveUUID(*value)
		if err != nil {
			return models.ErrSourceFileEvidenceInvalid
		}
		*value = id
	}
	return nil
}

func sourceFileOrigin(value string) bool {
	return value == "migration" || value == "ingest" || value == "review" || value == "scan"
}

func sourceFileTimestamp(value string) bool {
	if value == "" {
		return true
	}
	_, err := time.Parse(time.RFC3339Nano, value)
	return len(value) <= 64 && err == nil
}

func sourceFileDetails(value *json.RawMessage) error {
	body, err := accountEvidenceJSON(*value)
	if err != nil {
		return models.ErrSourceFileEvidenceInvalid
	}
	*value = json.RawMessage(body)
	return nil
}

func sourceFileFind[T any](ctx context.Context, id, query string) (*T, error) {
	if err := sourceFileIDs(&id); err != nil {
		return nil, err
	}
	var row T
	if err := dbWrapper.Get(ctx, &row, query+" WHERE uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func (s *SourceFileStore) ContentClaim(ctx context.Context, id string) (*models.SourceContentClaim, error) {
	return sourceFileFind[models.SourceContentClaim](ctx, id, "SELECT "+sourceContentColumns+" FROM source_content_claims")
}

func (s *SourceFileStore) RecordContentClaim(ctx context.Context, input models.SourceContentClaim) (*models.SourceContentClaim, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if err := sourceFileIDs(&input.UUID, &input.CollectionUUID); err != nil {
		return nil, err
	}
	if input.CollectionRevision < 1 || !sourceFileOrigin(input.Origin) || !validJobTime(input.ObservedAt) ||
		!validAccountText(input.ReferenceNamespace, 128, false) || !validAccountText(input.ReferenceValue, 4096, false) ||
		!sourceFileTimestamp(input.SourceCreatedAt) || (input.Size != nil && *input.Size < 0) ||
		(input.DigestAlgorithm == nil) != (input.Digest == nil) {
		return nil, models.ErrSourceFileEvidenceInvalid
	}
	if input.Digest != nil && (!validAccountText(*input.DigestAlgorithm, 128, false) || !validAccountText(*input.Digest, 4096, false)) {
		return nil, models.ErrSourceFileEvidenceInvalid
	}
	input.ObservedAt = input.ObservedAt.UTC()
	if err := sourceFileDetails(&input.Details); err != nil {
		return nil, err
	}
	prior, err := s.ContentClaim(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		input.CreatedAt = prior.CreatedAt
		if !reflect.DeepEqual(input, *prior) {
			return nil, models.ErrSourceFileEvidenceReplay
		}
		return prior, nil
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO source_content_claims(uuid,collection_uuid,collection_revision,reference_namespace,reference_value,digest_algorithm,digest,size,source_created_at,origin,observed_at,details)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, input.UUID, input.CollectionUUID, input.CollectionRevision, input.ReferenceNamespace, input.ReferenceValue,
		input.DigestAlgorithm, input.Digest, input.Size, input.SourceCreatedAt, input.Origin, input.ObservedAt, string(input.Details))
	if err != nil {
		return nil, err
	}
	return s.ContentClaim(ctx, input.UUID)
}

func (s *SourceFileStore) Observation(ctx context.Context, id string) (*models.SourceFileObservation, error) {
	return sourceFileFind[models.SourceFileObservation](ctx, id, "SELECT "+sourceFileObservationColumns+" FROM source_file_observations")
}

func (s *SourceFileStore) RecordObservation(ctx context.Context, input models.SourceFileObservation) (*models.SourceFileObservation, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if err := sourceFileIDs(&input.UUID, &input.CollectionUUID, &input.RootUUID, input.ContentClaimUUID); err != nil {
		return nil, err
	}
	if input.CollectionRevision < 1 || input.RootRevision < 1 || !sourceFileOrigin(input.Origin) || !validJobTime(input.ObservedAt) ||
		!archive.ValidRootRelativePath(input.RelativePath, false) || !sourceFileTimestamp(input.SourceFirstObserved) ||
		(input.ArchivePath != nil && !archive.ValidRootRelativePath(*input.ArchivePath, false)) ||
		(input.SurvivorPath != nil && !archive.ValidRootRelativePath(*input.SurvivorPath, false)) || (input.Size != nil && *input.Size < 0) {
		return nil, models.ErrSourceFileEvidenceInvalid
	}
	if input.State != "present" && input.State != "missing" && input.State != "pending" && input.State != "deduplicated" {
		return nil, models.ErrSourceFileEvidenceInvalid
	}
	if input.Role != "local" && input.Role != "converted-source" && input.Role != "source-media-reference" {
		return nil, models.ErrSourceFileEvidenceInvalid
	}
	input.ObservedAt = input.ObservedAt.UTC()
	if err := sourceFileDetails(&input.Details); err != nil {
		return nil, err
	}
	prior, err := s.Observation(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		input.CreatedAt = prior.CreatedAt
		if !reflect.DeepEqual(input, *prior) {
			return nil, models.ErrSourceFileEvidenceReplay
		}
		return prior, nil
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO source_file_observations(uuid,content_claim_uuid,collection_uuid,collection_revision,root_uuid,root_revision,relative_path,archive_path,state,role,size,modified_at_ns,source_first_observed,survivor_path,origin,observed_at,details)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, input.UUID, input.ContentClaimUUID, input.CollectionUUID, input.CollectionRevision, input.RootUUID, input.RootRevision,
		input.RelativePath, input.ArchivePath, input.State, input.Role, input.Size, input.ModifiedAtNS, input.SourceFirstObserved, input.SurvivorPath,
		input.Origin, input.ObservedAt, string(input.Details))
	if err != nil {
		return nil, err
	}
	return s.Observation(ctx, input.UUID)
}

func sourceFileObservationPage(ctx context.Context, condition string, args []any, after string, limit int) ([]models.SourceFileObservation, error) {
	after, limit, err := sourceDefinitionPage(after, limit)
	if err != nil {
		return nil, err
	}
	args = append(args, after, limit)
	ret := []models.SourceFileObservation{}
	err = dbWrapper.Select(ctx, &ret, "SELECT "+sourceFileObservationColumns+" FROM source_file_observations WHERE "+condition+" AND uuid>? ORDER BY uuid LIMIT ?", args...)
	return ret, err
}

func (s *SourceFileStore) ClaimObservations(ctx context.Context, claim, after string, limit int) ([]models.SourceFileObservation, error) {
	if err := sourceFileIDs(&claim); err != nil {
		return nil, err
	}
	return sourceFileObservationPage(ctx, "content_claim_uuid=?", []any{claim}, after, limit)
}

func (s *SourceFileStore) LocationObservations(ctx context.Context, root string, zipPath *string, relative, after string, limit int) ([]models.SourceFileObservation, error) {
	if err := sourceFileIDs(&root); err != nil {
		return nil, err
	}
	if !archive.ValidRootRelativePath(relative, false) || (zipPath != nil && !archive.ValidRootRelativePath(*zipPath, false)) {
		return nil, models.ErrSourceFileEvidenceInvalid
	}
	return sourceFileObservationPage(ctx, "root_uuid=? AND archive_path IS ? AND relative_path=?", []any{root, zipPath, relative}, after, limit)
}

func sourceMatchedFile(ctx context.Context, id string, generation int64) (*models.ArchiveEntity, models.File, error) {
	entity, err := (&ArchiveEntityStore{}).Resolve(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if entity == nil || entity.Kind != models.ArchiveFile || entity.State != models.ArchiveEntityActive || generation < 1 {
		return nil, nil, models.ErrFileGenerationConflict
	}
	files, err := NewFileStore().Find(ctx, models.FileID(*entity.LocalID))
	if err != nil {
		return nil, nil, err
	}
	if len(files) != 1 || files[0].Base().Generation != generation {
		return nil, nil, models.ErrFileGenerationConflict
	}
	return entity, files[0], nil
}

func (s *SourceFileStore) validateMatch(ctx context.Context, input *models.SourceFileMatch) error {
	observation, err := s.Observation(ctx, input.ObservationUUID)
	if err != nil {
		return err
	}
	if observation == nil {
		return models.ErrSourceFileEvidenceInvalid
	}
	entity, file, err := sourceMatchedFile(ctx, input.FileUUID, input.Generation)
	if err != nil {
		return err
	}
	input.FileUUID = entity.UUID
	var zipFile models.File
	if (file.Base().ZipFileID == nil) != (input.ArchiveFileUUID == nil) || (input.ArchiveFileUUID == nil) != (input.ArchiveGeneration == nil) {
		return models.ErrSourceFileEvidenceInvalid
	}
	if input.ArchiveFileUUID != nil {
		var zipEntity *models.ArchiveEntity
		zipEntity, zipFile, err = sourceMatchedFile(ctx, *input.ArchiveFileUUID, *input.ArchiveGeneration)
		if err != nil {
			return err
		}
		if zipFile.Base().ZipFileID != nil || zipFile.Base().ID != *file.Base().ZipFileID {
			return models.ErrSourceFileEvidenceInvalid
		}
		input.ArchiveFileUUID = &zipEntity.UUID
	}
	if input.Basis == "review" {
		return nil
	}
	var claim *models.SourceContentClaim
	if observation.ContentClaimUUID != nil {
		claim, err = s.ContentClaim(ctx, *observation.ContentClaimUUID)
		if err != nil {
			return err
		}
		if claim == nil {
			return models.ErrSourceFileEvidenceInvalid
		}
	}
	if input.Basis == "verified-content" {
		return sourceFileContentMatch(ctx, entity.UUID, input.Generation, claim)
	}
	return sourceFilePathMatch(input, observation, claim, file, zipFile)
}

func sourceFileContentMatch(ctx context.Context, file string, generation int64, claim *models.SourceContentClaim) error {
	if claim == nil || claim.DigestAlgorithm == nil || *claim.DigestAlgorithm != "sha256" || claim.Digest == nil || !archive.ValidSHA256(*claim.Digest) {
		return models.ErrSourceFileEvidenceInvalid
	}
	proof, err := (&FileContentStore{}).Current(ctx, file)
	if err != nil {
		return err
	}
	if proof == nil || proof.Generation != generation || proof.Content.SHA256 != *claim.Digest || (claim.Size != nil && proof.Content.Size != *claim.Size) {
		return models.ErrFileContentConflict
	}
	return nil
}

func sourceFilePathMatch(input *models.SourceFileMatch, observation *models.SourceFileObservation, claim *models.SourceContentClaim, file, zipFile models.File) error {
	if !validAccountText(input.LibraryRootPath, 4096, false) || !filepath.IsAbs(input.LibraryRootPath) || filepath.Clean(input.LibraryRootPath) != input.LibraryRootPath {
		return models.ErrSourceFileEvidenceInvalid
	}
	path, size := observation.RelativePath, observation.Size
	if input.Basis == "survivor-path" {
		if observation.SurvivorPath == nil {
			return models.ErrSourceFileEvidenceInvalid
		}
		path = *observation.SurvivorPath
		// A converted input's size need not equal its surviving output's size.
		// The shared asset claim, when present, describes the survivor.
		size = nil
		if claim != nil {
			size = claim.Size
		}
	}
	if size != nil && *size != file.Base().Size {
		return models.ErrSourceFileEvidenceInvalid
	}
	// FileStore persists Timestamp values at whole-second precision. Preserve
	// the source's nanoseconds, but compare only the precision the library kept.
	if input.Basis == "exact-path" && observation.ModifiedAtNS != nil && time.Unix(0, *observation.ModifiedAtNS).Unix() != file.Base().ModTime.Unix() {
		return models.ErrSourceFileEvidenceInvalid
	}
	prefix := input.LibraryRootPath
	if (zipFile == nil) != (observation.ArchivePath == nil) {
		return models.ErrSourceFileEvidenceInvalid
	}
	if zipFile != nil {
		prefix = filepath.Join(prefix, filepath.FromSlash(*observation.ArchivePath))
		if zipFile.Base().Path != prefix {
			return models.ErrSourceFileEvidenceInvalid
		}
	}
	if file.Base().Path != filepath.Join(prefix, filepath.FromSlash(path)) {
		return models.ErrSourceFileEvidenceInvalid
	}
	return nil
}

func (s *SourceFileStore) RecordMatch(ctx context.Context, input models.SourceFileMatch) (*models.SourceFileMatch, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if err := sourceFileIDs(&input.UUID, &input.ObservationUUID, &input.FileUUID, input.ArchiveFileUUID); err != nil {
		return nil, err
	}
	if input.Generation < 1 || !sourceFileOrigin(input.Origin) || (input.ArchiveFileUUID == nil) != (input.ArchiveGeneration == nil) ||
		(input.ArchiveGeneration != nil && *input.ArchiveGeneration < 1) {
		return nil, models.ErrSourceFileEvidenceInvalid
	}
	pathBasis := input.Basis == "exact-path" || input.Basis == "survivor-path"
	if (!pathBasis && input.Basis != "verified-content" && input.Basis != "review") || pathBasis != (input.LibraryRootPath != "") {
		return nil, models.ErrSourceFileEvidenceInvalid
	}
	if err := sourceFileDetails(&input.Details); err != nil {
		return nil, err
	}
	query := "SELECT " + sourceFileMatchColumns + " FROM source_file_matches"
	prior, err := sourceFileFind[models.SourceFileMatch](ctx, input.UUID, query)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		fileEqual, err := sameArchiveTarget(ctx, &input.FileUUID, &prior.FileUUID)
		if err != nil {
			return nil, err
		}
		archiveEqual, err := sameArchiveTarget(ctx, input.ArchiveFileUUID, prior.ArchiveFileUUID)
		if err != nil {
			return nil, err
		}
		input.FileUUID, input.ArchiveFileUUID, input.CreatedAt = prior.FileUUID, prior.ArchiveFileUUID, prior.CreatedAt
		if !fileEqual || !archiveEqual || !reflect.DeepEqual(input, *prior) {
			return nil, models.ErrSourceFileEvidenceReplay
		}
		return prior, nil
	}
	if err := s.validateMatch(ctx, &input); err != nil {
		return nil, err
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO source_file_matches(uuid,observation_uuid,file_uuid,generation,archive_file_uuid,archive_generation,library_root_path,basis,origin,details)
 VALUES(?,?,?,?,?,?,?,?,?,?)`, input.UUID, input.ObservationUUID, input.FileUUID, input.Generation, input.ArchiveFileUUID, input.ArchiveGeneration,
		input.LibraryRootPath, input.Basis, input.Origin, string(input.Details))
	if err != nil {
		return nil, err
	}
	return sourceFileFind[models.SourceFileMatch](ctx, input.UUID, query)
}

func (s *SourceFileStore) Matches(ctx context.Context, observation, after string, limit int) ([]models.SourceFileMatch, error) {
	if err := sourceFileIDs(&observation); err != nil {
		return nil, err
	}
	after, limit, err := sourceDefinitionPage(after, limit)
	if err != nil {
		return nil, err
	}
	ret := []models.SourceFileMatch{}
	err = dbWrapper.Select(ctx, &ret, "SELECT "+sourceFileMatchColumns+" FROM source_file_matches WHERE observation_uuid=? AND uuid>? ORDER BY uuid LIMIT ?", observation, after, limit)
	return ret, err
}

type sourcePostFileRow struct {
	postLinkRow
	ObservationUUID string `db:"observation_uuid"`
}

func (r sourcePostFileRow) resolve() *models.SourcePostFileEvidence {
	return &models.SourcePostFileEvidence{SourcePostEvidence: r.evidence(), ObservationUUID: r.ObservationUUID}
}

func (s *SourceFileStore) RecordPostEvidence(ctx context.Context, input models.SourcePostFileEvidence) (*models.SourcePostFileEvidence, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if err := normalizePostLink(&input.SourcePostEvidence); err != nil {
		return nil, err
	}
	if err := sourceFileIDs(&input.ObservationUUID); err != nil {
		return nil, err
	}
	prior, err := sourceFileFind[sourcePostFileRow](ctx, input.UUID, "SELECT * FROM source_post_file_evidence")
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if !reflect.DeepEqual(input, *prior.resolve()) {
			return nil, models.ErrSourceFileEvidenceReplay
		}
		return prior.resolve(), nil
	}
	if _, err := activePostLink(ctx, input.PostUUID); err != nil {
		return nil, err
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO source_post_file_evidence(uuid,post_uuid,observation_uuid,origin,basis,observed_at,details)
 VALUES(?,?,?,?,?,?,?)`, input.UUID, input.PostUUID, input.ObservationUUID, input.Origin, input.Basis, input.ObservedAt, string(input.Details))
	if err != nil {
		return nil, err
	}
	return &input, nil
}

func (s *SourceFileStore) PostEvidence(ctx context.Context, post, after string, limit int) ([]models.SourcePostFileEvidence, error) {
	if err := sourceFileIDs(&post); err != nil {
		return nil, err
	}
	after, limit, err := sourceDefinitionPage(after, limit)
	if err != nil {
		return nil, err
	}
	var rows []sourcePostFileRow
	if err := dbWrapper.Select(ctx, &rows, "SELECT * FROM source_post_file_evidence WHERE post_uuid=? AND uuid>? ORDER BY uuid LIMIT ?", post, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.SourcePostFileEvidence, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, *row.resolve())
	}
	return ret, nil
}
