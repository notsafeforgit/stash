package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type metadataFileKeepRow struct {
	RequestUUID      string         `db:"request_uuid"`
	EntityUUID       string         `db:"entity_uuid"`
	EntityRevision   int            `db:"entity_revision"`
	HistoryUUID      string         `db:"history_uuid"`
	SourceField      string         `db:"source_field"`
	MatchUUID        string         `db:"match_uuid"`
	SelectedDecision sql.NullString `db:"selected_decision_uuid"`
	RequestJSON      string         `db:"request_json"`
	Signature        string         `db:"signature"`
	CreatedAt        Timestamp      `db:"created_at"`
}

func metadataFileKeepSignature(input models.MetadataFileEditApplyInput, revision int, selected string) (string, error) {
	return sourceSignature("stash-file-edit-keep-receipt-v1", struct {
		Request          models.MetadataFileEditApplyInput `json:"request"`
		EntityRevision   int                               `json:"entity_revision"`
		SelectedDecision string                            `json:"selected_decision"`
	}{input, revision, selected})
}

func (s *MetadataFieldStore) keepFileEdit(ctx context.Context, input models.MetadataFileEditApplyInput, encoded []byte, preview *models.MetadataFileEditPreview) (*models.MetadataFileEditReview, bool, error) {
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return models.ErrMetadataFileReviewInvalid
		}
		return nil
	})
	signature, err := metadataFileKeepSignature(input, preview.EntityRevision, preview.CurrentDecisionUUID)
	if err != nil {
		return nil, false, err
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO metadata_file_edit_keeps(request_uuid,entity_uuid,entity_revision,history_uuid,source_field,match_uuid,selected_decision_uuid,request_json,signature)
VALUES(?,?,?,?,?,?,nullif(?,''),?,?)`, input.RequestUUID, input.EntityUUID, preview.EntityRevision, input.HistoryUUID, input.SourceField, input.MatchUUID, preview.CurrentDecisionUUID, string(encoded), signature)
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

func readMetadataFileKeep(get enrichmentGet, id string) (*models.MetadataFileEditReview, error) {
	var row metadataFileKeepRow
	if err := get(&row, "SELECT * FROM metadata_file_edit_keeps WHERE request_uuid=?", id); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var input models.MetadataFileEditApplyInput
	if err := json.Unmarshal([]byte(row.RequestJSON), &input); err != nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	encoded, _, err := metadataFileReviewRequest(input)
	signature, signatureErr := metadataFileKeepSignature(input, row.EntityRevision, row.SelectedDecision.String)
	if !input.KeepCurrent || err != nil || signatureErr != nil || string(encoded) != row.RequestJSON || signature != row.Signature || input.RequestUUID != row.RequestUUID || input.HistoryUUID != row.HistoryUUID || input.SourceField != row.SourceField || input.MatchUUID != row.MatchUUID {
		return nil, models.ErrSourcePayloadCorrupt
	}
	var binding struct {
		Field    string `db:"target_field"`
		Kind     string `db:"kind"`
		Revision int    `db:"revision"`
	}
	err = get(&binding, `SELECT e.target_field,a.kind,a.revision FROM source_file_history_edits e
JOIN source_file_history_locations l ON l.history_uuid=e.history_uuid
JOIN source_file_matches m ON m.observation_uuid=l.observation_uuid
JOIN archive_entities a ON a.uuid=?
WHERE e.history_uuid=? AND e.field=? AND m.uuid=?`, row.EntityUUID, row.HistoryUUID, row.SourceField, row.MatchUUID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	if err != nil {
		return nil, err
	}
	if (binding.Kind != "scene" && binding.Kind != "image") || binding.Revision < row.EntityRevision {
		return nil, models.ErrSourcePayloadCorrupt
	}
	if row.SelectedDecision.Valid {
		var valid bool
		if err := get(&valid, `SELECT EXISTS(SELECT 1 FROM metadata_field_decisions WHERE uuid=? AND entity_uuid=? AND field=?)`, row.SelectedDecision.String, row.EntityUUID, binding.Field); err != nil {
			return nil, err
		}
		if !valid {
			return nil, models.ErrSourcePayloadCorrupt
		}
	}
	// Adoption retargets the foreign key. The original request remains intact;
	// later merges/deletion do not invalidate a receipt or reapply a choice.
	seen := map[string]bool{}
	for expected := input.EntityUUID; expected != row.EntityUUID; {
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
	return &models.MetadataFileEditReview{RequestUUID: id, KeptCurrent: true, Field: binding.Field, Request: input, CreatedAt: row.CreatedAt.Timestamp}, nil
}

func validateMetadataFileKeepSchema(conn *sqlx.DB) error {
	for _, name := range []string{"metadata_file_edit_keeps", "metadata_file_edit_keeps_history", "metadata_file_edit_keeps_entity", "metadata_file_edit_keep_immutable", "metadata_file_edit_keep_request_distinct", "metadata_file_edit_apply_request_distinct", "metadata_file_edit_keep_scope"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var duplicate bool
	if err := conn.Get(&duplicate, `SELECT EXISTS(SELECT 1 FROM metadata_file_edit_keeps k JOIN metadata_file_edit_reviews a ON a.request_uuid=k.request_uuid)`); err != nil {
		return err
	}
	if duplicate {
		return models.ErrSourcePayloadCorrupt
	}
	for after := ""; ; {
		var id string
		if err := conn.Get(&id, "SELECT coalesce(min(request_uuid),'') FROM metadata_file_edit_keeps WHERE request_uuid>?", after); err != nil || id == "" {
			return err
		}
		if _, err := readMetadataFileKeep(conn.Get, id); err != nil {
			return fmt.Errorf("historical metadata keep %s: %w", id, err)
		}
		after = id
	}
}
