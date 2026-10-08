package stashbox

import (
	"context"
	"slices"

	"github.com/stashapp/stash/pkg/models"
)

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
