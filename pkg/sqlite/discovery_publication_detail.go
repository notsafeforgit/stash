package sqlite

import (
	"database/sql"
	"errors"
	"reflect"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// A later comparison only appends evidence. Completed details remain useful
// when that comparison advances, provided the same original target/candidate
// survives. Prefer the latest completed result, including a negative result;
// never search backwards specifically for a successful corroboration.
func latestDiscoveryPublicationDetail(get enrichmentGet, target models.DiscoveryMatchTarget, candidate models.DiscoveryMatchCandidate) (*models.DiscoveryDetailResult, error) {
	var row discoveryDetailResultRow
	err := get(&row, `SELECT r.* FROM discovery_detail_jobs d
 JOIN discovery_detail_results r ON r.job_uuid=d.job_uuid
 WHERE d.target_uuid=? AND d.candidate_sequence=? AND d.target_revision<=?
 ORDER BY d.target_revision DESC,d.generation DESC LIMIT 1`, target.UUID, candidate.Sequence, target.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row.resolve()
}

type preparedDiscoveryPublicationDetail struct {
	work    *models.DiscoveryDetailJobArguments
	result  *models.DiscoveryDetailResult
	records []archive.DiscoveryCaptureRecord
	witness time.Time
}

// Only server-retained, authenticated results can authorize this path. Rebuild
// their comparison from the frozen source and exact saved bytes, independently
// of the read-only application preview and the result's status string.
func prepareDiscoveryPublicationDetail(get enrichmentGet, selectRows enrichmentSelect, id string, target models.DiscoveryMatchTarget,
	listing *models.DiscoveryListing, candidate models.DiscoveryMatchCandidate) (*preparedDiscoveryPublicationDetail, error) {
	var jobRow archiveJobRow
	if err := get(&jobRow, "SELECT * FROM archive_jobs WHERE uuid=?", id); err != nil {
		return nil, err
	}
	job := jobRow.resolve()
	work, err := discoveryDetailJobProof(get, job)
	if err != nil {
		return nil, err
	}
	if job.State != "succeeded" || work.TargetUUID != target.UUID || work.TargetRevision > target.Revision || work.CandidateSequence != candidate.Sequence ||
		work.SourceSHA256 != target.SourceSHA256 || work.PostUUID != target.PostUUID || work.PostRevision != target.PostRevision ||
		work.ListingUUID != listing.UUID || work.DefinitionSHA256 != listing.Digest || work.CollectionUUID != listing.CollectionUUID ||
		work.CollectionRevision != listing.CollectionRevision || !reflect.DeepEqual(work.RootUUID, listing.RootUUID) ||
		work.Namespace != candidate.Namespace || work.Value != candidate.Value || work.URL != candidate.URL {
		return nil, models.ErrDiscoveryConflict
	}
	var resultRow discoveryDetailResultRow
	if err := get(&resultRow, "SELECT * FROM discovery_detail_results WHERE job_uuid=?", id); err != nil {
		return nil, err
	}
	result, err := resultRow.resolve()
	if err != nil {
		return nil, err
	}
	var head struct {
		models.EnrichmentCheckpointReceipt
		Body string `db:"body"`
	}
	if err := get(&head, `SELECT r.*,h.body FROM discovery_detail_checkpoints h
 JOIN discovery_detail_checkpoint_receipts r ON r.job_uuid=h.job_uuid AND r.revision=h.revision WHERE h.job_uuid=?`, id); err != nil {
		return nil, err
	}
	if head.Revision != result.CheckpointRevision || head.Digest != result.Evidence.TranscriptSHA256 || enrichmentDigest([]byte(head.Body)) != head.Digest ||
		head.PendingCount != 0 || result.Fence != job.Fence || result.CreatedAt.Before(head.CreatedAt) || result.CreatedAt.UnixMilli() != job.UpdatedAt.UnixMilli() {
		return nil, models.ErrSourcePayloadCorrupt
	}
	evidence, err := compareDiscoveryDetail(get, work, []byte(head.Body))
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(*evidence, result.Evidence) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	if evidence.Status != "corroborated" || evidence.WitnessOrdinal == nil {
		return nil, models.ErrDiscoveryConflict
	}
	var originals []models.EnrichmentCheckpointRecord
	if err := selectRows(&originals, `SELECT r.*,c.fence,a.producer_uuid FROM discovery_detail_checkpoint_records r
 JOIN discovery_detail_checkpoint_receipts c ON c.job_uuid=r.job_uuid AND c.revision=r.checkpoint_revision
 JOIN discovery_detail_attempts a ON a.job_uuid=c.job_uuid AND a.fence=c.fence
 WHERE r.job_uuid=? ORDER BY r.ordinal LIMIT 1025`, id); err != nil {
		return nil, err
	}
	if len(originals) != head.RecordCount {
		return nil, models.ErrSourcePayloadCorrupt
	}
	records, err := archive.PrepareDiscoveryDetailCaptures(id, target.PostUUID, work.PostIdentifier(), work.URL, work.ExtractorVersion, []byte(head.Body), originals)
	if err != nil {
		return nil, err
	}
	witness := *evidence.WitnessOrdinal
	if witness < 0 || witness >= len(records) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return &preparedDiscoveryPublicationDetail{work: work, result: result, records: records, witness: records[witness].Input.CapturedAt}, nil
}

func discoveryPublicationDetailMatches(review *models.DiscoveryMatchReview, id string) bool {
	if review.Candidate == nil {
		return false
	}
	if !review.Candidate.NeedsDetail {
		return id == ""
	}
	return review.Detail != nil && review.Detail.JobUUID == id && review.Detail.Evidence.Status == "corroborated"
}

func publicationDetailUUID(row *models.DiscoveryMatchPublication) string {
	if row.DetailJobUUID == nil {
		return ""
	}
	return *row.DetailJobUUID
}
