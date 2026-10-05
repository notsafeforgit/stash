package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
)

type metadataPolicyDraftRequest struct {
	CollectionUUID             string                          `json:"collection_uuid"`
	ExpectedCollectionRevision int                             `json:"expected_collection_revision"`
	ExpectedPolicyRevision     int                             `json:"expected_policy_revision"`
	EntityUUID                 string                          `json:"entity_uuid"`
	FileUUID                   string                          `json:"file_uuid"`
	Source                     *metadata.Source                `json:"source,omitempty"`
	Definition                 models.MetadataPolicyDefinition `json:"definition"`
	Event                      string                          `json:"event"`
	IncludeData                bool                            `json:"include_data"`
}

func policySampleScope(r *http.Request) (models.MetadataPolicySampleScope, int, error) {
	scope := models.MetadataPolicySampleScope{CollectionUUID: chi.URLParam(r, "collection"), EntityUUID: chi.URLParam(r, "entity")}
	var err error
	scope.CollectionRevision, err = strconv.Atoi(r.URL.Query().Get("collection_revision"))
	if err != nil || scope.CollectionRevision < 1 || !ingest.ValidUUID(scope.CollectionUUID) || !ingest.ValidUUID(scope.EntityUUID) {
		return scope, 0, ingest.ErrInvalid
	}
	limit := 25
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			return scope, 0, ingest.ErrInvalid
		}
	}
	return scope, limit, nil
}

func (rs *nativeArchiveRoutes) policySampleFiles(w http.ResponseWriter, r *http.Request) {
	scope, limit, err := policySampleScope(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	var result []models.MetadataPolicySampleFile
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.MetadataPolicy.SampleFiles(ctx, scope, r.URL.Query().Get("after"), limit)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) policySampleSources(w http.ResponseWriter, r *http.Request) {
	scope, limit, err := policySampleScope(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	var after *models.MetadataPolicySourceCursor
	if capture, attachment := r.URL.Query().Get("after_capture"), r.URL.Query().Get("after_attachment"); capture != "" || attachment != "" {
		after = &models.MetadataPolicySourceCursor{CaptureUUID: capture, AttachmentUUID: attachment}
	}
	var result []models.MetadataPolicySampleSource
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.MetadataPolicy.SampleSources(ctx, scope, after, limit)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

type metadataPolicyReference struct {
	RequestedUUID  string                  `json:"requested_uuid"`
	Entity         *metadataEntityIdentity `json:"entity"`
	Name           string                  `json:"name"`
	Disambiguation string                  `json:"disambiguation"`
}

// Resolve a bounded page of stored constant references for display, including
// merge redirects. Reading names must not rewrite the reviewed definition.
func (rs *nativeArchiveRoutes) policyReferences(w http.ResponseWriter, r *http.Request) {
	var request struct {
		UUIDs []string `json:"uuids"`
	}
	if err := readIngestJSON(w, r, 8192, &request); err != nil {
		ingestError(w, err)
		return
	}
	if len(request.UUIDs) > 100 {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	for _, id := range request.UUIDs {
		if !ingest.ValidUUID(id) {
			ingestError(w, ingest.ErrInvalid)
			return
		}
	}
	result := make([]metadataPolicyReference, 0, len(request.UUIDs))
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		for _, id := range request.UUIDs {
			item := metadataPolicyReference{RequestedUUID: id}
			entity, err := rs.repo.ArchiveEntity.Resolve(ctx, id)
			if err != nil {
				return err
			}
			if entity != nil && entity.State == models.ArchiveEntityActive && entity.LocalID != nil {
				identity := metadataIdentity(entity)
				switch entity.Kind {
				case models.ArchivePerformer:
					value, err := rs.repo.Performer.Find(ctx, *entity.LocalID)
					if err != nil {
						return err
					}
					if value != nil {
						item.Entity, item.Name, item.Disambiguation = &identity, value.Name, value.Disambiguation
					}
				case models.ArchiveStudio:
					value, err := rs.repo.Studio.Find(ctx, *entity.LocalID)
					if err != nil {
						return err
					}
					if value != nil {
						item.Entity, item.Name = &identity, value.Name
					}
				case models.ArchiveTag:
					value, err := rs.repo.Tag.Find(ctx, *entity.LocalID)
					if err != nil {
						return err
					}
					if value != nil {
						item.Entity, item.Name = &identity, value.Name
					}
				case models.ArchiveGroup:
					value, err := rs.repo.Group.Find(ctx, *entity.LocalID)
					if err != nil {
						return err
					}
					if value != nil {
						item.Entity, item.Name = &identity, value.Name
					}
				}
			}
			result = append(result, item)
		}
		return nil
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) previewPolicyDraft(w http.ResponseWriter, r *http.Request) {
	var request metadataPolicyDraftRequest
	if err := readIngestJSON(w, r, 147456, &request); err != nil {
		ingestError(w, err)
		return
	}
	if request.ExpectedCollectionRevision < 1 || request.ExpectedPolicyRevision < 0 || (request.Event != "create" && request.Event != "existing") {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result *metadata.DraftPreview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		input, err := rs.policyInput(ctx, metadataPreviewRequest{CollectionUUID: request.CollectionUUID, EntityUUID: request.EntityUUID, FileUUID: request.FileUUID, Source: request.Source})
		if err != nil {
			return err
		}
		if input.CollectionRevision != request.ExpectedCollectionRevision || input.PolicyRevision != request.ExpectedPolicyRevision {
			return models.ErrMetadataPolicyConflict
		}
		input.Created = request.Event == "create"
		result, err = (metadata.Service{Repo: rs.repo}).PreviewDraft(ctx, input, request.Definition)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	if !request.IncludeData {
		result.Data = nil
	}
	ingestJSON(w, http.StatusOK, result)
}
