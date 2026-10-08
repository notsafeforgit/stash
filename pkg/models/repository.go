package models

import (
	"context"

	"github.com/stashapp/stash/pkg/txn"
)

type TxnManager interface {
	txn.Manager
	txn.DatabaseProvider
}

type Repository struct {
	TxnManager TxnManager

	Blob                        BlobReader
	File                        FileReaderWriter
	FileContent                 FileContentReaderWriter
	FileDeduplication           FileDeduplicationReaderWriter
	FilePath                    FilePathReader
	Folder                      FolderReaderWriter
	Gallery                     GalleryReaderWriter
	GalleryChapter              GalleryChapterReaderWriter
	Image                       ImageReaderWriter
	Group                       GroupReaderWriter
	Performer                   PerformerReaderWriter
	Scene                       SceneReaderWriter
	SceneMarker                 SceneMarkerReaderWriter
	Studio                      StudioReaderWriter
	Tag                         TagReaderWriter
	SavedFilter                 SavedFilterReaderWriter
	DefaultFilter               DefaultFilterReaderWriter
	ConfigurationMigration      ConfigurationMigrationReaderWriter
	ArchiveEntity               ArchiveEntityReaderWriter
	SourceAccount               SourceAccountReaderWriter
	MediaRoot                   MediaRootReaderWriter
	SourceCollection            SourceCollectionReaderWriter
	CapturePublisher            CapturePublisherReaderWriter
	Ingest                      IngestReaderWriter
	ArchiveJob                  ArchiveJobReaderWriter
	ArchiveActivity             ArchiveActivityReader
	ArchiveReview               ArchiveReviewReader
	ArchiveImport               ArchiveImportReader
	SourceRun                   SourceRunReaderWriter
	SourceBackfill              SourceBackfillReaderWriter
	ScanJournal                 ScanJournalReaderWriter
	CatalogIdentityImport       CatalogIdentityImportReaderWriter
	CatalogRegistryImport       CatalogRegistryImportReaderWriter
	CatalogSnapshot             CatalogSnapshotReaderWriter
	AutomationSnapshot          AutomationSnapshotReaderWriter
	AutomationEnrichmentImport  AutomationEnrichmentImportReaderWriter
	AutomationDiscoveryImport   AutomationDiscoveryImportReaderWriter
	AutomationCheckpointImport  AutomationCheckpointImportReaderWriter
	AutomationTranslationImport AutomationTranslationImportReaderWriter
	CatalogEvidenceImport       CatalogEvidenceImportReaderWriter
	CatalogRelationsImport      CatalogRelationsImportReaderWriter
	CatalogPublisherImport      CatalogPublisherImportReaderWriter
	CatalogAttachmentImport     CatalogAttachmentImportReaderWriter
	SourceEvidence              SourceEvidenceReaderWriter
	SourceDocument              SourceDocumentReaderWriter
	SourceTranslation           SourceTranslationReaderWriter
	TranslationWork             TranslationWorkReaderWriter
	EnrichmentWork              EnrichmentWorkReaderWriter
	EnrichmentJob               EnrichmentJobReaderWriter
	DiscoveryJob                DiscoveryJobReaderWriter
	DiscoveryMatch              DiscoveryMatchReaderWriter
	DiscoveryDetail             DiscoveryDetailReaderWriter
	TranslationPolicy           TranslationPolicyReaderWriter
	CatalogCleanupImport        CatalogCleanupImportReaderWriter
	SourceCleanupIntent         SourceCleanupIntentReader
	CatalogEnrichmentImport     CatalogEnrichmentImportReaderWriter
	SourceEnrichmentReceipt     SourceEnrichmentReceiptReader
	CatalogTranslationImport    CatalogTranslationImportReaderWriter
	CatalogDocumentImport       CatalogDocumentImportReaderWriter
	SourcePostLinks             SourcePostLinksReaderWriter
	SourceFile                  SourceFileReaderWriter
	SourceFileHistory           SourceFileHistoryReaderWriter
	CatalogFileHistoryImport    CatalogFileHistoryImportReaderWriter
	CatalogMediaImport          CatalogMediaImportReaderWriter
	CatalogMembershipImport     CatalogMembershipImportReaderWriter
	SourceAttachment            SourceAttachmentReaderWriter
	SourcePostMedia             SourcePostMediaReaderWriter
	SourceGallery               SourceGalleryReaderWriter
	MetadataField               MetadataFieldReaderWriter
	ProviderMetadata            ProviderMetadataReaderWriter
	MetadataPolicy              MetadataPolicyReaderWriter
	MetadataPolicyImport        MetadataPolicyImportReaderWriter
	Share                       ShareReaderWriter
}

func (r *Repository) WithTxn(ctx context.Context, fn txn.TxnFunc) error {
	return txn.WithTxn(ctx, r.TxnManager, fn)
}

func (r *Repository) WithReadTxn(ctx context.Context, fn txn.TxnFunc) error {
	return txn.WithReadTxn(ctx, r.TxnManager, fn)
}

func (r *Repository) WithDB(ctx context.Context, fn txn.TxnFunc) error {
	return txn.WithDatabase(ctx, r.TxnManager, fn)
}
