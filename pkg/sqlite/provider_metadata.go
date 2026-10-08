package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type ProviderMetadataStore struct{ db *Database }

const providerMetadataColumns = `sequence,uuid,entity_uuid,original_entity_uuid,entity_kind,entity_revision,endpoint,remote_id,operation,CAST(values_json AS BLOB) AS values_json,signature,created_at`

func providerMetadataSignature(r models.ProviderMetadataImport) (string, error) {
	if !validSourceRunUUID(r.UUID) || !validSourceRunUUID(r.EntityUUID) || !validSourceRunUUID(r.OriginalEntityUUID) || r.EntityRevision < 1 ||
		!validAccountText(r.Endpoint, 4096, false) || !validAccountText(r.RemoteID, 1024, false) || strings.TrimSpace(r.RemoteID) != r.RemoteID ||
		!slices.Contains([]string{"review", "identify", "batch"}, r.Operation) || !validJobTime(r.CreatedAt) {
		return "", models.ErrProviderMetadataInvalid
	}
	u, err := url.Parse(r.Endpoint)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || strings.TrimSpace(r.Endpoint) != r.Endpoint {
		return "", models.ErrProviderMetadataInvalid
	}
	normal, err := metadataFieldJSON(r.Values)
	if err != nil || !bytes.Equal(normal, r.Values) {
		return "", models.ErrProviderMetadataInvalid
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(normal, &values); err != nil || len(values) == 0 {
		return "", models.ErrProviderMetadataInvalid
	}
	for field := range values {
		if !slices.Contains(models.ProviderMetadataFields(r.EntityKind), field) {
			return "", models.ErrProviderMetadataInvalid
		}
	}
	// The original identity stays in the hashed document when its foreign key
	// follows UUID adoption. Source aliases preserve that original identity.
	proof, err := json.Marshal([]any{"stash-provider-metadata-v1", r.UUID, r.OriginalEntityUUID, r.EntityKind, r.EntityRevision, r.Endpoint, r.RemoteID, r.Operation, normal, r.CreatedAt.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(proof)), nil
}

func (s *ProviderMetadataStore) Record(ctx context.Context, input models.ProviderMetadataImportInput) (*models.ProviderMetadataImport, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return models.ErrProviderMetadataInvalid
		}
		return nil
	})
	entity, err := (&ArchiveEntityStore{}).Find(ctx, input.EntityUUID)
	if err != nil {
		return nil, err
	}
	if entity == nil || entity.LocalID == nil || entity.State != models.ArchiveEntityActive || entity.Revision != input.ExpectedEntityRevision {
		return nil, models.ErrArchiveIdentityConflict
	}
	if len(input.Fields) == 0 || len(input.Fields) > len(models.ProviderMetadataFields(entity.Kind)) {
		return nil, models.ErrProviderMetadataInvalid
	}
	values := make(map[string]json.RawMessage, len(input.Fields))
	for _, field := range input.Fields {
		if !slices.Contains(models.ProviderMetadataFields(entity.Kind), field) || values[field] != nil {
			return nil, models.ErrProviderMetadataInvalid
		}
		value, err := s.value(ctx, entity, field)
		if err != nil {
			return nil, err
		}
		values[field] = value
	}
	body, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	body, err = metadataFieldJSON(body)
	if err != nil {
		return nil, err
	}
	r := &models.ProviderMetadataImport{UUID: uuid.NewString(), EntityUUID: entity.UUID, OriginalEntityUUID: entity.UUID,
		EntityKind: entity.Kind, EntityRevision: entity.Revision, Endpoint: input.Endpoint, RemoteID: input.RemoteID, Operation: input.Operation, Values: body, CreatedAt: time.Now().UTC().Truncate(time.Second)}
	r.Signature, err = providerMetadataSignature(*r)
	if err != nil {
		return nil, err
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO provider_metadata_imports(uuid,entity_uuid,original_entity_uuid,entity_kind,entity_revision,endpoint,remote_id,operation,values_json,signature,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		r.UUID, r.EntityUUID, r.OriginalEntityUUID, r.EntityKind, r.EntityRevision, r.Endpoint, r.RemoteID, r.Operation, string(r.Values), r.Signature, r.CreatedAt)
	if err != nil {
		return nil, err
	}
	err = dbWrapper.Get(ctx, r, "SELECT "+providerMetadataColumns+" FROM provider_metadata_imports WHERE uuid=?", r.UUID)
	complete = err == nil
	return r, err
}

func (s *ProviderMetadataStore) History(ctx context.Context, entityUUID string, after, limit int) ([]models.ProviderMetadataImport, error) {
	id, err := archiveUUID(entityUUID)
	if err != nil || after < 0 {
		return nil, models.ErrProviderMetadataInvalid
	}
	limit, err = sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	ret := []models.ProviderMetadataImport{}
	err = dbWrapper.Select(ctx, &ret, "SELECT "+providerMetadataColumns+" FROM provider_metadata_imports WHERE entity_uuid=? AND sequence>? ORDER BY sequence LIMIT ?", id, after, limit)
	if err != nil {
		return nil, err
	}
	for _, row := range ret {
		signature, err := providerMetadataSignature(row)
		if err != nil || signature != row.Signature {
			return nil, models.ErrSourcePayloadCorrupt
		}
	}
	return ret, err
}

func providerMetadataReference(ctx context.Context, kind models.ArchiveEntityKind, ids []int) (json.RawMessage, error) {
	values := []string{}
	for _, id := range ids {
		entity, err := (&ArchiveEntityStore{}).FindByLocalID(ctx, kind, id)
		if err != nil {
			return nil, err
		}
		if entity == nil || entity.State != models.ArchiveEntityActive {
			return nil, models.ErrArchiveIdentityConflict
		}
		values = append(values, entity.UUID)
	}
	return json.Marshal(values)
}

func (s *ProviderMetadataStore) value(ctx context.Context, entity *models.ArchiveEntity, field string) (json.RawMessage, error) {
	id := *entity.LocalID
	if field == "image" {
		var value []byte
		var err error
		switch entity.Kind {
		case models.ArchiveScene:
			value, err = s.db.Scene.GetCover(ctx, id)
		case models.ArchivePerformer:
			value, err = s.db.Performer.GetImage(ctx, id)
		case models.ArchiveStudio:
			value, err = s.db.Studio.GetImage(ctx, id)
		case models.ArchiveTag:
			value, err = s.db.Tag.GetImage(ctx, id)
		}
		if err != nil {
			return nil, err
		}
		if len(value) == 0 {
			return json.RawMessage(`null`), nil
		}
		// Artwork bytes remain in the normal blob store. A historical import
		// records their digest, not a second base64 copy or a temporary URL.
		return json.Marshal(map[string]any{"sha256": fmt.Sprintf("%x", sha256.Sum256(value)), "bytes": len(value)})
	}
	if entity.Kind == models.ArchiveScene {
		state, err := (&MetadataFieldStore{}).State(ctx, entity.UUID, field)
		if err != nil {
			return nil, err
		}
		return state.Value, nil
	}
	var scalar any
	var value any
	var err error
	switch entity.Kind {
	case models.ArchivePerformer:
		store := s.db.Performer
		switch field {
		case "urls":
			value, err = store.GetURLs(ctx, id)
		case "aliases":
			value, err = store.GetPerformerAliases(ctx, id)
		case "tags":
			var ids []int
			ids, err = store.GetTagIDs(ctx, id)
			if err == nil {
				return providerMetadataReference(ctx, models.ArchiveTag, ids)
			}
		default:
			var performer *models.Performer
			performer, err = store.Find(ctx, id)
			if err == nil && performer != nil {
				dates := map[string]*models.Date{"birthdate": performer.Birthdate, "death_date": performer.DeathDate, "career_start": performer.CareerStart, "career_end": performer.CareerEnd}
				if date, ok := dates[field]; ok {
					if date == nil {
						return json.RawMessage(`null`), nil
					}
					return json.Marshal(date.String())
				}
			}
			scalar = performer
		}
	case models.ArchiveStudio:
		store := s.db.Studio
		switch field {
		case "urls":
			value, err = store.GetURLs(ctx, id)
		case "aliases":
			value, err = store.GetAliases(ctx, id)
		case "tags":
			var ids []int
			ids, err = store.GetTagIDs(ctx, id)
			if err == nil {
				return providerMetadataReference(ctx, models.ArchiveTag, ids)
			}
		default:
			var studio *models.Studio
			studio, err = store.Find(ctx, id)
			if err == nil && studio != nil && field == "parent" {
				if studio.ParentID == nil {
					return json.RawMessage(`null`), nil
				}
				parent, err := (&ArchiveEntityStore{}).FindByLocalID(ctx, models.ArchiveStudio, *studio.ParentID)
				if err != nil {
					return nil, err
				}
				if parent == nil {
					return nil, models.ErrArchiveIdentityConflict
				}
				return json.Marshal(parent.UUID)
			}
			scalar = studio
		}
	case models.ArchiveTag:
		store := s.db.Tag
		switch field {
		case "aliases":
			value, err = store.GetAliases(ctx, id)
		case "parents":
			var ids []int
			ids, err = store.GetParentIDs(ctx, id)
			if err == nil {
				return providerMetadataReference(ctx, models.ArchiveTag, ids)
			}
		default:
			scalar, err = store.Find(ctx, id)
		}
	}
	if err != nil {
		return nil, err
	}
	if scalar != nil {
		body, err := json.Marshal(scalar)
		if err != nil {
			return nil, err
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(body, &fields); err != nil {
			return nil, err
		}
		if v, ok := fields[field]; ok {
			return v, nil
		}
		return nil, models.ErrProviderMetadataInvalid
	}
	return json.Marshal(value)
}
