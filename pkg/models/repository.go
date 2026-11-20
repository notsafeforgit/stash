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

	Blob                   BlobReader
	File                   FileReaderWriter
	FileContent            FileContentReaderWriter
	FileDeduplication      FileDeduplicationReaderWriter
	FilePath               FilePathReader
	Folder                 FolderReaderWriter
	Gallery                GalleryReaderWriter
	GalleryChapter         GalleryChapterReaderWriter
	Image                  ImageReaderWriter
	Group                  GroupReaderWriter
	Performer              PerformerReaderWriter
	Scene                  SceneReaderWriter
	SceneCoverUpgrade      SceneCoverUpgradeReaderWriter
	LibraryBulkJob         LibraryBulkJobReaderWriter
	SceneMarker            SceneMarkerReaderWriter
	Studio                 StudioReaderWriter
	Tag                    TagReaderWriter
	SavedFilter            SavedFilterReaderWriter
	DefaultFilter          DefaultFilterReaderWriter
	ConfigurationMigration ConfigurationMigrationReaderWriter
	ArchiveEntity          ArchiveEntityReaderWriter
	MediaConversion        MediaConversionReaderWriter
	SourceAccount          SourceAccountReaderWriter
	MediaRoot              MediaRootReaderWriter
	SourceCollection       SourceCollectionReaderWriter
	SourceStudio           SourceStudioReaderWriter
	SourceSubscription     SourceSubscriptionReaderWriter
	CapturePublisher       CapturePublisherReaderWriter
	Ingest                 IngestReaderWriter
	ArchiveJob             ArchiveJobReaderWriter
	ArchiveActivity        ArchiveActivityReader
	ArchiveReview          ArchiveReviewReader
	ArchivePagination      ArchivePaginationReader
	SourceRun              SourceRunReaderWriter
	WorkerPolicy           WorkerPolicyReaderWriter
	SourceBackfill         SourceBackfillReaderWriter
	ScanJournal            ScanJournalReaderWriter
	SourceEvidence         SourceEvidenceReaderWriter
	SourceDocument         SourceDocumentReaderWriter
	SourceTranslation      SourceTranslationReaderWriter
	TranslationWork        TranslationWorkReaderWriter
	EnrichmentWork         EnrichmentWorkReaderWriter
	EnrichmentJob          EnrichmentJobReaderWriter
	TranslationPolicy      TranslationPolicyReaderWriter
	SourcePostLinks        SourcePostLinksReaderWriter
	SourceThread           SourceThreadReaderWriter
	SourceFile             SourceFileReaderWriter
	SourceAttachment       SourceAttachmentReaderWriter
	SourcePostMedia        SourcePostMediaReaderWriter
	SourceGallery          SourceGalleryReaderWriter
	MetadataField          MetadataFieldReaderWriter
	ProviderMetadata       ProviderMetadataReaderWriter
	MetadataPolicy         MetadataPolicyReaderWriter
	MetadataPolicyImport   MetadataPolicyImportReaderWriter
	Share                  ShareReaderWriter
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
