package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type SourceFileHistoryStore struct{}

const sourceFileHistoryColumns = `h.uuid,h.kind,h.collection_uuid,h.collection_revision,h.root_uuid,h.root_revision,h.reference_namespace,h.reference_value,
 h.source_time,h.origin,h.observed_at,CAST(h.details AS BLOB) AS details,h.created_at`

type sourceFileHistoryRow struct {
	models.SourceFileHistory
	LocationCount int    `db:"location_count"`
	EditCount     int    `db:"edit_count"`
	Signature     string `db:"signature"`
}

func normalizeSourceFileHistory(v *models.SourceFileHistory) error {
	invalid := models.ErrSourceFileHistoryInvalid
	if sourceFileIDs(&v.UUID, &v.CollectionUUID, &v.RootUUID) != nil || v.CollectionRevision < 1 || v.RootRevision < 1 ||
		!validAccountText(v.ReferenceNamespace, 128, false) || !validAccountText(v.ReferenceValue, 4096, false) ||
		!sourceFileTimestamp(v.SourceTime) || !sourceFileOrigin(v.Origin) || !validJobTime(v.ObservedAt) ||
		len(v.Locations) < 1 || len(v.Locations) > 1024 || sourceFileDetails(&v.Details) != nil {
		return invalid
	}
	v.ObservedAt = v.ObservedAt.UTC()
	paths := make(map[string]bool, len(v.Locations))
	for i, location := range v.Locations {
		if location.Position != i || !archive.ValidRootRelativePath(location.RelativePath, false) ||
			(location.ArchivePath != nil && !archive.ValidRootRelativePath(*location.ArchivePath, false)) ||
			sourceFileIDs(location.ObservationUUID) != nil || (v.Kind != "deduplication" && location.ObservationUUID == nil) {
			return invalid
		}
		path := sourceFileHistoryPath(location)
		if paths[path] {
			return invalid
		}
		paths[path] = true
	}
	switch v.Kind {
	case "metadata_edit":
		if len(v.Locations) != 1 || len(v.Edits) < 1 || len(v.Edits) > 256 || v.StateChange != nil || v.Deduplication != nil {
			return invalid
		}
		fields := make(map[string]json.RawMessage, len(v.Edits))
		for _, edit := range v.Edits {
			if _, exists := fields[edit.Field]; exists {
				return invalid
			}
			fields[edit.Field] = edit.Value
		}
		body, err := archive.EncodeSourceJSON(fields)
		if err != nil {
			return invalid
		}
		expected, err := archive.CatalogFileEdits(body)
		if err != nil || !reflect.DeepEqual(v.Edits, expected) {
			return invalid
		}
	case "state_change":
		if len(v.Locations) != 1 || len(v.Edits) != 0 || v.StateChange == nil || v.Deduplication != nil ||
			!sourceFileState(v.StateChange.OldState) || !sourceFileState(v.StateChange.NewState) || !validAccountText(v.StateChange.Reason, 4096, false) {
			return invalid
		}
	case "deduplication":
		if len(v.Locations) < 2 || len(v.Edits) != 0 || v.StateChange != nil || v.Deduplication == nil ||
			sourceFileIDs(&v.Deduplication.ContentClaimUUID) != nil {
			return invalid
		}
		dedupe := v.Deduplication
		switch dedupe.Stage {
		case "prepared":
			if dedupe.SurvivorPath != nil {
				return invalid
			}
		case "finished":
			if dedupe.SurvivorPath == nil || !archive.ValidRootRelativePath(*dedupe.SurvivorPath, false) {
				return invalid
			}
			found := false
			for _, location := range v.Locations {
				found = found || sourceFileHistoryPath(location) == *dedupe.SurvivorPath
			}
			if !found {
				return invalid
			}
		default:
			return invalid
		}
	default:
		return invalid
	}
	return nil
}

func sourceFileHistoryPath(location models.SourceFileHistoryLocation) string {
	if location.ArchivePath != nil {
		return *location.ArchivePath + "/" + location.RelativePath
	}
	return location.RelativePath
}

func sourceFileState(value string) bool {
	return value == "present" || value == "missing" || value == "pending" || value == "deduplicated"
}

func sourceFileHistorySignature(v models.SourceFileHistory) (string, error) {
	locations := make([]any, 0, len(v.Locations))
	for _, location := range v.Locations {
		locations = append(locations, []any{location.Position, location.RelativePath, location.ArchivePath, location.ObservationUUID})
	}
	edits := make([]any, 0, len(v.Edits))
	for _, edit := range v.Edits {
		edits = append(edits, []any{edit.Field, edit.TargetField, edit.ValueType, edit.Mode, edit.Value})
	}
	var state, dedupe any
	if v.StateChange != nil {
		state = []any{v.StateChange.OldState, v.StateChange.NewState, v.StateChange.Reason}
	}
	if v.Deduplication != nil {
		dedupe = []any{v.Deduplication.ContentClaimUUID, v.Deduplication.Stage, v.Deduplication.SurvivorPath}
	}
	return sourceSignature("stash-source-file-history-v1", []any{v.UUID, v.Kind, v.CollectionUUID, v.CollectionRevision, v.RootUUID, v.RootRevision,
		v.ReferenceNamespace, v.ReferenceValue, v.SourceTime, v.Origin, v.ObservedAt.UTC().Format(time.RFC3339Nano), v.Details, locations, edits, state, dedupe})
}

func readSourceFileHistory(get func(any, string, ...any) error, selectRows func(any, string, ...any) error, id string) (*models.SourceFileHistory, error) {
	var row sourceFileHistoryRow
	if err := get(&row, "SELECT "+sourceFileHistoryColumns+",h.location_count,h.edit_count,h.signature FROM source_file_history h WHERE h.uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	v := &row.SourceFileHistory
	if err := selectRows(&v.Locations, "SELECT position,relative_path,archive_path,observation_uuid FROM source_file_history_locations WHERE history_uuid=? ORDER BY position LIMIT 1025", id); err != nil {
		return nil, err
	}
	if err := selectRows(&v.Edits, "SELECT field,target_field,value_type,mode,CAST(value_json AS BLOB) AS value_json FROM source_file_history_edits WHERE history_uuid=? ORDER BY field LIMIT 257", id); err != nil {
		return nil, err
	}
	var state models.SourceFileStateChange
	if err := get(&state, "SELECT old_state,new_state,reason FROM source_file_history_states WHERE history_uuid=?", id); err == nil {
		v.StateChange = &state
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var dedupe models.SourceFileDeduplication
	if err := get(&dedupe, "SELECT content_claim_uuid,stage,survivor_path FROM source_file_history_deduplications WHERE history_uuid=?", id); err == nil {
		v.Deduplication = &dedupe
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if len(v.Locations) != row.LocationCount || len(v.Edits) != row.EditCount || normalizeSourceFileHistory(v) != nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	signature, err := sourceFileHistorySignature(*v)
	if err != nil || signature != row.Signature {
		return nil, models.ErrSourcePayloadCorrupt
	}
	var invalid bool
	if err := get(&invalid, `SELECT EXISTS(SELECT 1 FROM source_file_history_locations l
 JOIN source_file_history h ON h.uuid=l.history_uuid LEFT JOIN source_file_observations o ON o.uuid=l.observation_uuid
 LEFT JOIN source_file_history_deduplications d ON d.history_uuid=h.uuid WHERE h.uuid=? AND l.observation_uuid IS NOT NULL
 AND (o.uuid IS NULL OR o.collection_uuid!=h.collection_uuid OR o.collection_revision!=h.collection_revision
 OR o.root_uuid!=h.root_uuid OR o.root_revision!=h.root_revision OR o.relative_path!=l.relative_path OR o.archive_path IS NOT l.archive_path
 OR (d.history_uuid IS NOT NULL AND o.content_claim_uuid IS NOT d.content_claim_uuid)))
 OR EXISTS(SELECT 1 FROM source_file_history_deduplications d JOIN source_file_history h ON h.uuid=d.history_uuid
 LEFT JOIN source_content_claims c ON c.uuid=d.content_claim_uuid WHERE h.uuid=?
 AND (c.uuid IS NULL OR c.collection_uuid!=h.collection_uuid OR c.collection_revision!=h.collection_revision))`, id, id); err != nil {
		return nil, err
	}
	if invalid {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return v, nil
}

func (s *SourceFileHistoryStore) Find(ctx context.Context, id string) (*models.SourceFileHistory, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrSourceFileHistoryInvalid
	}
	return readSourceFileHistory(func(out any, q string, args ...any) error { return dbWrapper.Get(ctx, out, q, args...) },
		func(out any, q string, args ...any) error { return dbWrapper.Select(ctx, out, q, args...) }, id)
}

func (s *SourceFileHistoryStore) Record(ctx context.Context, input models.SourceFileHistory) (*models.SourceFileHistory, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if err := normalizeSourceFileHistory(&input); err != nil {
		return nil, err
	}
	prior, err := s.Find(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	signature, err := sourceFileHistorySignature(input)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		expected, err := sourceFileHistorySignature(*prior)
		if err != nil || expected != signature {
			return nil, models.ErrSourceFileEvidenceReplay
		}
		return prior, nil
	}
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return models.ErrSourceFileHistoryInvalid
		}
		return nil
	})
	_, err = dbWrapper.Exec(ctx, `INSERT INTO source_file_history(uuid,kind,collection_uuid,collection_revision,root_uuid,root_revision,
 reference_namespace,reference_value,source_time,origin,observed_at,details,location_count,edit_count,signature)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, input.UUID, input.Kind, input.CollectionUUID, input.CollectionRevision, input.RootUUID, input.RootRevision,
		input.ReferenceNamespace, input.ReferenceValue, input.SourceTime, input.Origin, input.ObservedAt, string(input.Details), len(input.Locations), len(input.Edits), signature)
	if err != nil {
		return nil, err
	}
	for _, location := range input.Locations {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO source_file_history_locations VALUES(?,?,?,?,?)", input.UUID, location.Position, location.RelativePath, location.ArchivePath, location.ObservationUUID); err != nil {
			return nil, err
		}
	}
	for _, edit := range input.Edits {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO source_file_history_edits VALUES(?,?,?,?,?,?)", input.UUID, edit.Field, edit.TargetField, edit.ValueType, edit.Mode, string(edit.Value)); err != nil {
			return nil, err
		}
	}
	if v := input.StateChange; v != nil {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO source_file_history_states VALUES(?,?,?,?)", input.UUID, v.OldState, v.NewState, v.Reason); err != nil {
			return nil, err
		}
	}
	if v := input.Deduplication; v != nil {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO source_file_history_deduplications VALUES(?,?,?,?)", input.UUID, v.ContentClaimUUID, v.Stage, v.SurvivorPath); err != nil {
			return nil, err
		}
	}
	result, err := s.Find(ctx, input.UUID)
	complete = err == nil
	return result, err
}

func sourceFileHistoryPage(ctx context.Context, id, after string, limit int, joins string) ([]models.SourceFileHistory, error) {
	if !validSourceRunUUID(id) || (after != "" && !validSourceRunUUID(after)) || limit < 1 || limit > 100 {
		return nil, models.ErrSourceFileHistoryInvalid
	}
	ret := []models.SourceFileHistory{}
	err := dbWrapper.Select(ctx, &ret, "SELECT DISTINCT "+sourceFileHistoryColumns+" FROM source_file_history h "+joins+" AND h.uuid>? ORDER BY h.uuid LIMIT ?", id, after, limit)
	return ret, err
}

func (s *SourceFileHistoryStore) ObservationHistory(ctx context.Context, id, after string, limit int) ([]models.SourceFileHistory, error) {
	return sourceFileHistoryPage(ctx, id, after, limit, "JOIN source_file_history_locations l ON l.history_uuid=h.uuid WHERE l.observation_uuid=?")
}

func (s *SourceFileHistoryStore) ClaimHistory(ctx context.Context, id, after string, limit int) ([]models.SourceFileHistory, error) {
	return sourceFileHistoryPage(ctx, id, after, limit, "JOIN source_file_history_deduplications d ON d.history_uuid=h.uuid WHERE d.content_claim_uuid=?")
}
