package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
)

const maxMetadataCollectionItems = 4096

func canonicalMetadataJSONValue(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n, nil
		}
		n, err := v.Float64()
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, errors.New("metadata number is outside the native range")
		}
		return canonicalMetadataJSONValue(n)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, errors.New("metadata number is outside the native range")
		}
		// Above the exact integer range, a shortest double representation can
		// look like a different valid int64. Keep an exponent so replay parses
		// it as a double rather than changing its value or rejecting it.
		if v >= 1<<53 || v <= -(1<<53) {
			return json.Number(strconv.FormatFloat(v, 'e', -1, 64)), nil
		}
		return v, nil
	case map[string]interface{}:
		for key, item := range v {
			normal, err := canonicalMetadataJSONValue(item)
			if err != nil {
				return nil, err
			}
			v[key] = normal
		}
	case []interface{}:
		for i, item := range v {
			normal, err := canonicalMetadataJSONValue(item)
			if err != nil {
				return nil, err
			}
			v[i] = normal
		}
	}
	return value, nil
}

type metadataReference struct {
	UUID       string `json:"uuid" db:"target_uuid"`
	SceneIndex *int   `json:"scene_index" db:"scene_index"`
}

type metadataCollectionValue struct {
	value      json.RawMessage
	references []metadataReference
	targets    map[string]*models.ArchiveEntity
	urls       []string
	custom     map[string]interface{}
}

type metadataRelation struct{ table, owner, foreign string }

func metadataCollectionDefinition(def models.MetadataFieldDefinition) bool {
	return def.ReferenceKind != "" || def.Type == "urls" || def.Type == "custom_fields"
}

func metadataReferenceKind(field string) models.ArchiveEntityKind {
	return map[string]models.ArchiveEntityKind{"studio": models.ArchiveStudio, "performers": models.ArchivePerformer,
		"tags": models.ArchiveTag, "groups": models.ArchiveGroup}[field]
}

func metadataRelationFor(kind models.ArchiveEntityKind, field string) (metadataRelation, error) {
	table, owner, err := metadataFieldTable(kind)
	if err != nil {
		return metadataRelation{}, err
	}
	switch field {
	case "performers":
		return metadataRelation{"performers_" + table, owner, "performer_id"}, nil
	case "tags":
		return metadataRelation{table + "_tags", owner, "tag_id"}, nil
	case "groups":
		if kind == models.ArchiveScene {
			return metadataRelation{"groups_scenes", owner, "group_id"}, nil
		}
	case "studio":
		return metadataRelation{table, "id", "studio_id"}, nil
	case "urls":
		return metadataRelation{string(kind) + "_urls", owner, "url"}, nil
	case "custom_fields":
		return metadataRelation{string(kind) + "_custom_fields", owner, "field"}, nil
	}
	return metadataRelation{}, errors.New("invalid metadata collection target")
}

func readMetadataCollection(ctx context.Context, entityUUID, field string) (json.RawMessage, error) {
	var row struct {
		Value string `db:"value_json"`
		Valid bool   `db:"references_valid"`
	}
	if err := dbWrapper.Get(ctx, &row, "SELECT value_json,references_valid FROM metadata_collection_values WHERE entity_uuid=? AND field=?", entityUUID, field); err != nil {
		return nil, err
	}
	if !row.Valid {
		return nil, errors.New("metadata relationship has no archive identity")
	}
	return metadataFieldJSON([]byte(row.Value))
}

func decodeMetadataReferences(field string, value json.RawMessage) ([]metadataReference, error) {
	if _, err := metadataFieldJSON(value); err != nil {
		return nil, err
	}
	var ret []metadataReference
	switch field {
	case "studio":
		var id *string
		if err := json.Unmarshal(value, &id); err != nil {
			return nil, errors.New("studio requires a UUID or null")
		}
		if id != nil {
			ret = append(ret, metadataReference{UUID: *id})
		}
	case "performers", "tags":
		var ids []string
		if err := json.Unmarshal(value, &ids); err != nil || ids == nil {
			return nil, errors.New("metadata relationship requires an array of UUIDs")
		}
		for _, id := range ids {
			ret = append(ret, metadataReference{UUID: id})
		}
	case "groups":
		decoder := json.NewDecoder(bytes.NewReader(value))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&ret); err != nil || ret == nil {
			return nil, errors.New("groups requires an array of UUID and scene_index objects")
		}
	default:
		return nil, errors.New("not a reference metadata field")
	}
	if len(ret) > maxMetadataCollectionItems {
		return nil, errors.New("metadata relationship exceeds 4096 targets")
	}
	for i := range ret {
		id, err := archiveUUID(ret[i].UUID)
		if err != nil {
			return nil, err
		}
		ret[i].UUID = id
	}
	slices.SortFunc(ret, func(a, b metadataReference) int { return strings.Compare(a.UUID, b.UUID) })
	unique := make([]metadataReference, 0, len(ret))
	for _, ref := range ret {
		if len(unique) != 0 && unique[len(unique)-1].UUID == ref.UUID {
			previous := unique[len(unique)-1].SceneIndex
			if (previous == nil) != (ref.SceneIndex == nil) || (previous != nil && *previous != *ref.SceneIndex) {
				return nil, errors.New("one group has conflicting scene indexes")
			}
			continue
		}
		unique = append(unique, ref)
	}
	return unique, nil
}

func encodeMetadataReferences(field string, refs []metadataReference) (json.RawMessage, error) {
	var value interface{}
	switch field {
	case "studio":
		if len(refs) > 1 {
			return nil, errors.New("studio requires at most one target")
		}
		if len(refs) == 1 {
			value = refs[0].UUID
		}
	case "performers", "tags":
		ids := make([]string, 0, len(refs))
		for _, ref := range refs {
			ids = append(ids, ref.UUID)
		}
		value = ids
	case "groups":
		if refs == nil {
			refs = []metadataReference{}
		}
		value = refs
	default:
		return nil, errors.New("not a reference metadata field")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return metadataFieldJSON(raw)
}

func metadataDecisionReferences(ctx context.Context, id, field string, count int) (json.RawMessage, error) {
	var rows []metadataReference
	if err := dbWrapper.Select(ctx, &rows, "SELECT target_uuid,scene_index FROM metadata_field_references WHERE decision_uuid=? ORDER BY position LIMIT 4097", id); err != nil {
		return nil, err
	}
	if len(rows) != count || len(rows) > maxMetadataCollectionItems {
		return nil, errors.New("metadata reference count differs from the sealed decision")
	}
	return encodeMetadataReferences(field, rows)
}

// Compare identities through retained redirects. History still reports the
// original referenced UUIDs; a current selection uses their surviving targets.
func resolveMetadataReferences(ctx context.Context, field string, value json.RawMessage) (json.RawMessage, []*models.ArchiveEntity, error) {
	refs, err := decodeMetadataReferences(field, value)
	if err != nil {
		return nil, nil, err
	}
	requested := make(map[string]bool, len(refs))
	for _, ref := range refs {
		requested[ref.UUID] = true
	}
	resolved, err := sourceGalleryIdentities(ctx, requested)
	if err != nil {
		return nil, nil, err
	}
	targets := make([]*models.ArchiveEntity, 0, len(refs))
	for i := range refs {
		target := resolved[refs[i].UUID]
		if target == nil || target.Kind != metadataReferenceKind(field) {
			return nil, nil, errors.New("metadata reference has the wrong entity kind")
		}
		refs[i].UUID = target.UUID
		targets = append(targets, target)
	}
	value, err = encodeMetadataReferences(field, refs)
	if err != nil {
		return nil, nil, err
	}
	refs, err = decodeMetadataReferences(field, value)
	if err != nil {
		return nil, nil, err
	}
	value, err = encodeMetadataReferences(field, refs)
	return value, targets, err
}

func normalizeMetadataCollection(ctx context.Context, def models.MetadataFieldDefinition, raw json.RawMessage, revisions map[string]int) (json.RawMessage, interface{}, error) {
	ret := &metadataCollectionValue{}
	if def.ReferenceKind != "" {
		refs, err := decodeMetadataReferences(def.Name, raw)
		if err != nil {
			return nil, nil, err
		}
		requested := make(map[string]bool, len(refs))
		for _, ref := range refs {
			requested[ref.UUID] = true
		}
		if len(revisions) != len(requested) {
			return nil, nil, errors.New("metadata relationships require every target revision")
		}
		targets, err := sourceGalleryIdentities(ctx, requested)
		if err != nil {
			if errors.Is(err, models.ErrSourcePayloadCorrupt) {
				return nil, nil, models.ErrMetadataFieldConflict
			}
			return nil, nil, err
		}
		for _, ref := range refs {
			target := targets[ref.UUID]
			if target == nil || target.UUID != ref.UUID || target.State != models.ArchiveEntityActive || target.Kind != def.ReferenceKind || target.Revision != revisions[ref.UUID] {
				return nil, nil, models.ErrMetadataFieldConflict
			}
		}
		ret.references, ret.targets = refs, targets
		ret.value, err = encodeMetadataReferences(def.Name, refs)
		return ret.value, ret, err
	}
	if len(revisions) != 0 {
		return nil, nil, errors.New("only relationship metadata accepts target revisions")
	}
	if _, err := metadataFieldJSON(raw); err != nil {
		return nil, nil, err
	}
	switch def.Type {
	case "urls":
		var values []string
		if err := json.Unmarshal(raw, &values); err != nil || values == nil || len(values) > maxMetadataCollectionItems {
			return nil, nil, errors.New("urls requires an array of at most 4096 URLs")
		}
		seen := make(map[string]bool)
		ret.urls = []string{}
		for _, value := range values {
			value = strings.TrimSpace(value)
			u, err := url.Parse(value)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(value) > 8192 {
				return nil, nil, errors.New("metadata URLs must be absolute HTTP or HTTPS URLs within 8192 bytes")
			}
			if !seen[value] {
				ret.urls = append(ret.urls, value)
				seen[value] = true
			}
		}
		value, err := json.Marshal(ret.urls)
		if err != nil {
			return nil, nil, err
		}
		ret.value = value
	case "custom_fields":
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&ret.custom); err != nil || ret.custom == nil || len(ret.custom) > maxMetadataCollectionItems {
			return nil, nil, errors.New("custom_fields requires an object with at most 4096 primitive values")
		}
		for key, value := range ret.custom {
			if err := (&customFieldsStore{}).validateCustomFieldName(key); err != nil {
				return nil, nil, err
			}
			switch v := value.(type) {
			case string:
			case bool:
				ret.custom[key] = 0
				if v {
					ret.custom[key] = 1
				}
			case json.Number:
				if n, err := v.Int64(); err == nil {
					ret.custom[key] = n
				} else if strings.ContainsAny(v.String(), ".eE") {
					n, err := v.Float64()
					if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
						return nil, nil, errors.New("custom field number is outside the native range")
					}
					ret.custom[key] = n
				} else {
					return nil, nil, errors.New("custom field integer exceeds the native range; use a string for large identifiers")
				}
			default:
				return nil, nil, errors.New("custom field values must be strings, numbers or booleans; omit a key to remove it")
			}
		}
		// Canonicalization is for the JSON decision. Keep native double values
		// in the SQL bindings; json.Number there would be stored as text.
		canonical, err := canonicalMetadataJSONValue(maps.Clone(ret.custom))
		if err != nil {
			return nil, nil, err
		}
		value, err := json.Marshal(canonical)
		if err != nil {
			return nil, nil, err
		}
		ret.value = value
	default:
		return nil, nil, errors.New("unsupported metadata collection")
	}
	value, err := metadataFieldJSON(ret.value)
	ret.value = value
	return value, ret, err
}

func writeMetadataCollection(ctx context.Context, entity *models.ArchiveEntity, field string, value *metadataCollectionValue) error {
	relation, err := metadataRelationFor(entity.Kind, field)
	if err != nil {
		return err
	}
	if field == "studio" {
		var id *int
		if len(value.references) == 1 {
			id = value.targets[value.references[0].UUID].LocalID
		}
		result, err := dbWrapper.Exec(ctx, "UPDATE "+relation.table+" SET studio_id=?,updated_at=CURRENT_TIMESTAMP WHERE id=?", id, *entity.LocalID)
		return checkArchiveIdentityUpdate(result, err)
	}
	if _, err := dbWrapper.Exec(ctx, "DELETE FROM "+relation.table+" WHERE "+relation.owner+"=?", *entity.LocalID); err != nil {
		return err
	}
	switch field {
	case "urls":
		for position, item := range value.urls {
			if _, err := dbWrapper.Exec(ctx, "INSERT INTO "+relation.table+"("+relation.owner+",position,url) VALUES(?,?,?)", *entity.LocalID, position, item); err != nil {
				return err
			}
		}
	case "custom_fields":
		for key, item := range value.custom {
			if _, err := dbWrapper.Exec(ctx, "INSERT INTO "+relation.table+"("+relation.owner+",field,value) VALUES(?,?,?)", *entity.LocalID, key, item); err != nil {
				return err
			}
		}
	default:
		for _, ref := range value.references {
			query := "INSERT INTO " + relation.table + "(" + relation.owner + "," + relation.foreign + ") VALUES(?,?)"
			args := []interface{}{*entity.LocalID, *value.targets[ref.UUID].LocalID}
			if field == "groups" {
				query = "INSERT INTO " + relation.table + "(" + relation.owner + "," + relation.foreign + ",scene_index) VALUES(?,?,?)"
				args = append(args, ref.SceneIndex)
			}
			if _, err := dbWrapper.Exec(ctx, query, args...); err != nil {
				return err
			}
		}
	}
	table, _, err := metadataFieldTable(entity.Kind)
	if err != nil {
		return err
	}
	result, err := dbWrapper.Exec(ctx, "UPDATE "+table+" SET updated_at=CURRENT_TIMESTAMP WHERE id=?", *entity.LocalID)
	return checkArchiveIdentityUpdate(result, err)
}

func metadataPending(ctx context.Context, entity, field string) (json.RawMessage, error) {
	var raw string
	if err := dbWrapper.Get(ctx, &raw, "SELECT previous_json FROM metadata_field_pending WHERE entity_uuid=? AND field=?", entity, field); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading pending metadata choice: %w", err)
	}
	return metadataFieldJSON([]byte(raw))
}

func insertMetadataReferenceDecision(ctx context.Context, entity, field, mode, origin string, value json.RawMessage, capture *string, reason string) error {
	refs, err := decodeMetadataReferences(field, value)
	if err != nil {
		return err
	}
	id := uuid.NewString()
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO metadata_field_decisions
(uuid,entity_uuid,field,mode,origin,value_json,capture_uuid,reason,sealed)
VALUES(?,?,?,?,?,'null',?,?,0)`, id, entity, field, mode, origin, capture, reason); err != nil {
		return err
	}
	for position, ref := range refs {
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO metadata_field_references
(decision_uuid,position,target_uuid,scene_index) VALUES(?,?,?,?)`, id, position, ref.UUID, ref.SceneIndex); err != nil {
			return err
		}
	}
	_, err = dbWrapper.Exec(ctx, "UPDATE metadata_field_decisions SET sealed=1,reference_count=? WHERE uuid=?", len(refs), id)
	return err
}

// The first native join change retains the previous value; the transaction's
// final value becomes one decision regardless of how many rows were changed.
func flushMetadataFieldPending(ctx context.Context, entity, field string) error {
	previous, err := metadataPending(ctx, entity, field)
	if err != nil || previous == nil {
		return err
	}
	identity, err := (&ArchiveEntityStore{}).Find(ctx, entity)
	if err != nil {
		return err
	}
	if identity == nil {
		return errors.New("pending metadata entity is missing")
	}
	if identity.State == models.ArchiveEntityActive {
		def, err := metadataFieldDefinition(identity.Kind, field)
		if err != nil {
			return err
		}
		value, err := readMetadataCollection(ctx, entity, field)
		if err != nil {
			return err
		}
		var head, legacy bool
		if err := dbWrapper.Get(ctx, &head, "SELECT EXISTS(SELECT 1 FROM metadata_field_heads WHERE entity_uuid=? AND field=?)", entity, field); err != nil {
			return err
		}
		if !head {
			if err := dbWrapper.Get(ctx, &legacy, "SELECT EXISTS(SELECT 1 FROM metadata_field_baselines WHERE entity_uuid=?)", entity); err != nil {
				return err
			}
			if legacy || !bytes.Equal(previous, def.ClearValue) {
				origin := "unattributed"
				if legacy {
					origin = "legacy"
				}
				if err := insertMetadataDecision(ctx, entity, field, "preserved", origin, previous, nil, "Preserved before the first recorded choice"); err != nil {
					return err
				}
			}
		}
		mode := "set"
		if bytes.Equal(value, def.ClearValue) {
			mode = "clear"
		}
		if err := insertMetadataDecision(ctx, entity, field, mode, "library", value, nil, ""); err != nil {
			return err
		}
	}
	_, err = dbWrapper.Exec(ctx, "DELETE FROM metadata_field_pending WHERE entity_uuid=? AND field=?", entity, field)
	return err
}

func flushMetadataPending(ctx context.Context) error {
	for {
		var rows []struct {
			Entity string `db:"entity_uuid"`
			Field  string `db:"field"`
		}
		if err := dbWrapper.Select(ctx, &rows, "SELECT entity_uuid,field FROM metadata_field_pending ORDER BY entity_uuid,field LIMIT 100"); err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			if err := flushMetadataFieldPending(ctx, row.Entity, row.Field); err != nil {
				return fmt.Errorf("recording final %s metadata choice: %w", row.Field, err)
			}
		}
	}
}

func validateMetadataCommit(ctx context.Context) error {
	var unfinished bool
	if err := dbWrapper.Get(ctx, &unfinished, `SELECT EXISTS(SELECT 1 FROM metadata_field_write_context)
OR EXISTS(SELECT 1 FROM metadata_field_decisions WHERE sealed=0)`); err != nil {
		return err
	}
	if unfinished {
		return errors.New("unfinished metadata field write context or unsealed decision")
	}
	return flushMetadataPending(ctx)
}

// SQL triggers see row changes; an explicit empty SET or an ADD of an existing
// relationship must protect the value too. Inverted joins target each media
// owner. Other join tables do not participate in metadata field choices.
func markMetadataCollectionIntent(ctx context.Context, table, idColumn string, id int, foreignIDs []int) error {
	for _, kind := range []models.ArchiveEntityKind{models.ArchiveScene, models.ArchiveImage, models.ArchiveGallery} {
		for _, field := range []string{"performers", "tags", "groups", "urls", "custom_fields"} {
			relation, err := metadataRelationFor(kind, field)
			if err != nil || relation.table != table {
				continue
			}
			ids := foreignIDs
			if idColumn == relation.owner {
				ids = []int{id}
			}
			for _, owner := range ids {
				_, err := dbWrapper.Exec(ctx, `INSERT INTO metadata_field_pending(entity_uuid,field,previous_json)
SELECT a.uuid,?,v.value_json FROM archive_entities a
JOIN metadata_collection_values v ON v.entity_uuid=a.uuid AND v.field=?
WHERE a.`+relation.owner+`=? AND a.state='active'
AND NOT EXISTS(SELECT 1 FROM metadata_field_write_context w WHERE w.entity_uuid=a.uuid AND w.field=?)
AND NOT EXISTS(SELECT 1 FROM metadata_field_pending p WHERE p.entity_uuid=a.uuid AND p.field=?)`, field, field, owner, field, field)
				if err != nil {
					return err
				}
			}
			return nil
		}
	}
	return nil
}
