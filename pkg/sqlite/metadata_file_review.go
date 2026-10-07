package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

const metadataFileReviewReason = "Selected a retained catalog edit after review"

func validateMetadataFileEditInput(input models.MetadataFileEditInput) error {
	for _, id := range []string{input.EntityUUID, input.HistoryUUID, input.MatchUUID} {
		parsed, err := archiveUUID(id)
		if err != nil || parsed != id {
			return models.ErrMetadataFileReviewInvalid
		}
	}
	if !validAccountText(input.SourceField, 128, false) || len(input.Selections) > 128 {
		return models.ErrMetadataFileReviewInvalid
	}
	for name, selection := range input.Selections {
		id, err := archiveUUID(selection.UUID)
		if err != nil || id != selection.UUID || selection.Revision < 1 || !validAccountText(name, 1024, false) {
			return models.ErrMetadataFileReviewInvalid
		}
	}
	return nil
}

func metadataFileOwnerTable(kind models.ArchiveEntityKind) (string, string, error) {
	switch kind {
	case models.ArchiveScene:
		return "scenes_files", "scene_id", nil
	case models.ArchiveImage:
		return "images_files", "image_id", nil
	default:
		return "", "", models.ErrMetadataFileReviewInvalid
	}
}

// FileEdits walks only the selected entity's file matches. It does not scan
// catalogs or load every payload, and keeps different histories/matches visible.
func (s *MetadataFieldStore) FileEdits(ctx context.Context, entityUUID, afterHistory, afterMatch string, limit int) ([]models.MetadataFileEditCandidate, error) {
	entity, err := (&ArchiveEntityStore{}).Find(ctx, entityUUID)
	if err != nil {
		return nil, err
	}
	if entity == nil || entity.LocalID == nil || entity.State != models.ArchiveEntityActive {
		return nil, models.ErrMetadataFieldConflict
	}
	table, column, err := metadataFileOwnerTable(entity.Kind)
	if err != nil {
		return nil, err
	}
	limit, err = sourcePageLimit(limit)
	if err != nil || (afterHistory == "") != (afterMatch == "") {
		return nil, models.ErrMetadataFileReviewInvalid
	}
	if afterHistory != "" {
		if err := sourceFileIDs(&afterHistory, &afterMatch); err != nil {
			return nil, models.ErrMetadataFileReviewInvalid
		}
	}
	ret := []models.MetadataFileEditCandidate{}
	err = dbWrapper.Select(ctx, &ret, `SELECT h.uuid AS history_uuid, m.uuid AS match_uuid, h.collection_uuid, h.source_time,
 l.relative_path, m.file_uuid FROM `+table+` owner
 JOIN archive_entities f ON f.file_id=owner.file_id AND f.kind='file' AND f.state='active'
 JOIN source_file_matches m ON m.file_uuid=f.uuid
 JOIN source_file_history_locations l ON l.observation_uuid=m.observation_uuid
 JOIN source_file_history h ON h.uuid=l.history_uuid AND h.kind='metadata_edit'
 WHERE owner.`+column+`=? AND (h.uuid,m.uuid)>(?,?) ORDER BY h.uuid,m.uuid LIMIT ?`, *entity.LocalID, afterHistory, afterMatch, limit)
	return ret, err
}

func (s *MetadataFieldStore) PreviewFileEdit(ctx context.Context, input models.MetadataFileEditInput) (*models.MetadataFileEditPreview, error) {
	if err := validateMetadataFileEditInput(input); err != nil {
		return nil, err
	}
	history, err := (&SourceFileHistoryStore{}).Find(ctx, input.HistoryUUID)
	if err != nil {
		return nil, err
	}
	if history == nil || history.Kind != "metadata_edit" {
		return nil, models.ErrMetadataFileReviewInvalid
	}
	var edit *models.SourceFileEdit
	for i := range history.Edits {
		if history.Edits[i].Field == input.SourceField {
			edit = &history.Edits[i]
		}
	}
	if edit == nil {
		return nil, models.ErrMetadataFileReviewInvalid
	}
	match, err := sourceFileFind[models.SourceFileMatch](ctx, input.MatchUUID, "SELECT "+sourceFileMatchColumns+" FROM source_file_matches")
	if err != nil {
		return nil, err
	}
	if match == nil || len(history.Locations) != 1 || history.Locations[0].ObservationUUID == nil || *history.Locations[0].ObservationUUID != match.ObservationUUID {
		return nil, models.ErrMetadataFileReviewInvalid
	}
	// Retained matches can outlive a file or its archive. Revalidate both
	// generations and the original match basis before offering an editable choice.
	if err := (&SourceFileStore{}).validateMatch(ctx, match); err != nil {
		if errors.Is(err, models.ErrFileGenerationConflict) || errors.Is(err, models.ErrFileContentConflict) ||
			errors.Is(err, models.ErrFilePathChanged) || errors.Is(err, models.ErrSourceFileEvidenceInvalid) {
			// A previously ready choice can lose its file basis. Report the same
			// definitive stale-preview refusal as an entity/relation change so a
			// client can retire its rejected request and start a fresh review.
			return nil, errors.Join(models.ErrMetadataFieldConflict, err)
		}
		return nil, err
	}
	entity, err := (&ArchiveEntityStore{}).Find(ctx, input.EntityUUID)
	if err != nil {
		return nil, err
	}
	if entity == nil || entity.State != models.ArchiveEntityActive || entity.LocalID == nil {
		return nil, models.ErrMetadataFieldConflict
	}
	table, column, err := metadataFileOwnerTable(entity.Kind)
	if err != nil {
		return nil, err
	}
	var owns bool
	err = dbWrapper.Get(ctx, &owns, `SELECT EXISTS(SELECT 1 FROM `+table+` o JOIN archive_entities f ON f.file_id=o.file_id
 WHERE o.`+column+`=? AND f.uuid=? AND f.state='active')`, *entity.LocalID, match.FileUUID)
	if err != nil {
		return nil, err
	}
	if !owns {
		return nil, models.ErrMetadataFieldConflict
	}
	ret := &models.MetadataFileEditPreview{Input: input, EntityRevision: entity.Revision, Field: edit.TargetField,
		Mode: edit.Mode, FileUUID: match.FileUUID, Generation: match.Generation, ArchiveFileUUID: match.ArchiveFileUUID,
		ArchiveGeneration: match.ArchiveGeneration, Status: "ready", ReferenceRevisions: map[string]int{}}
	def, defErr := metadataFieldDefinition(entity.Kind, edit.TargetField)
	if edit.Mode == "unmapped" || defErr != nil {
		ret.Status = "unsupported"
	} else {
		current, err := s.State(ctx, entity.UUID, edit.TargetField)
		if err != nil {
			return nil, err
		}
		ret.CurrentValue, ret.CurrentMode, ret.CurrentOrigin, ret.Protected = current.Value, current.Mode, current.Origin, current.Protected
		if current.Decision != nil {
			ret.CurrentDecisionUUID = current.Decision.UUID
		}
		ret.Value = edit.Value
		switch {
		case edit.Mode == "inherit":
			// Removing an override releases its protection. Keep the current value
			// until a permitted native policy evaluates it; do not invent a fallback.
			ret.Value = current.Value
			for _, ref := range current.References {
				ret.ReferenceRevisions[ref.UUID] = ref.Revision
			}
		case edit.ValueType == "names":
			if err := s.previewFileEditNames(ctx, ret, def, edit.Value); err != nil {
				return nil, err
			}
		}
		if (edit.ValueType != "names" || edit.Mode == "inherit") && len(input.Selections) != 0 {
			return nil, models.ErrMetadataFileReviewInvalid
		}
		if ret.Status == "ready" {
			ret.Value, err = s.Normalize(ctx, entity.Kind, def.Name, ret.Value, ret.ReferenceRevisions)
			if err != nil {
				if errors.Is(err, models.ErrMetadataFieldConflict) {
					return nil, err
				}
				ret.Status, ret.Value = "unsupported", nil
			}
		}
	}
	ret.Digest, err = sourceSignature("stash-file-edit-preview-v1", ret)
	return ret, err
}

func fileEditSelectedName(ctx context.Context, kind models.ArchiveEntityKind, choice models.MetadataNameSelection) (*models.MetadataNameCandidate, error) {
	id, err := (&ArchiveEntityStore{}).Find(ctx, choice.UUID)
	if err != nil {
		return nil, err
	}
	if id == nil || id.State != models.ArchiveEntityActive || id.LocalID == nil || id.Kind != kind || id.Revision != choice.Revision {
		return nil, models.ErrMetadataFieldConflict
	}
	ret := &models.MetadataNameCandidate{UUID: id.UUID, LocalID: *id.LocalID, Revision: id.Revision}
	var query string
	switch kind {
	case models.ArchivePerformer:
		query = "SELECT " + performerPrimaryNameSQL + " AS name,coalesce(disambiguation,'') AS disambiguation FROM performers WHERE id=?"
	case models.ArchiveTag:
		query = "SELECT name FROM tags WHERE id=?"
	case models.ArchiveStudio:
		query = "SELECT name FROM studios WHERE id=?"
	case models.ArchiveGroup:
		query = "SELECT name FROM groups WHERE id=?"
	default:
		return nil, models.ErrMetadataFileReviewInvalid
	}
	if err := dbWrapper.Get(ctx, ret, query, *id.LocalID); err != nil {
		return nil, err
	}
	return ret, nil
}

func (s *MetadataFieldStore) previewFileEditNames(ctx context.Context, ret *models.MetadataFileEditPreview, def models.MetadataFieldDefinition, value json.RawMessage) error {
	var names []string
	if len(value) != 0 && value[0] == '"' {
		var name string
		if err := json.Unmarshal(value, &name); err != nil {
			return err
		}
		names = []string{name}
	} else if err := json.Unmarshal(value, &names); err != nil {
		return err
	}
	if len(names) > 128 {
		ret.Status, ret.Value = "unsupported", nil
		return nil
	}
	seenNames, seenIDs := map[string]bool{}, map[string]bool{}
	ids := []string{}
	for _, name := range names {
		if seenNames[name] {
			continue
		}
		seenNames[name] = true
		if !validAccountText(name, 1024, false) {
			ret.Status, ret.Value = "unsupported", nil
			return nil
		}
		candidates, err := s.NameCandidates(ctx, def.ReferenceKind, name)
		if err != nil {
			return err
		}
		match := models.MetadataNameMatch{Name: name, Candidates: candidates, More: len(candidates) > 100}
		if match.More {
			match.Candidates = match.Candidates[:100]
		}
		if choice, ok := ret.Input.Selections[name]; ok {
			match.Selected, err = fileEditSelectedName(ctx, def.ReferenceKind, choice)
			if err != nil {
				return err
			}
		} else if len(candidates) == 1 {
			match.Selected = &candidates[0]
		}
		if match.Selected == nil {
			ret.Status = "unresolved_names"
		} else {
			selected := match.Selected
			if !seenIDs[selected.UUID] {
				seenIDs[selected.UUID] = true
				ids = append(ids, selected.UUID)
				ret.ReferenceRevisions[selected.UUID] = selected.Revision
			}
		}
		ret.Names = append(ret.Names, match)
	}
	for name := range ret.Input.Selections {
		if !seenNames[name] {
			return models.ErrMetadataFileReviewInvalid
		}
	}
	ret.Value = nil
	if ret.Status != "ready" {
		return nil
	}
	var result any = ids
	switch def.Type {
	case "reference":
		if len(ids) != 1 {
			return models.ErrMetadataFileReviewInvalid
		}
		result = ids[0]
	case "groups":
		groups := []map[string]any{}
		for _, id := range ids {
			groups = append(groups, map[string]any{"uuid": id})
		}
		result = groups
	}
	var err error
	ret.Value, err = json.Marshal(result)
	return err
}

func metadataFileReviewRequest(input models.MetadataFileEditApplyInput) ([]byte, string, error) {
	if err := validateMetadataFileEditInput(input.MetadataFileEditInput); err != nil {
		return nil, "", err
	}
	id, err := archiveUUID(input.RequestUUID)
	if err != nil || id != input.RequestUUID || !archive.ValidSHA256(input.Digest) {
		return nil, "", models.ErrMetadataFileReviewInvalid
	}
	encoded, err := json.Marshal(input)
	if err != nil || len(encoded) > 262144 {
		return nil, "", models.ErrMetadataFileReviewInvalid
	}
	signature, err := sourceSignature("stash-file-edit-review-v1", input)
	return encoded, signature, err
}

func metadataFileReviewSignature(input models.MetadataFileEditApplyInput, decision string) (string, error) {
	return sourceSignature("stash-file-edit-receipt-v1", struct {
		Request      models.MetadataFileEditApplyInput `json:"request"`
		DecisionUUID string                            `json:"decision_uuid"`
	}{input, decision})
}

func (s *MetadataFieldStore) ApplyFileEdit(ctx context.Context, input models.MetadataFileEditApplyInput) (*models.MetadataFileEditReview, bool, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, false, err
	}
	encoded, signature, err := metadataFileReviewRequest(input)
	if err != nil {
		return nil, false, err
	}
	prior, err := s.FileEditReview(ctx, input.RequestUUID)
	if err != nil {
		return nil, false, err
	}
	if prior != nil {
		_, previous, err := metadataFileReviewRequest(prior.Request)
		if err != nil {
			return nil, false, err
		}
		if signature != previous {
			return nil, false, models.ErrMetadataFileReviewReplay
		}
		return prior, true, nil
	}
	preview, err := s.PreviewFileEdit(ctx, input.MetadataFileEditInput)
	if err != nil {
		return nil, false, err
	}
	if preview.Digest != input.Digest || (!input.KeepCurrent && preview.Status != "ready") {
		return nil, false, models.ErrMetadataFieldConflict
	}
	if input.KeepCurrent {
		return s.keepFileEdit(ctx, input, encoded, preview)
	}
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return models.ErrMetadataFileReviewInvalid
		}
		return nil
	})
	state, err := s.apply(ctx, models.MetadataFieldDecisionInput{EntityUUID: input.EntityUUID, ExpectedEntityRevision: preview.EntityRevision,
		Field: preview.Field, Mode: preview.Mode, Value: preview.Value, Origin: "review", Reason: metadataFileReviewReason,
		ReferenceRevisions: preview.ReferenceRevisions}, false, true)
	if err != nil {
		return nil, false, err
	}
	signature, err = metadataFileReviewSignature(input, state.Decision.UUID)
	if err != nil {
		return nil, false, err
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO metadata_file_edit_reviews(request_uuid,decision_uuid,history_uuid,source_field,match_uuid,request_json,signature)
 VALUES(?,?,?,?,?,?,?)`, input.RequestUUID, state.Decision.UUID, input.HistoryUUID, input.SourceField, input.MatchUUID, string(encoded), signature)
	if err != nil {
		return nil, false, err
	}
	ret, err := s.FileEditReview(ctx, input.RequestUUID)
	if err == nil && ret == nil {
		err = models.ErrSourcePayloadCorrupt
	}
	complete = err == nil
	return ret, false, err
}

func (s *MetadataFieldStore) FileEditReview(ctx context.Context, id string) (*models.MetadataFileEditReview, error) {
	if _, err := archiveUUID(id); err != nil {
		return nil, models.ErrMetadataFileReviewInvalid
	}
	get := func(dest any, query string, args ...any) error { return dbWrapper.Get(ctx, dest, query, args...) }
	var count int
	if err := get(&count, `SELECT (SELECT count(*) FROM metadata_file_edit_reviews WHERE request_uuid=?)+(SELECT count(*) FROM metadata_file_edit_keeps WHERE request_uuid=?)`, id, id); err != nil {
		return nil, err
	}
	if count > 1 {
		return nil, models.ErrSourcePayloadCorrupt
	}
	ret, err := readMetadataFileReview(get, id)
	if err != nil || ret != nil {
		return ret, err
	}
	return readMetadataFileKeep(get, id)
}

type metadataFileReviewRow struct {
	RequestUUID  string    `db:"request_uuid"`
	DecisionUUID string    `db:"decision_uuid"`
	HistoryUUID  string    `db:"history_uuid"`
	SourceField  string    `db:"source_field"`
	MatchUUID    string    `db:"match_uuid"`
	RequestJSON  string    `db:"request_json"`
	Signature    string    `db:"signature"`
	CreatedAt    Timestamp `db:"created_at"`
}

func readMetadataFileReview(get enrichmentGet, id string) (*models.MetadataFileEditReview, error) {
	var row metadataFileReviewRow
	if err := get(&row, "SELECT * FROM metadata_file_edit_reviews WHERE request_uuid=?", id); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var input models.MetadataFileEditApplyInput
	if err := json.Unmarshal([]byte(row.RequestJSON), &input); err != nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	encoded, _, err := metadataFileReviewRequest(input)
	signature, signatureErr := metadataFileReviewSignature(input, row.DecisionUUID)
	if input.KeepCurrent || err != nil || signatureErr != nil || string(encoded) != row.RequestJSON || signature != row.Signature || input.RequestUUID != row.RequestUUID ||
		input.HistoryUUID != row.HistoryUUID || input.SourceField != row.SourceField || input.MatchUUID != row.MatchUUID {
		return nil, models.ErrSourcePayloadCorrupt
	}
	var valid bool
	err = get(&valid, `SELECT EXISTS(SELECT 1 FROM source_file_history_edits e
 JOIN source_file_history_locations l ON l.history_uuid=e.history_uuid
 JOIN source_file_matches m ON m.observation_uuid=l.observation_uuid
 JOIN metadata_field_decisions d ON d.uuid=?
 WHERE e.history_uuid=? AND e.field=? AND m.uuid=? AND d.field=e.target_field AND d.mode=e.mode
 AND d.origin='review' AND d.capture_uuid IS NULL AND d.sealed=1 AND d.reason=?)`, row.DecisionUUID, row.HistoryUUID, row.SourceField, row.MatchUUID, metadataFileReviewReason)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, models.ErrSourcePayloadCorrupt
	}
	var source struct {
		Field    string `db:"target_field"`
		Mode     string `db:"mode"`
		Type     string `db:"value_type"`
		Value    string `db:"value_json"`
		Selected string `db:"selected"`
	}
	if err := get(&source, `SELECT e.target_field,e.mode,e.value_type,e.value_json,d.value_json AS selected
 FROM source_file_history_edits e JOIN metadata_field_decisions d ON d.uuid=? WHERE e.history_uuid=? AND e.field=?`, row.DecisionUUID, row.HistoryUUID, row.SourceField); err != nil {
		return nil, err
	}
	if source.Mode == "set" && source.Type != "names" {
		def, err := metadataFieldDefinition(models.ArchiveScene, source.Field)
		if err != nil {
			return nil, models.ErrSourcePayloadCorrupt
		}
		var value json.RawMessage
		if def.Type == "urls" {
			// URL normalization is pure; this branch never reads a transaction.
			value, _, err = normalizeMetadataCollection(context.Background(), def, []byte(source.Value), nil)
		} else {
			value, _, err = normalizeMetadataValue(def, []byte(source.Value))
		}
		if err != nil || !bytes.Equal(value, []byte(source.Selected)) {
			return nil, models.ErrSourcePayloadCorrupt
		}
	}
	// UUID adoption may retarget a decision's FK; retain and resolve the exact
	// UUID submitted with the original request instead of rewriting its receipt.
	var entityUUID string
	if err := get(&entityUUID, "SELECT entity_uuid FROM metadata_field_decisions WHERE uuid=?", row.DecisionUUID); err != nil {
		return nil, err
	}
	var field string
	if err := get(&field, "SELECT field FROM metadata_field_decisions WHERE uuid=?", row.DecisionUUID); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for expected := input.EntityUUID; expected != entityUUID; {
		if seen[expected] || len(seen) >= 1024 {
			return nil, models.ErrSourcePayloadCorrupt
		}
		seen[expected] = true
		var next sql.NullString
		if err := get(&next, "SELECT redirect_to FROM archive_entities WHERE uuid=?", expected); err != nil || !next.Valid {
			return nil, models.ErrSourcePayloadCorrupt
		}
		expected = next.String
	}
	return &models.MetadataFileEditReview{RequestUUID: id, DecisionUUID: row.DecisionUUID, Field: field, Request: input, CreatedAt: row.CreatedAt.Timestamp}, nil
}
