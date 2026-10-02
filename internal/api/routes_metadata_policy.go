package api

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
)

type nativeArchiveRoutes struct {
	repo           models.Repository
	notifyMetadata func(context.Context, metadata.Input, []string) error
	albums         *gallery.AlbumBackfill
}

// This router is mounted behind application authentication. Producer bearer
// tokens grant no access to collection configuration or library metadata edits.
func (rs *nativeArchiveRoutes) router() http.Handler {
	r := chi.NewRouter()
	r.Use(nativeAdminOrigin)
	r.Get("/metadata-fields/{kind}", rs.fields)
	r.Get("/collections/{collection}/metadata-policy", rs.policy)
	r.Put("/collections/{collection}/metadata-policy", rs.putPolicy)
	r.Get("/collections/{collection}/metadata-policy/history", rs.policyHistory)
	r.Post("/metadata-policy/preview", rs.preview)
	r.Post("/metadata-policy/apply", rs.apply)
	r.Get("/media-roots", rs.roots)
	r.Post("/media-roots", rs.putRoot)
	r.Put("/media-roots/{root}", rs.putRoot)
	r.Get("/collections", rs.collections)
	r.Post("/collections", rs.putCollection)
	r.Put("/collections/{collection}", rs.putCollection)
	r.Get("/collections/{collection}/post-memberships", rs.collectionPostMemberships)
	r.Get("/posts/{post}/collection-memberships", rs.postCollectionMemberships)
	r.Get("/album-backfill-posts", rs.albumPosts)
	r.Post("/posts/{post}/album-backfill/preview", rs.previewAlbum)
	r.Post("/posts/{post}/album-backfills", rs.applyAlbum)
	r.Get("/posts/{post}/album-backfills", rs.albumHistory)
	r.Get("/album-backfills/{job}", rs.albumJob)
	r.Get("/album-backfills/{job}/attempts", rs.albumAttempts)
	r.Post("/album-backfills/{job}/cancel", rs.cancelAlbum)
	r.Post("/album-backfills/{job}/retry", rs.retryAlbum)
	r.Get("/album-backfill-requests/{request}", rs.albumRequest)
	r.Post("/backfills/import", rs.importBackfills)
	r.Post("/backfills/status", rs.backfillStatus)
	r.Get("/backfills/{decision}", rs.backfill)
	r.Post("/scan-journals/import", rs.importScanJournal)
	r.Post("/scan-journal-activations/preview", rs.previewScanActivation)
	r.Post("/scan-journal-activations", rs.activateScanJournal)
	r.Get("/scan-journal-activations/{activation}", rs.scanActivation)
	r.Get("/scan-journals/{journal}", rs.scanJournal)
	r.Get("/scan-journals/{journal}/records", rs.scanJournalRecords)
	r.Get("/scan-journal-records/{record}", rs.scanJournalRecord)
	r.Post("/catalog-identity-imports/preview", rs.previewCatalogIdentityImport)
	r.Post("/catalog-identity-imports", rs.applyCatalogIdentityImport)
	r.Get("/catalog-identity-imports/{import}", rs.catalogIdentityImport)
	r.Get("/catalog-identity-imports/{import}/records", rs.catalogIdentityImportRecords)
	r.Post("/catalog-registry-imports/preview", rs.previewCatalogRegistryImport)
	r.Post("/catalog-registry-imports", rs.applyCatalogRegistryImport)
	r.Get("/catalog-registry-imports/{import}", rs.catalogRegistryImport)
	r.Get("/catalog-registry-imports/{import}/records", rs.catalogRegistryImportRecords)
	r.Post("/catalog-snapshots", rs.beginCatalogSnapshot)
	r.Get("/catalog-snapshots/{snapshot}", rs.catalogSnapshot)
	r.Put("/catalog-snapshots/{snapshot}/chunks/{chunk}", rs.receiveCatalogSnapshotChunk)
	r.Get("/catalog-snapshots/{snapshot}/evidence-import", rs.catalogEvidenceImport)
	r.Post("/catalog-snapshots/{snapshot}/evidence-import", rs.advanceCatalogEvidenceImport)
	r.Get("/catalog-snapshots/{snapshot}/evidence-import/records", rs.catalogEvidenceRecords)
	r.Get("/catalog-snapshots/{snapshot}/relations-import", rs.catalogRelationsImport)
	r.Post("/catalog-snapshots/{snapshot}/relations-import", rs.advanceCatalogRelationsImport)
	r.Get("/catalog-snapshots/{snapshot}/relations-import/records", rs.catalogRelationsRecords)
	r.Get("/catalog-snapshots/{snapshot}/relations-import/records/{ordinal}", rs.catalogRelationRecord)
	r.Get("/catalog-snapshots/{snapshot}/publisher-import", rs.catalogPublisherImport)
	r.Post("/catalog-snapshots/{snapshot}/publisher-import", rs.advanceCatalogPublisherImport)
	r.Get("/catalog-snapshots/{snapshot}/publisher-import/records", rs.catalogPublisherRecords)
	r.Get("/catalog-snapshots/{snapshot}/publisher-import/records/{ordinal}", rs.catalogPublisherRecord)
	r.Get("/catalog-snapshots/{snapshot}/attachment-import", rs.catalogAttachmentImport)
	r.Post("/catalog-snapshots/{snapshot}/attachment-import", rs.advanceCatalogAttachmentImport)
	r.Get("/catalog-snapshots/{snapshot}/attachment-import/records", rs.catalogAttachmentRecords)
	r.Get("/catalog-snapshots/{snapshot}/attachment-import/records/{ordinal}", rs.catalogAttachmentRecord)
	r.Get("/catalog-snapshots/{snapshot}/membership-import", rs.catalogMembershipImport)
	r.Post("/catalog-snapshots/{snapshot}/membership-import", rs.advanceCatalogMembershipImport)
	r.Get("/catalog-snapshots/{snapshot}/membership-import/records", rs.catalogMembershipRecords)
	r.Get("/catalog-snapshots/{snapshot}/membership-import/records/{ordinal}", rs.catalogMembershipRecord)
	r.Get("/catalog-snapshots/{snapshot}/media-import", rs.catalogMediaImport)
	r.Post("/catalog-snapshots/{snapshot}/media-import", rs.beginCatalogMediaImport)
	r.Post("/catalog-snapshots/{snapshot}/media-import/advance", rs.advanceCatalogMediaImport)
	r.Get("/catalog-snapshots/{snapshot}/media-import/records", rs.catalogMediaRecords)
	r.Get("/catalog-snapshots/{snapshot}/media-import/records/{ordinal}", rs.catalogMediaRecord)
	return r
}

func nativeArchiveError(w http.ResponseWriter, err error) {
	if errors.Is(err, models.ErrMetadataPolicyInvalid) {
		ingestJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_policy", "message": err.Error()})
		return
	}
	if errors.Is(err, models.ErrMetadataPolicyConflict) || errors.Is(err, models.ErrMetadataFieldConflict) || errors.Is(err, models.ErrSourceDefinitionConflict) {
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "preview_changed", "message": "The collection, policy, or metadata changed; load a fresh preview."})
		return
	}
	ingestError(w, err)
}

func (rs *nativeArchiveRoutes) fields(w http.ResponseWriter, r *http.Request) {
	fields := models.MetadataFields(models.ArchiveEntityKind(chi.URLParam(r, "kind")))
	if fields == nil {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	ingestJSON(w, http.StatusOK, fields)
}

func (rs *nativeArchiveRoutes) policy(w http.ResponseWriter, r *http.Request) {
	var result *models.MetadataPolicy
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.MetadataPolicy.Find(ctx, chi.URLParam(r, "collection"))
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) putPolicy(w http.ResponseWriter, r *http.Request) {
	var input models.MetadataPolicyInput
	if err := readIngestJSON(w, r, 135168, &input); err != nil {
		ingestError(w, err)
		return
	}
	input.CollectionUUID, input.Origin = chi.URLParam(r, "collection"), "review"
	if !ingest.ValidUUID(input.CollectionUUID) || metadata.ValidateDefinition(input.Definition) != nil {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result *models.MetadataPolicy
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.MetadataPolicy.Put(ctx, input)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) policyHistory(w http.ResponseWriter, r *http.Request) {
	after := 0
	if value := r.URL.Query().Get("after"); value != "" {
		var err error
		after, err = strconv.Atoi(value)
		if err != nil || after < 0 {
			ingestError(w, ingest.ErrInvalid)
			return
		}
	}
	var result []*models.MetadataPolicy
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.MetadataPolicy.History(ctx, chi.URLParam(r, "collection"), after, 50)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

type metadataPreviewRequest struct {
	CollectionUUID string           `json:"collection_uuid"`
	EntityUUID     string           `json:"entity_uuid"`
	FileUUID       string           `json:"file_uuid"`
	Source         *metadata.Source `json:"source,omitempty"`
	IncludeData    bool             `json:"include_data"`
	Digest         string           `json:"digest,omitempty"`
}

func (rs *nativeArchiveRoutes) policyInput(ctx context.Context, request metadataPreviewRequest) (metadata.Input, error) {
	var ret metadata.Input
	if !ingest.ValidUUID(request.CollectionUUID) || !ingest.ValidUUID(request.EntityUUID) || !ingest.ValidUUID(request.FileUUID) {
		return ret, ingest.ErrInvalid
	}
	entity, err := rs.repo.ArchiveEntity.Find(ctx, request.EntityUUID)
	if err != nil {
		return ret, err
	}
	file, err := rs.repo.ArchiveEntity.Find(ctx, request.FileUUID)
	if err != nil {
		return ret, err
	}
	collection, err := rs.repo.SourceCollection.Find(ctx, request.CollectionUUID)
	if err != nil {
		return ret, err
	}
	if entity == nil || entity.State != models.ArchiveEntityActive || entity.LocalID == nil || file == nil || file.State != models.ArchiveEntityActive || file.Kind != models.ArchiveFile || file.LocalID == nil || collection == nil || collection.RootUUID == nil {
		return ret, ingest.ErrInvalid
	}
	var files []models.File
	switch entity.Kind {
	case models.ArchiveScene:
		videos, err := rs.repo.Scene.GetFiles(ctx, *entity.LocalID)
		if err != nil {
			return ret, err
		}
		for _, video := range videos {
			files = append(files, video)
		}
	case models.ArchiveImage:
		files, err = rs.repo.Image.GetFiles(ctx, *entity.LocalID)
		if err != nil {
			return ret, err
		}
	default:
		return ret, ingest.ErrInvalid
	}
	var filePath string
	for _, found := range files {
		if int(found.Base().ID) == *file.LocalID && found.Base().ZipFileID == nil {
			filePath = found.Base().Path
		}
	}
	root, err := rs.repo.MediaRoot.Find(ctx, *collection.RootUUID)
	if err != nil {
		return ret, err
	}
	if root == nil || root.State != "active" || root.Binding == nil || filePath == "" {
		return ret, ingest.ErrInvalid
	}
	relative, err := filepath.Rel(root.Binding.Path, filePath)
	if err != nil || !archive.ValidRootRelativePath(filepath.ToSlash(relative), false) {
		return ret, ingest.ErrInvalid
	}
	policy, err := rs.repo.MetadataPolicy.Find(ctx, collection.UUID)
	if err != nil {
		return ret, err
	}
	ret = metadata.Input{CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, EntityUUID: entity.UUID, ExpectedEntityRevision: entity.Revision, RelativePath: filepath.ToSlash(relative), Source: request.Source}
	if policy != nil {
		ret.PolicyRevision = policy.Revision
	}
	return ret, nil
}

func (rs *nativeArchiveRoutes) preview(w http.ResponseWriter, r *http.Request) {
	rs.evaluate(w, r, false)
}
func (rs *nativeArchiveRoutes) apply(w http.ResponseWriter, r *http.Request) { rs.evaluate(w, r, true) }

func (rs *nativeArchiveRoutes) evaluate(w http.ResponseWriter, r *http.Request, apply bool) {
	var request metadataPreviewRequest
	if err := readIngestJSON(w, r, 16384, &request); err != nil {
		ingestError(w, err)
		return
	}
	if apply && !archive.ValidSHA256(request.Digest) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	withTxn := rs.repo.WithReadTxn
	if apply {
		withTxn = rs.repo.WithTxn
	}
	var result *metadata.Preview
	err := withTxn(r.Context(), func(ctx context.Context) error {
		input, err := rs.policyInput(ctx, request)
		if err != nil {
			return err
		}
		service := metadata.Service{Repo: rs.repo}
		if apply {
			result, err = service.Apply(ctx, input, request.Digest)
			if err == nil && rs.notifyMetadata != nil && len(result.AppliedFields()) > 0 {
				err = rs.notifyMetadata(ctx, input, result.AppliedFields())
			}
		} else {
			result, err = service.Preview(ctx, input)
		}
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
