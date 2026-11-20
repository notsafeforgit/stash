package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

type metadataEntityIdentity struct {
	UUID     string                   `json:"uuid"`
	Kind     models.ArchiveEntityKind `json:"kind"`
	Revision int                      `json:"revision"`
	LocalID  *int                     `json:"-"`
}

func metadataIdentity(entity *models.ArchiveEntity) metadataEntityIdentity {
	return metadataEntityIdentity{entity.UUID, entity.Kind, entity.Revision, entity.LocalID}
}

// Resolve UUIDs (including merge redirects) or current local keys at the
// application boundary. Historical original_id values never identify a record.
func (rs *nativeArchiveRoutes) metadataEntity(w http.ResponseWriter, r *http.Request) {
	value := chi.URLParam(r, "identifier")
	id, idErr := strconv.Atoi(value)
	isUUID := ingest.ValidUUID(value)
	kind := models.ArchiveEntityKind(chi.URLParam(r, "kind"))
	validKind := false
	switch kind {
	case models.ArchiveScene, models.ArchiveImage, models.ArchiveGallery, models.ArchivePerformer, models.ArchiveStudio, models.ArchiveTag, models.ArchiveGroup, models.ArchiveFile, models.ArchiveFolder, models.ArchiveSceneMarker, models.ArchiveGalleryChapter, models.ArchiveSavedFilter:
		validKind = true
	}
	if (!isUUID && (idErr != nil || id < 1)) || !validKind {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var entity *models.ArchiveEntity
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		if isUUID {
			entity, err = rs.repo.ArchiveEntity.Find(ctx, value)
			if err != nil || entity == nil {
				return err
			}
			if entity.Kind != kind {
				return ingest.ErrNotFound
			}
			entity, err = rs.repo.ArchiveEntity.Resolve(ctx, value)
			return err
		}
		entity, err = rs.repo.ArchiveEntity.FindByLocalID(ctx, kind, id)
		return err
	})
	if err == nil && (entity == nil || entity.State != models.ArchiveEntityActive || entity.Kind != kind || entity.LocalID == nil) {
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
// The current value is returned once per field; decision metadata carries its provenance.
type metadataDecisionProvenance struct {
	UUID        string                    `json:"uuid"`
	Sequence    int                       `json:"sequence"`
	CaptureUUID *string                   `json:"capture_uuid,omitempty"`
	Reason      string                    `json:"reason"`
	CreatedAt   time.Time                 `json:"created_at"`
	Policy      *models.MetadataPolicyRef `json:"policy,omitempty"`
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
					Reason: decision.Reason, CreatedAt: decision.CreatedAt, Policy: decision.Policy}
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
