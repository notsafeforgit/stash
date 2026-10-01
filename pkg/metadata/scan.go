package metadata

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// ApplyScan shares the intake evaluator without inventing a source post or
// account. The caller has registered the file/media in this same transaction.
func (s Service) ApplyScan(ctx context.Context, kind models.ArchiveEntityKind, localID int, file models.File, created bool) (*Preview, error) {
	if file.Base().ZipFileID != nil {
		return nil, nil // A ZIP member needs its own reviewed collection binding.
	}
	matches, err := s.Repo.MetadataPolicy.MatchScan(ctx, file.Base().Path)
	if err != nil || len(matches) == 0 {
		return nil, err
	}
	if len(matches) != 1 {
		return finishPreview(&Preview{State: "ambiguous_directory", Changes: []Change{}})
	}
	match := matches[0]
	opened, err := archive.OpenMediaRootFile(*match.Root, match.RelativePath)
	if err != nil {
		return nil, err
	}
	defer opened.Close()
	observed, err := opened.Stat()
	if err != nil {
		return nil, err
	}
	current, err := os.Stat(file.Base().Path)
	if err != nil || !os.SameFile(observed, current) {
		return nil, models.ErrMetadataPolicyConflict
	}
	entity, err := s.Repo.ArchiveEntity.FindByLocalID(ctx, kind, localID)
	if err != nil {
		return nil, err
	}
	identity, err := s.Repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveFile, int(file.Base().ID))
	if err != nil {
		return nil, err
	}
	if entity == nil || identity == nil {
		return nil, models.ErrArchiveIdentityConflict
	}
	intakeID := uuid.NewSHA1(uuid.MustParse(identity.UUID), []byte(fmt.Sprintf("scan-policy\x00%s\x00%d\x00%s", match.Collection.UUID, match.Collection.Revision, entity.UUID))).String()
	if _, err := s.Repo.SourceCollection.RecordMediaIntake(ctx, models.CollectionMediaIntake{
		UUID: intakeID, CollectionUUID: match.Collection.UUID, CollectionRevision: match.Collection.Revision, MediaUUID: entity.UUID, Origin: "scan", Reason: "Explicit directory policy",
	}); err != nil {
		return nil, err
	}
	return s.Apply(ctx, Input{CollectionUUID: match.Collection.UUID, CollectionRevision: match.Collection.Revision,
		PolicyRevision: match.Policy.Revision, EntityUUID: entity.UUID, RelativePath: match.RelativePath, Created: created}, "")
}
