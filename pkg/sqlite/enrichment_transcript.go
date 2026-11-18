package sqlite

import (
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// Checkpoint handoff seeds were retired with the import history. Every
// remaining enrichment job starts from an empty transcript.
func verifyEnrichmentTranscript(job *models.ArchiveJob, work *models.EnrichmentJobArguments, parsed *archive.EnrichmentTranscript) error {
	if parsed == nil || parsed.ExtractorVersion != work.ExtractorVersion {
		return models.ErrEnrichmentConflict
	}
	if work.Handoff != nil || parsed.Schema != archive.EnrichmentTranscriptSchema {
		return models.ErrEnrichmentInvalid
	}
	return nil
}

func enrichmentRecordCapture(job string, work *models.EnrichmentJobArguments, transcript *archive.EnrichmentTranscript,
	record models.EnrichmentCheckpointRecord, preceding []string) (*models.SourceCaptureInput, *models.SourcePostIdentifier, error) {
	if record.Ordinal < 0 || record.Ordinal >= len(transcript.Records) || work.Handoff != nil {
		return nil, nil, models.ErrSourcePayloadCorrupt
	}
	var parent string
	if ordinal := transcript.Records[record.Ordinal].Parent; ordinal != nil {
		if *ordinal < 0 || *ordinal >= record.Ordinal || *ordinal >= len(preceding) || preceding[*ordinal] == "" {
			return nil, nil, models.ErrSourcePayloadCorrupt
		}
		parent = preceding[*ordinal]
	}
	return archive.PrepareEnrichmentRecord(job, work, transcript, record, parent, nil)
}
