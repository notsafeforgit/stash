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
	"github.com/stashapp/stash/pkg/translation"
)

type nativeArchiveRoutes struct {
	repo           models.Repository
	notifyMetadata func(context.Context, metadata.Input, []string) error
	albums         *gallery.AlbumBackfill
	translations   *translation.Service
}

// This router is mounted behind application authentication. Producer bearer
// tokens grant no access to collection configuration or library metadata edits.
func (rs *nativeArchiveRoutes) router() http.Handler {
	r := chi.NewRouter()
	r.Use(nativeAdminOrigin)
	r.Get("/source-accounts", rs.reviewAccounts)
	r.Get("/source-accounts/lookup", rs.lookupReviewAccounts)
	r.Get("/source-accounts/{account}", rs.reviewAccount)
	r.Get("/source-accounts/{account}/identifiers", rs.reviewAccountIdentifiers)
	r.Get("/source-accounts/{account}/ownership-history", rs.reviewAccountHistory)
	r.Get("/source-account-identifiers/{identifier}/evidence", rs.reviewAccountEvidence)
	r.Post("/account-ownership/preview", rs.previewAccountOwnership)
	r.Post("/account-ownership/apply", rs.applyAccountOwnership)
	r.Get("/account-ownership/requests/{request}", rs.accountOwnershipReview)
	r.Post("/account-consolidation/preview", rs.previewAccountConsolidation)
	r.Post("/account-consolidation/apply", rs.applyAccountConsolidation)
	r.Post("/account-consolidation/requests/{request}/check", rs.checkAccountConsolidation)
	r.Get("/source-accounts/{account}/consolidation-history", rs.accountConsolidationHistory)
	r.Get("/metadata-fields/{kind}", rs.fields)
	r.Get("/entity-identities/{kind}/{localID}", rs.metadataEntity)
	r.Get("/entities/{entity}/metadata-fields", rs.entityMetadataFields)
	r.Get("/entities/{entity}/metadata-fields/{field}/history", rs.entityMetadataHistory)
	r.Get("/entities/{entity}/file-edits", rs.entityFileEdits)
	r.Post("/metadata-file-edits/preview", rs.previewFileEdit)
	r.Post("/metadata-file-edits/apply", rs.applyFileEdit)
	r.Get("/metadata-file-edits/requests/{request}", rs.fileEditReview)
	r.Get("/collections/{collection}/metadata-policy", rs.policy)
	r.Put("/collections/{collection}/metadata-policy", rs.putPolicy)
	r.Get("/collections/{collection}/metadata-policy/history", rs.policyHistory)
	r.Get("/collections/{collection}/translation-policy", rs.translationPolicy)
	r.Put("/collections/{collection}/translation-policy", rs.putTranslationPolicy)
	r.Get("/collections/{collection}/translation-policy/history", rs.translationPolicyHistory)
	r.Get("/captures/{capture}/translation-decision", rs.captureTranslationDecision)
	r.Post("/metadata-policy/preview", rs.preview)
	r.Post("/metadata-policy/apply", rs.apply)
	r.Get("/media-roots", rs.roots)
	r.Post("/media-roots", rs.putRoot)
	r.Put("/media-roots/{root}", rs.putRoot)
	r.Get("/collections", rs.collections)
	r.Post("/collections", rs.putCollection)
	r.Get("/collections/{collection}", rs.collection)
	r.Put("/collections/{collection}", rs.putCollection)
	r.Get("/collections/{collection}/post-memberships", rs.collectionPostMemberships)
	r.Get("/posts/{post}/collection-memberships", rs.postCollectionMemberships)
	r.Get("/documents/{document}", rs.document)
	r.Get("/documents/{document}/content", rs.documentContent)
	r.Get("/document-sources/{source}", rs.documentSource)
	r.Get("/document-claims/{claim}", rs.documentClaim)
	r.Get("/posts/{post}/documents", rs.postDocuments)
	r.Get("/collections/{collection}/documents", rs.collectionDocuments)
	r.Get("/collections/{collection}/document-head", rs.documentHead)
	r.Put("/collections/{collection}/document-head", rs.putDocumentHead)
	r.Get("/collections/{collection}/document-head/claims", rs.documentClaims)
	r.Get("/collections/{collection}/document-head/history", rs.documentHeadHistory)
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
	r.Post("/automation-snapshots", rs.beginAutomationSnapshot)
	r.Get("/automation-snapshots/{snapshot}", rs.automationSnapshot)
	r.Put("/automation-snapshots/{snapshot}/chunks/{chunk}", rs.receiveAutomationSnapshotChunk)
	r.Get("/automation-snapshots/{snapshot}/enrichment-checkpoints", rs.automationCheckpointImport)
	r.Post("/automation-snapshots/{snapshot}/enrichment-checkpoints", rs.advanceAutomationCheckpointImport)
	r.Get("/automation-snapshots/{snapshot}/enrichment-checkpoints/records", rs.automationCheckpointRecords)
	r.Get("/automation-snapshots/{snapshot}/enrichment-checkpoints/records/{ordinal}", rs.automationCheckpointRecord)
	r.Get("/automation-snapshots/{snapshot}/enrichment-import/held-targets", rs.heldAutomationEnrichments)
	r.Get("/automation-snapshots/{snapshot}/discovery-import", rs.automationDiscoveryImport)
	r.Post("/automation-snapshots/{snapshot}/discovery-import", rs.advanceAutomationDiscoveryImport)
	r.Get("/automation-snapshots/{snapshot}/discovery-import/records", rs.automationDiscoveryRecords)
	r.Get("/automation-snapshots/{snapshot}/discovery-import/records/{ordinal}", rs.automationDiscoveryRecord)
	r.Get("/automation-snapshots/{snapshot}/enrichment-import", rs.automationEnrichmentImport)
	r.Post("/automation-snapshots/{snapshot}/enrichment-import", rs.advanceAutomationEnrichmentImport)
	r.Get("/automation-snapshots/{snapshot}/enrichment-import/records", rs.automationEnrichmentRecords)
	r.Get("/automation-snapshots/{snapshot}/enrichment-import/records/{ordinal}", rs.automationEnrichmentRecord)
	r.Get("/automation-snapshots/{snapshot}/translation-import", rs.automationTranslationImport)
	r.Post("/automation-snapshots/{snapshot}/translation-import", rs.advanceAutomationTranslationImport)
	r.Get("/automation-snapshots/{snapshot}/translation-import/records", rs.automationTranslationRecords)
	r.Get("/automation-snapshots/{snapshot}/translation-import/records/{ordinal}", rs.automationTranslationRecord)
	r.Get("/automation-snapshots/{snapshot}/translation-import/held-targets", rs.heldAutomationTranslations)
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
	r.Get("/translations/{translation}", rs.translation)
	r.Get("/translation-evidence/{evidence}", rs.translationEvidence)
	r.Get("/posts/{post}/translations", rs.postTranslations)
	r.Post("/posts/{post}/enrichment-targets", rs.createEnrichmentTarget)
	r.Get("/posts/{post}/enrichment-targets", rs.enrichmentTargets)
	r.Get("/collections/{collection}/enrichment-targets", rs.enrichmentTargets)
	r.Get("/enrichment-targets/{target}", rs.enrichmentTarget)
	r.Get("/enrichment-targets/{target}/history", rs.enrichmentTargetHistory)
	r.Put("/enrichment-targets/{target}/schedule", rs.scheduleEnrichmentTarget)
	r.Post("/enrichment-targets/{target}/retry", rs.retryEnrichmentTarget)
	r.Get("/enrichment-completions/{completion}", rs.enrichmentCompletion)
	r.Get("/enrichment-jobs/{job}/discovery-resolution", rs.enrichmentDiscoveryResolution)
	r.Get("/translation-requests/{request}", rs.translationRequest)
	r.Get("/translation-requests/{request}/cache", rs.translationRequestCache)
	r.Get("/translation-requests/{request}/targets", rs.translationTargets)
	r.Get("/posts/{post}/translation-targets", rs.translationTargets)
	r.Get("/translation-targets/{target}", rs.translationTarget)
	r.Get("/translation-targets/{target}/history", rs.translationTargetHistory)
	r.Post("/posts/{post}/translation-targets", rs.createTranslationTarget)
	r.Put("/translation-targets/{target}/schedule", rs.scheduleTranslationTarget)
	r.Post("/translation-targets/{target}/retry", rs.retryTranslationTarget)
	r.Get("/translation-targets/{target}/job", rs.translationTargetJob)
	r.Post("/enrichment-activations/preview", rs.previewEnrichmentActivation)
	r.Post("/enrichment-activations", rs.activateEnrichments)
	r.Get("/enrichment-activations/{activation}", rs.enrichmentActivation)
	r.Post("/discovery-activations/preview", rs.previewDiscoveryActivation)
	r.Post("/discovery-activations", rs.activateDiscovery)
	r.Get("/discovery-activations/{activation}", rs.discoveryActivation)
	r.Get("/discovery-match-targets/{target}", rs.discoveryMatchTarget)
	r.Get("/discovery-match-targets/{target}/review", rs.discoveryMatchReview)
	r.Post("/discovery-match-targets/{target}/publication", rs.publishDiscoveryMatch)
	r.Get("/discovery-match-targets/{target}/publication", rs.discoveryPublication)
	r.Get("/discovery-match-targets/{target}/publication/records", rs.discoveryPublishedRecords)
	r.Get("/discovery-match-targets/{target}/candidates", rs.discoveryMatchCandidates)
	r.Get("/discovery-match-candidates/{candidate}/evidence", rs.discoveryMatchEvidence)
	r.Post("/discovery-match-targets/{target}/detail-preview", rs.previewDiscoveryDetail)
	r.Get("/discovery-listings/{listing}", rs.discoveryListingReview)
	r.Get("/discovery-listings/{listing}/pages", rs.discoveryListingPages)
	r.Post("/checkpoint-evidence/preview", rs.previewCheckpointEvidence)
	r.Post("/checkpoint-evidence", rs.acceptCheckpointEvidence)
	r.Get("/checkpoint-evidence/{acceptance}", rs.checkpointEvidenceAcceptance)
	r.Post("/checkpoint-handoffs/preview", rs.previewCheckpointHandoff)
	r.Post("/checkpoint-handoffs", rs.acceptCheckpointHandoff)
	r.Get("/checkpoint-handoffs/{handoff}", rs.checkpointHandoff)
	r.Get("/checkpoint-handoffs/{handoff}/seed", rs.checkpointHandoffSeed)
	r.Post("/translation-activations/preview", rs.previewTranslationActivation)
	r.Post("/translation-activations", rs.activateTranslations)
	r.Get("/translation-activations/{activation}", rs.translationActivation)
	r.Post("/translation-jobs/admit", rs.admitTranslation)
	r.Get("/translation-jobs/{job}", rs.translationJob)
	r.Get("/translation-jobs/{job}/attempts", rs.translationJobAttempts)
	r.Post("/translation-jobs/{job}/cancel", rs.cancelTranslationJob)
	r.Get("/translation-requests/{request}/jobs", rs.translationJobHistory)
	r.Get("/catalog-snapshots/{snapshot}/enrichment-import", rs.catalogEnrichmentImport)
	r.Post("/catalog-snapshots/{snapshot}/enrichment-import", rs.advanceCatalogEnrichmentImport)
	r.Get("/catalog-snapshots/{snapshot}/enrichment-import/records", rs.catalogEnrichmentRecords)
	r.Get("/catalog-snapshots/{snapshot}/enrichment-import/records/{ordinal}", rs.catalogEnrichmentRecord)
	r.Get("/posts/{post}/enrichment-receipts", rs.postEnrichmentReceipts)
	r.Get("/enrichment-receipts/{receipt}", rs.enrichmentReceipt)
	r.Get("/catalog-snapshots/{snapshot}/translation-import", rs.catalogTranslationImport)
	r.Post("/catalog-snapshots/{snapshot}/translation-import", rs.advanceCatalogTranslationImport)
	r.Get("/catalog-snapshots/{snapshot}/translation-import/records", rs.catalogTranslationRecords)
	r.Get("/catalog-snapshots/{snapshot}/translation-import/records/{ordinal}", rs.catalogTranslationRecord)
	r.Get("/catalog-snapshots/{snapshot}/document-import", rs.catalogDocumentImport)
	r.Post("/catalog-snapshots/{snapshot}/document-import", rs.advanceCatalogDocumentImport)
	r.Get("/catalog-snapshots/{snapshot}/document-import/records", rs.catalogDocumentRecords)
	r.Get("/catalog-snapshots/{snapshot}/document-import/records/{ordinal}", rs.catalogDocumentRecord)
	r.Get("/catalog-snapshots/{snapshot}/media-import", rs.catalogMediaImport)
	r.Post("/catalog-snapshots/{snapshot}/media-import", rs.beginCatalogMediaImport)
	r.Post("/catalog-snapshots/{snapshot}/media-import/advance", rs.advanceCatalogMediaImport)
	r.Get("/catalog-snapshots/{snapshot}/media-import/records", rs.catalogMediaRecords)
	r.Get("/catalog-snapshots/{snapshot}/media-import/records/{ordinal}", rs.catalogMediaRecord)
	r.Get("/catalog-snapshots/{snapshot}/file-history-import", rs.catalogFileHistoryImport)
	r.Post("/catalog-snapshots/{snapshot}/file-history-import", rs.advanceCatalogFileHistoryImport)
	r.Get("/catalog-snapshots/{snapshot}/file-history-import/records", rs.catalogFileHistoryRecords)
	r.Get("/catalog-snapshots/{snapshot}/file-history-import/records/{ordinal}", rs.catalogFileHistoryRecord)
	r.Get("/file-history/{history}", rs.sourceFileHistory)
	r.Get("/file-observations/{observation}/history", rs.observationFileHistory)
	r.Get("/content-claims/{claim}/history", rs.claimFileHistory)
	return r
}

func nativeArchiveError(w http.ResponseWriter, err error) {
	if errors.Is(err, models.ErrMetadataFileReviewInvalid) || errors.Is(err, models.ErrAccountReviewInvalid) || errors.Is(err, models.ErrAccountConsolidationReviewInvalid) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	if errors.Is(err, models.ErrAccountReviewReplay) {
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "request_conflict", "message": "This request UUID already names a different account ownership review."})
		return
	}
	if errors.Is(err, models.ErrAccountConsolidationReplay) {
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "request_conflict", "message": "This request UUID already names a different account consolidation."})
		return
	}
	if errors.Is(err, models.ErrAccountOwnershipResolution) {
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "ownership_resolution_required", "message": "Choose the resulting ownership before consolidating these accounts."})
		return
	}
	if errors.Is(err, models.ErrAccountIdentifierResolution) {
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "identifier_resolution_required", "message": "Review and acknowledge the conflicting stable account identifiers."})
		return
	}
	if errors.Is(err, models.ErrSourceAccountConflict) {
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "preview_changed", "message": "The account or performer changed; load a fresh preview."})
		return
	}
	if errors.Is(err, models.ErrMetadataFileReviewReplay) {
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "request_conflict", "message": "This request UUID already names a different historical metadata review."})
		return
	}
	if errors.Is(err, models.ErrMetadataPolicyInvalid) || errors.Is(err, models.ErrTranslationPolicyInvalid) {
		ingestJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_policy", "message": err.Error()})
		return
	}
	if errors.Is(err, models.ErrMetadataPolicyConflict) || errors.Is(err, models.ErrMetadataFieldConflict) || errors.Is(err, models.ErrSourceDefinitionConflict) || errors.Is(err, models.ErrTranslationPolicyConflict) {
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
