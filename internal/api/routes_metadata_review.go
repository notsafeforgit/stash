package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
)

type metadataEntityIdentity struct {
	UUID     string                   `json:"uuid"`
	Kind     models.ArchiveEntityKind `json:"kind"`
	Revision int                      `json:"revision"`
	LocalID  *int                     `json:"local_id,omitempty"`
}

func metadataIdentity(entity *models.ArchiveEntity) metadataEntityIdentity {
	return metadataEntityIdentity{entity.UUID, entity.Kind, entity.Revision, entity.LocalID}
}

// Existing library URLs retain their integer IDs. Resolve them once at the
// application boundary, then use native identities for review and mutation.
func (rs *nativeArchiveRoutes) metadataEntity(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "localID"))
	kind := models.ArchiveEntityKind(chi.URLParam(r, "kind"))
	validKind := false
	switch kind {
	case models.ArchiveScene, models.ArchiveImage, models.ArchiveGallery, models.ArchivePerformer, models.ArchiveStudio, models.ArchiveTag, models.ArchiveGroup, models.ArchiveFile:
		validKind = true
	}
	if err != nil || id < 1 || !validKind {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var entity *models.ArchiveEntity
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		entity, err = rs.repo.ArchiveEntity.FindByLocalID(ctx, kind, id)
		return err
	})
	if err == nil && (entity == nil || entity.State != models.ArchiveEntityActive) {
		err = ingest.ErrNotFound
	}
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, metadataIdentity(entity))
}

type metadataFieldSummary struct {
	Definition models.MetadataFieldDefinition `json:"definition"`
	Value      json.RawMessage                `json:"value"`
	Mode       string                         `json:"mode"`
	Origin     string                         `json:"origin"`
	Protected  bool                           `json:"protected"`
	Decision   *metadataDecisionProvenance    `json:"decision,omitempty"`
	References []metadataEntityIdentity       `json:"references,omitempty"`
}

// The selected value, field, mode and entity already appear in the summary.
// Full decision values are returned only when requesting field history.
type metadataDecisionProvenance struct {
	UUID        string                         `json:"uuid"`
	Sequence    int                            `json:"sequence"`
	CaptureUUID *string                        `json:"capture_uuid,omitempty"`
	Reason      string                         `json:"reason"`
	CreatedAt   time.Time                      `json:"created_at"`
	Policy      *models.MetadataPolicyRef      `json:"policy,omitempty"`
	FileEdit    *models.MetadataFileEditReview `json:"file_edit,omitempty"`
}

func (rs *nativeArchiveRoutes) entityMetadataFields(w http.ResponseWriter, r *http.Request) {
	if !ingest.ValidUUID(chi.URLParam(r, "entity")) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result struct {
		Entity metadataEntityIdentity `json:"entity"`
		Fields []metadataFieldSummary `json:"fields"`
	}
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		entity, err := rs.repo.ArchiveEntity.Find(ctx, chi.URLParam(r, "entity"))
		if err != nil {
			return err
		}
		if entity == nil || entity.State != models.ArchiveEntityActive || entity.LocalID == nil {
			return ingest.ErrNotFound
		}
		definitions := models.MetadataFields(entity.Kind)
		if definitions == nil {
			return ingest.ErrInvalid
		}
		result.Entity = metadataIdentity(entity)
		for _, def := range definitions {
			state, err := rs.repo.MetadataField.State(ctx, entity.UUID, def.Name)
			if err != nil {
				return err
			}
			field := metadataFieldSummary{Definition: def, Value: state.Value, Mode: state.Mode, Origin: state.Origin, Protected: state.Protected}
			if decision := state.Decision; decision != nil {
				field.Decision = &metadataDecisionProvenance{UUID: decision.UUID, Sequence: decision.Sequence, CaptureUUID: decision.CaptureUUID,
					Reason: decision.Reason, CreatedAt: decision.CreatedAt, Policy: decision.Policy, FileEdit: decision.FileEdit}
			}
			for _, ref := range state.References {
				field.References = append(field.References, metadataIdentity(ref))
			}
			result.Fields = append(result.Fields, field)
		}
		return nil
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) entityMetadataHistory(w http.ResponseWriter, r *http.Request) {
	if !ingest.ValidUUID(chi.URLParam(r, "entity")) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	query := r.URL.Query()
	after, limit := 0, 50
	for key, dest := range map[string]*int{"after": &after, "limit": &limit} {
		if value := query.Get(key); value != "" {
			var err error
			*dest, err = strconv.Atoi(value)
			if err != nil || *dest < 0 {
				ingestError(w, ingest.ErrInvalid)
				return
			}
		}
	}
	if limit < 1 || limit > 100 {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result []models.MetadataFieldDecision
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		entity, err := rs.repo.ArchiveEntity.Find(ctx, chi.URLParam(r, "entity"))
		if err != nil {
			return err
		}
		if entity == nil {
			return ingest.ErrNotFound
		}
		validField := false
		for _, field := range models.MetadataFields(entity.Kind) {
			validField = validField || field.Name == chi.URLParam(r, "field")
		}
		if !validField {
			return ingest.ErrInvalid
		}
		result, err = rs.repo.MetadataField.History(ctx, chi.URLParam(r, "entity"), chi.URLParam(r, "field"), after, limit)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) entityFileEdits(w http.ResponseWriter, r *http.Request) {
	if !ingest.ValidUUID(chi.URLParam(r, "entity")) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	query, limit := r.URL.Query(), 50
	if value := query.Get("limit"); value != "" {
		var err error
		limit, err = strconv.Atoi(value)
		if err != nil {
			ingestError(w, ingest.ErrInvalid)
			return
		}
	}
	if limit < 1 || limit > 100 {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result []models.MetadataFileEditCandidate
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.MetadataField.FileEdits(ctx, chi.URLParam(r, "entity"), query.Get("after_history"), query.Get("after_match"), limit)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) previewFileEdit(w http.ResponseWriter, r *http.Request) {
	var input models.MetadataFileEditInput
	if err := readIngestJSON(w, r, 262144, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.MetadataFileEditPreview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.MetadataField.PreviewFileEdit(ctx, input)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) applyFileEdit(w http.ResponseWriter, r *http.Request) {
	var input models.MetadataFileEditApplyInput
	if err := readIngestJSON(w, r, 262144, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result struct {
		Review   *models.MetadataFileEditReview `json:"review"`
		Replayed bool                           `json:"replayed"`
	}
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result.Review, result.Replayed, err = rs.repo.MetadataField.ApplyFileEdit(ctx, input)
		if err == nil && !result.Replayed && !result.Review.KeptCurrent && rs.notifyMetadata != nil {
			err = rs.notifyMetadata(ctx, metadata.Input{EntityUUID: input.EntityUUID}, []string{result.Review.Field})
		}
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) fileEditReview(w http.ResponseWriter, r *http.Request) {
	var result *models.MetadataFileEditReview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.MetadataField.FileEditReview(ctx, chi.URLParam(r, "request"))
		return err
	})
	if err == nil && result == nil {
		err = ingest.ErrNotFound
	}
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
