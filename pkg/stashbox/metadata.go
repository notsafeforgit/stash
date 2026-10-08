package stashbox

import (
	"context"
	"errors"
	"slices"

	"github.com/stashapp/stash/pkg/models"
)

type MetadataRecorder func(context.Context, models.ArchiveEntityKind, int, string, *string, []string) error

func NewMetadataRecorder(repo models.Repository, operation string) MetadataRecorder {
	return func(ctx context.Context, kind models.ArchiveEntityKind, id int, endpoint string, remoteID *string, fields []string) error {
		return RecordMetadataImport(ctx, repo, kind, id, endpoint, remoteID, operation, fields)
	}
}

// Ordinary scrapers have no stash-box endpoint. A provider result, however,
// must never silently fall back to an unattributed write.
func (r MetadataRecorder) Record(ctx context.Context, kind models.ArchiveEntityKind, id int, endpoint string, remoteID *string, fields []string) error {
	if endpoint == "" || len(fields) == 0 {
		return nil
	}
	if r == nil {
		return errors.New("provider metadata recorder is required")
	}
	return r(ctx, kind, id, endpoint, remoteID, fields)
}

// RecordMetadataImport belongs in the same transaction as the accepted edit.
// It reads persisted values after normalization and relationship merging.
func RecordMetadataImport(ctx context.Context, repo models.Repository, kind models.ArchiveEntityKind, id int, endpoint string, remoteID *string, operation string, fields []string) error {
	if len(fields) == 0 {
		return nil
	}
	if remoteID == nil || *remoteID == "" || endpoint == "" {
		return models.ErrProviderMetadataInvalid
	}
	entity, err := repo.ArchiveEntity.FindByLocalID(ctx, kind, id)
	if err != nil {
		return err
	}
	if entity == nil {
		return models.ErrArchiveIdentityConflict
	}
	_, err = repo.ProviderMetadata.Record(ctx, models.ProviderMetadataImportInput{EntityUUID: entity.UUID, ExpectedEntityRevision: entity.Revision,
		Endpoint: endpoint, RemoteID: *remoteID, Operation: operation, Fields: fields})
	return err
}

func selectedMetadataFields(selected map[string]bool) []string {
	ret := []string{}
	for field, include := range selected {
		if include {
			ret = append(ret, field)
		}
	}
	slices.Sort(ret)
	return ret
}

func PerformerImportFields(p models.PerformerPartial, image bool) []string {
	return selectedMetadataFields(map[string]bool{
		"name": p.Name.Set, "disambiguation": p.Disambiguation.Set, "gender": p.Gender.Set,
		"birthdate": p.Birthdate.Set, "death_date": p.DeathDate.Set, "ethnicity": p.Ethnicity.Set,
		"country": p.Country.Set, "eye_color": p.EyeColor.Set, "height": p.Height.Set,
		"measurements": p.Measurements.Set, "fake_tits": p.FakeTits.Set, "penis_length": p.PenisLength.Set,
		"circumcised": p.Circumcised.Set, "career_start": p.CareerStart.Set, "career_end": p.CareerEnd.Set,
		"tattoos": p.Tattoos.Set, "piercings": p.Piercings.Set, "details": p.Details.Set,
		"hair_color": p.HairColor.Set, "weight": p.Weight.Set, "urls": p.URLs != nil,
		"aliases": p.Aliases != nil, "tags": p.TagIDs != nil, "image": image,
	})
}

func PerformerCreateImportFields(source *models.ScrapedPerformer, created *models.Performer, excluded map[string]bool, image bool) []string {
	p := source.ToPartial("", excluded, nil, nil)
	p.Name = models.NewOptionalString(created.Name)
	// Invalid optional source values are ignored on creation. They must not
	// acquire attribution for the resulting empty local fields.
	p.Gender.Set = p.Gender.Set && created.Gender != nil
	p.Circumcised.Set = p.Circumcised.Set && created.Circumcised != nil
	p.Birthdate.Set = p.Birthdate.Set && created.Birthdate != nil
	p.DeathDate.Set = p.DeathDate.Set && created.DeathDate != nil
	p.CareerStart.Set = p.CareerStart.Set && created.CareerStart != nil
	p.CareerEnd.Set = p.CareerEnd.Set && created.CareerEnd != nil
	p.Height.Set = p.Height.Set && created.Height != nil
	p.Weight.Set = p.Weight.Set && created.Weight != nil
	p.PenisLength.Set = p.PenisLength.Set && created.PenisLength != nil
	return PerformerImportFields(p, image)
}

func StudioImportFields(p models.StudioPartial, image bool) []string {
	return selectedMetadataFields(map[string]bool{"name": p.Name.Set, "details": p.Details.Set, "urls": p.URLs != nil,
		"aliases": p.Aliases != nil, "parent": p.ParentID.Set, "tags": p.TagIDs != nil, "image": image})
}

func StudioCreateImportFields(source *models.ScrapedStudio, created *models.CreateStudioInput, excluded map[string]bool, image bool) []string {
	p := source.ToPartial("", "", excluded, nil)
	p.Name = models.NewOptionalString(created.Name)
	p.ParentID.Set = p.ParentID.Set && created.ParentID != nil
	return StudioImportFields(p, image)
}

func TagImportFields(p models.TagPartial) []string {
	return selectedMetadataFields(map[string]bool{"name": p.Name.Set, "description": p.Description.Set, "aliases": p.Aliases != nil, "parents": p.ParentIDs != nil})
}

func TagCreateImportFields(source *models.ScrapedTag, created *models.Tag, excluded map[string]bool) []string {
	p := source.ToPartial("", "", excluded, nil)
	p.Name = models.NewOptionalString(created.Name)
	return TagImportFields(p)
}

func SceneImportFields(p models.ScenePartial, image bool) []string {
	return selectedMetadataFields(map[string]bool{"title": p.Title.Set, "code": p.Code.Set, "details": p.Details.Set,
		"director": p.Director.Set, "date": p.Date.Set, "production_date": p.ProductionDate.Set,
		"urls": p.URLs != nil, "studio": p.StudioID.Set, "performers": p.PerformerIDs != nil,
		"tags": p.TagIDs != nil, "groups": p.GroupIDs != nil, "image": image})
}
