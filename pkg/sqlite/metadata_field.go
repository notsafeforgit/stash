package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type MetadataFieldStore struct{}

const maxMetadataFieldBytes = 4 * 1024 * 1024

type metadataFieldRow struct {
	UUID        string         `db:"uuid"`
	Sequence    int            `db:"sequence"`
	EntityUUID  string         `db:"entity_uuid"`
	Field       string         `db:"field"`
	Mode        string         `db:"mode"`
	Origin      string         `db:"origin"`
	Value       string         `db:"value_json"`
	CaptureUUID sql.NullString `db:"capture_uuid"`
	Reason      string         `db:"reason"`
	CreatedAt   Timestamp      `db:"created_at"`
	Sealed      bool           `db:"sealed"`
	RefCount    int            `db:"reference_count"`
}

func (r metadataFieldRow) resolve(ctx context.Context) (*models.MetadataFieldDecision, error) {
	if !r.Sealed {
		return nil, errors.New("metadata decision is not sealed")
	}
	value, err := metadataFieldJSON([]byte(r.Value))
	if err != nil {
		return nil, err
	}
	if metadataReferenceKind(r.Field) != "" {
		value, err = metadataDecisionReferences(ctx, r.UUID, r.Field, r.RefCount)
		if err != nil {
			return nil, err
		}
	}
	ret := &models.MetadataFieldDecision{UUID: r.UUID, Sequence: r.Sequence, EntityUUID: r.EntityUUID,
		Field: r.Field, Mode: r.Mode, Origin: r.Origin, Value: value, Reason: r.Reason, CreatedAt: r.CreatedAt.Timestamp}
	if r.CaptureUUID.Valid {
		ret.CaptureUUID = &r.CaptureUUID.String
	}
	var policy struct {
		CollectionUUID string `db:"collection_uuid"`
		Revision       int    `db:"revision"`
	}
	if err := dbWrapper.Get(ctx, &policy, "SELECT collection_uuid,revision FROM metadata_decision_policies WHERE decision_uuid=?", r.UUID); err == nil {
		ret.Policy = &models.MetadataPolicyRef{CollectionUUID: policy.CollectionUUID, Revision: policy.Revision}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return ret, nil
}

func metadataFieldJSON(value json.RawMessage) (json.RawMessage, error) {
	if len(value) > maxMetadataFieldBytes || !utf8.Valid(value) || !json.Valid(value) {
		return nil, errors.New("metadata value must be valid UTF-8 JSON within 4 MiB")
	}
	var decoded interface{}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	decoded, err := canonicalMetadataJSONValue(decoded)
	if err != nil {
		return nil, err
	}
	ret, err := json.Marshal(decoded)
	if err == nil && len(ret) > maxMetadataFieldBytes {
		return nil, errors.New("canonical metadata value exceeds 4 MiB")
	}
	return ret, err
}

func metadataFieldDefinition(kind models.ArchiveEntityKind, name string) (models.MetadataFieldDefinition, error) {
	for _, def := range models.MetadataFields(kind) {
		if def.Name == name {
			return def, nil
		}
	}
	return models.MetadataFieldDefinition{}, fmt.Errorf("unsupported %s metadata field %q", kind, name)
}

func metadataFieldTable(kind models.ArchiveEntityKind) (string, string, error) {
	switch kind {
	case models.ArchiveScene:
		return "scenes", "scene_id", nil
	case models.ArchiveImage:
		return "images", "image_id", nil
	case models.ArchiveGallery:
		return "galleries", "gallery_id", nil
	default:
		return "", "", errors.New("metadata choices require a scene, image or gallery")
	}
}

func metadataFieldColumn(field string) string {
	if field == "rating100" {
		return "rating"
	}
	return field
}

// The definition comes exclusively from the native field allowlist.
func metadataFieldValueSQL(def models.MetadataFieldDefinition) string {
	column := metadataFieldColumn(def.Name)
	switch def.Type {
	case "string":
		return "json_quote(coalesce(" + column + ", ''))"
	case "date":
		return "json_quote(CASE WHEN " + column + " IS NULL THEN NULL ELSE substr(" + column + ", 1, CASE " + column + "_precision WHEN 1 THEN 7 WHEN 2 THEN 4 ELSE 10 END) END)"
	case "boolean":
		return "CASE WHEN " + column + " THEN 'true' ELSE 'false' END"
	default:
		return "json_quote(" + column + ")"
	}
}

func (s *MetadataFieldStore) State(ctx context.Context, entityUUID, field string) (*models.MetadataFieldState, error) {
	identity, err := (&ArchiveEntityStore{}).Find(ctx, entityUUID)
	if err != nil {
		return nil, err
	}
	if identity == nil || identity.State != models.ArchiveEntityActive || identity.LocalID == nil {
		return nil, models.ErrMetadataFieldConflict
	}
	def, err := metadataFieldDefinition(identity.Kind, field)
	if err != nil {
		return nil, err
	}
	table, _, err := metadataFieldTable(identity.Kind)
	if err != nil {
		return nil, err
	}
	var value json.RawMessage
	if metadataCollectionDefinition(def) {
		value, err = readMetadataCollection(ctx, identity.UUID, field)
	} else {
		var raw string
		err = dbWrapper.Get(ctx, &raw, "SELECT "+metadataFieldValueSQL(def)+" FROM "+table+" WHERE id=?", *identity.LocalID)
		if err == nil {
			value, err = metadataFieldJSON([]byte(raw))
		}
	}
	if err != nil {
		return nil, err
	}
	ret := &models.MetadataFieldState{Entity: identity, Field: field, Value: value, Mode: "inherit", Origin: "unset"}
	if def.ReferenceKind != "" {
		ret.Value, ret.References, err = resolveMetadataReferences(ctx, field, value)
		if err != nil {
			return nil, err
		}
	}
	if metadataCollectionDefinition(def) {
		previous, err := metadataPending(ctx, identity.UUID, field)
		if err != nil {
			return nil, err
		}
		if previous != nil {
			ret.Pending, ret.Protected, ret.Mode, ret.Origin = true, true, "set", "library"
			if bytes.Equal(ret.Value, def.ClearValue) {
				ret.Mode = "clear"
			}
			return ret, nil
		}
	}
	var row metadataFieldRow
	err = dbWrapper.Get(ctx, &row, `SELECT d.* FROM metadata_field_heads h
JOIN metadata_field_decisions d ON d.uuid=h.decision_uuid AND d.entity_uuid=h.entity_uuid AND d.field=h.field
WHERE h.entity_uuid=? AND h.field=?`, identity.UUID, field)
	if err == nil {
		ret.Decision, err = row.resolve(ctx)
		if err != nil {
			return nil, err
		}
		chosen := ret.Decision.Value
		if def.ReferenceKind != "" {
			chosen, _, err = resolveMetadataReferences(ctx, field, chosen)
			if err != nil {
				return nil, err
			}
		}
		if !bytes.Equal(ret.Value, chosen) {
			return nil, errors.New("metadata field differs from its recorded decision")
		}
		ret.Mode, ret.Origin = row.Mode, row.Origin
		ret.Protected = row.Mode != "inherit"
		return ret, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var legacy bool
	if err := dbWrapper.Get(ctx, &legacy, "SELECT EXISTS(SELECT 1 FROM metadata_field_baselines WHERE entity_uuid=?)", identity.UUID); err != nil {
		return nil, err
	}
	if legacy || !bytes.Equal(value, def.ClearValue) {
		ret.Mode, ret.Origin, ret.Protected = "preserved", "unattributed", true
		if legacy {
			ret.Origin = "legacy"
		}
	}
	return ret, nil
}

func (s *MetadataFieldStore) History(ctx context.Context, entityUUID, field string, after, limit int) ([]models.MetadataFieldDecision, error) {
	id, err := archiveUUID(entityUUID)
	if err != nil {
		return nil, err
	}
	limit, err = sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	if after < 0 {
		return nil, errors.New("invalid metadata history cursor")
	}
	identity, err := (&ArchiveEntityStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if identity == nil {
		return nil, models.ErrMetadataFieldConflict
	}
	if _, err := metadataFieldDefinition(identity.Kind, field); err != nil {
		return nil, err
	}
	var rows []metadataFieldRow
	if err := dbWrapper.Select(ctx, &rows, "SELECT * FROM metadata_field_decisions WHERE entity_uuid=? AND field=? AND sequence>? AND sealed=1 ORDER BY sequence LIMIT ?", id, field, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.MetadataFieldDecision, 0, len(rows))
	for _, row := range rows {
		decision, err := row.resolve(ctx)
		if err != nil {
			return nil, err
		}
		ret = append(ret, *decision)
	}
	return ret, nil
}

func normalizeMetadataValue(def models.MetadataFieldDefinition, raw json.RawMessage) (json.RawMessage, interface{}, error) {
	value, err := metadataFieldJSON(raw)
	if err != nil {
		return nil, nil, err
	}
	var decoded interface{}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, nil, err
	}
	switch def.Type {
	case "string", "date":
		if decoded == nil && def.Type == "date" {
			return value, nil, nil
		}
		text, ok := decoded.(string)
		if !ok {
			return nil, nil, errors.New("metadata field requires a string")
		}
		if def.Type == "date" {
			date, err := models.ParseDate(text)
			if err != nil {
				return nil, nil, err
			}
			value, err = json.Marshal(date.String())
			return value, &date, err
		}
		return value, text, nil
	case "boolean":
		boolean, ok := decoded.(bool)
		if !ok {
			return nil, nil, errors.New("metadata field requires a boolean")
		}
		return value, boolean, nil
	case "integer":
		if decoded == nil {
			return value, nil, nil
		}
		number, ok := decoded.(json.Number)
		if !ok {
			return nil, nil, errors.New("rating100 requires an integer or null")
		}
		n, err := number.Int64()
		if err != nil || n < 0 || n > 100 {
			return nil, nil, errors.New("rating100 must be an integer between 0 and 100")
		}
		// JSON permits -0, while SQLite stores it as 0. Store the canonical
		// integer so a successful choice reads back as the same value.
		value, err = json.Marshal(n)
		return value, n, err
	default:
		return nil, nil, errors.New("unsupported metadata value type")
	}
}

func (s *MetadataFieldStore) Normalize(ctx context.Context, kind models.ArchiveEntityKind, field string, raw json.RawMessage, revisions map[string]int) (json.RawMessage, error) {
	def, err := metadataFieldDefinition(kind, field)
	if err != nil {
		return nil, err
	}
	var value json.RawMessage
	if metadataCollectionDefinition(def) {
		value, _, err = normalizeMetadataCollection(ctx, def, raw, revisions)
	} else {
		if len(revisions) != 0 {
			return nil, errors.New("only relationship metadata accepts target revisions")
		}
		value, _, err = normalizeMetadataValue(def, raw)
	}
	return value, err
}

func (s *MetadataFieldStore) Decide(ctx context.Context, input models.MetadataFieldDecisionInput) (*models.MetadataFieldState, error) {
	if input.Origin != "review" && input.Origin != "migration" {
		return nil, errors.New("metadata choice requires review or migration")
	}
	return s.apply(ctx, input, false)
}

func (s *MetadataFieldStore) ApplyAutomatic(ctx context.Context, input models.MetadataFieldDecisionInput) (*models.MetadataFieldState, error) {
	if input.Origin != "source" && input.Origin != "policy" && input.Origin != "filename" {
		return nil, errors.New("invalid automatic metadata origin")
	}
	if input.Mode != "inherit" {
		return nil, errors.New("automatic metadata requires inheritance")
	}
	if input.Origin == "filename" && (input.Field != "title" || input.CaptureUUID != "") {
		return nil, errors.New("filename fallback can set only title without source capture provenance")
	}
	return s.apply(ctx, input, true)
}

func (s *MetadataFieldStore) apply(ctx context.Context, input models.MetadataFieldDecisionInput, automatic bool) (*models.MetadataFieldState, error) {
	if !automatic && input.Policy != nil {
		return nil, errors.New("reviewed field choices do not accept automatic policy provenance")
	}
	current, err := s.State(ctx, input.EntityUUID, input.Field)
	if err != nil {
		return nil, err
	}
	if input.ExpectedEntityRevision != current.Entity.Revision {
		return nil, models.ErrMetadataFieldConflict
	}
	def, err := metadataFieldDefinition(current.Entity.Kind, input.Field)
	if err != nil {
		return nil, err
	}
	if automatic && (current.Protected || (input.Origin == "filename" && current.Origin != "filename" && !bytes.Equal(current.Value, def.ClearValue))) {
		return nil, models.ErrMetadataFieldProtected
	}
	if !validAccountText(input.Reason, 4096, true) {
		return nil, errors.New("invalid metadata choice reason")
	}
	switch input.Mode {
	case "clear":
		if len(input.Value) != 0 {
			return nil, errors.New("clear does not accept a metadata value")
		}
		input.Value = def.ClearValue
	case "inherit":
		if len(input.Value) == 0 {
			input.Value = def.ClearValue
		}
	case "set":
	default:
		return nil, errors.New("invalid metadata choice mode")
	}
	var value json.RawMessage
	var native interface{}
	if metadataCollectionDefinition(def) {
		value, native, err = normalizeMetadataCollection(ctx, def, input.Value, input.ReferenceRevisions)
	} else {
		if len(input.ReferenceRevisions) != 0 {
			return nil, errors.New("only relationship metadata accepts target revisions")
		}
		value, native, err = normalizeMetadataValue(def, input.Value)
	}
	if err != nil {
		return nil, err
	}
	var capture *string
	if input.CaptureUUID != "" {
		id, err := archiveUUID(input.CaptureUUID)
		if err != nil {
			return nil, err
		}
		capture = &id
		var active bool
		if err := dbWrapper.Get(ctx, &active, `SELECT EXISTS(SELECT 1 FROM source_captures c JOIN source_posts p ON p.uuid=c.post_uuid
WHERE c.uuid=? AND p.state='active')`, id); err != nil {
			return nil, err
		}
		if !active {
			return nil, errors.New("metadata choice requires an existing active source capture")
		}
	}
	if input.Origin == "source" && capture == nil {
		return nil, errors.New("source metadata requires capture provenance")
	}
	if d := current.Decision; d != nil && d.Mode == input.Mode && d.Origin == input.Origin && d.Reason == input.Reason &&
		bytes.Equal(d.Value, value) && equalMetadataCapture(d.CaptureUUID, capture) && reflect.DeepEqual(d.Policy, input.Policy) {
		return current, nil
	}
	var ret *models.MetadataFieldState
	err = withMetadataFieldWrite(ctx, current.Entity.UUID, input.Field, func() error {
		if current.Pending {
			if err := flushMetadataFieldPending(ctx, current.Entity.UUID, input.Field); err != nil {
				return err
			}
			current, err = s.State(ctx, current.Entity.UUID, input.Field)
			if err != nil {
				return err
			}
		}
		if current.Decision == nil && current.Protected {
			if err := insertMetadataDecision(ctx, current.Entity.UUID, input.Field, "preserved", current.Origin, current.Value, nil, "Preserved before the first recorded choice"); err != nil {
				return err
			}
		}
		if collection, ok := native.(*metadataCollectionValue); ok {
			if err := writeMetadataCollection(ctx, current.Entity, input.Field, collection); err != nil {
				return err
			}
		} else {
			table, _, err := metadataFieldTable(current.Entity.Kind)
			if err != nil {
				return err
			}
			column := metadataFieldColumn(def.Name)
			query := "UPDATE " + table + " SET " + column + "=?, updated_at=CURRENT_TIMESTAMP WHERE id=?"
			args := []interface{}{native, *current.Entity.LocalID}
			if def.Type == "date" {
				date, _ := native.(*models.Date)
				query = "UPDATE " + table + " SET " + column + "=?, " + column + "_precision=?, updated_at=CURRENT_TIMESTAMP WHERE id=?"
				args = []interface{}{NullDateFromDatePtr(date), datePrecisionFromDatePtr(date), *current.Entity.LocalID}
			}
			result, err := dbWrapper.Exec(ctx, query, args...)
			if err := checkArchiveIdentityUpdate(result, err); err != nil {
				return err
			}
		}
		if err := insertMetadataDecision(ctx, current.Entity.UUID, input.Field, input.Mode, input.Origin, value, capture, input.Reason); err != nil {
			return err
		}
		ret, err = s.State(ctx, current.Entity.UUID, input.Field)
		if err == nil && input.Policy != nil {
			_, err = dbWrapper.Exec(ctx, "INSERT INTO metadata_decision_policies(decision_uuid,collection_uuid,revision) VALUES(?,?,?)", ret.Decision.UUID, input.Policy.CollectionUUID, input.Policy.Revision)
			ref := *input.Policy
			ret.Decision.Policy = &ref
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func equalMetadataCapture(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func insertMetadataDecision(ctx context.Context, entityUUID, field, mode, origin string, value json.RawMessage, capture *string, reason string) error {
	if metadataReferenceKind(field) != "" {
		return insertMetadataReferenceDecision(ctx, entityUUID, field, mode, origin, value, capture, reason)
	}
	_, err := dbWrapper.Exec(ctx, `INSERT INTO metadata_field_decisions(uuid, entity_uuid, field, mode, origin, value_json, capture_uuid, reason)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, uuid.NewString(), entityUUID, field, mode, origin, string(value), capture, reason)
	return err
}

func withMetadataFieldWrite(ctx context.Context, entityUUID, field string, fn func() error) error {
	if _, err := getTx(ctx); err != nil {
		return err
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		var unfinished bool
		if err := dbWrapper.Get(ctx, &unfinished, "SELECT EXISTS(SELECT 1 FROM metadata_field_write_context)"); err != nil {
			return err
		}
		if unfinished {
			return errors.New("unfinished metadata field write context")
		}
		return nil
	})
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO metadata_field_write_context(entity_uuid, field) VALUES (?, ?)", entityUUID, field); err != nil {
		return err
	}
	if err := fn(); err != nil {
		return err
	}
	_, err := dbWrapper.Exec(ctx, "DELETE FROM metadata_field_write_context WHERE entity_uuid=? AND field=?", entityUUID, field)
	return err
}
