package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ProviderMetadataImport records the values accepted from one stash-box result.
// Values are the resulting library values, including any locally selected merge
// behavior. This is an import receipt, not a claim that subsequent edits still
// come from this provider, or that the provider signed these values.
type ProviderMetadataImport struct {
	Sequence           int               `json:"sequence" db:"sequence"`
	UUID               string            `json:"uuid" db:"uuid"`
	EntityUUID         string            `json:"entity_uuid" db:"entity_uuid"`
	OriginalEntityUUID string            `json:"original_entity_uuid" db:"original_entity_uuid"`
	EntityKind         ArchiveEntityKind `json:"entity_kind" db:"entity_kind"`
	EntityRevision     int               `json:"entity_revision" db:"entity_revision"`
	Endpoint           string            `json:"endpoint" db:"endpoint"`
	RemoteID           string            `json:"remote_id" db:"remote_id"`
	Operation          string            `json:"operation" db:"operation"`
	Values             json.RawMessage   `json:"values" db:"values_json"`
	Signature          string            `json:"signature" db:"signature"`
	CreatedAt          time.Time         `json:"created_at" db:"created_at"`
}

type ProviderMetadataImportInput struct {
	EntityUUID             string
	ExpectedEntityRevision int
	Endpoint               string
	RemoteID               string
	Operation              string // review, identify, batch
	// Fields use the library's metadata names. Record reads their actual values
	// in the caller's write transaction; callers cannot supply invented values.
	Fields []string
}

var ErrProviderMetadataInvalid = errors.New("invalid provider metadata import")

type ProviderMetadataReaderWriter interface {
	Record(context.Context, ProviderMetadataImportInput) (*ProviderMetadataImport, error)
	// History addresses a particular identity, including retired identities.
	// UUID adoption follows the canonical UUID; merges retain the source history.
	History(context.Context, string, int, int) ([]ProviderMetadataImport, error)
}

func ProviderMetadataFields(kind ArchiveEntityKind) []string {
	switch kind {
	case ArchiveScene:
		return []string{"title", "code", "details", "date", "production_date", "director", "urls", "studio", "performers", "tags", "groups", "image"}
	case ArchivePerformer:
		return []string{"name", "disambiguation", "gender", "birthdate", "death_date", "ethnicity", "country", "eye_color", "height", "measurements", "fake_tits", "penis_length", "circumcised", "career_start", "career_end", "tattoos", "piercings", "details", "hair_color", "weight", "urls", "aliases", "tags", "image"}
	case ArchiveStudio:
		return []string{"name", "details", "urls", "aliases", "parent", "tags", "image"}
	case ArchiveTag:
		return []string{"name", "description", "aliases", "image"}
	default:
		return nil
	}
}
