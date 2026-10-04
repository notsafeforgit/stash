package archive

import (
	"encoding/json"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

// ParseDiscoveryDetail reuses the compact metadata-fetch format. A detail is
// about exactly the selected post; unrelated posts or retained unobserved
// contexts cannot supply its proof. Parsing never authenticates a producer.
func validDiscoveryDetailURL(selected models.SourcePostIdentifier, requestedURL string) bool {
	if (selected.Namespace != "native:reddit" && selected.Namespace != "native:twitter") || selected.Value == "" {
		return false
	}
	canonical, alphabet := "https://www.reddit.com/comments/"+selected.Value, "abcdefghijklmnopqrstuvwxyz0123456789"
	if selected.Namespace == "native:twitter" {
		canonical, alphabet = "https://x.com/i/web/status/"+selected.Value, "0123456789"
	}
	return len(selected.Value) <= 256 && strings.Trim(selected.Value, alphabet) == "" && requestedURL == canonical
}

func ParseDiscoveryDetail(raw json.RawMessage, selected models.SourcePostIdentifier, requestedURL, extractor string) (*EnrichmentTranscript, error) {
	if !validDiscoveryDetailURL(selected, requestedURL) || extractor == "" {
		return nil, models.ErrDiscoveryInvalid
	}
	transcript, err := ParseEnrichmentTranscript(raw)
	if err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	if transcript.Schema != EnrichmentTranscriptSchema || transcript.URL != requestedURL || transcript.ExtractorVersion != extractor {
		return nil, models.ErrDiscoveryConflict
	}
	for ordinal := range transcript.Records {
		metadata, err := transcript.Metadata(ordinal)
		if err != nil {
			return nil, err
		}
		post, err := ExtractCapturedPost(metadata)
		if err != nil {
			return nil, models.ErrDiscoveryInvalid
		}
		if post == nil || *post != selected || transcript.Records[ordinal].RetainedCapture != nil {
			return nil, models.ErrDiscoveryConflict
		}
	}
	return transcript, nil
}

// PrepareDiscoveryDetailCaptures preserves per-record observing producers and
// original times across checkpoint failover. It prepares values only: callers
// must corroborate the detail against the original target and publish inside
// the owned transaction. A completed fetch alone cannot authorize publication.
func PrepareDiscoveryDetailCaptures(job, postUUID string, selected models.SourcePostIdentifier, requestedURL, extractor string,
	body json.RawMessage, records []models.EnrichmentCheckpointRecord) ([]DiscoveryCaptureRecord, error) {
	if !translationUUID(job) || !translationUUID(postUUID) {
		return nil, models.ErrDiscoveryInvalid
	}
	transcript, err := ParseDiscoveryDetail(body, selected, requestedURL, extractor)
	if err != nil {
		return nil, err
	}
	if len(transcript.Pending) != 0 || len(transcript.Records) == 0 || len(transcript.Records) != len(records) {
		return nil, models.ErrDiscoveryConflict
	}
	work := &models.EnrichmentJobArguments{Version: 2, PostUUID: postUUID, CapturePolicy: CaptureContextPolicy, ExtractorVersion: extractor}
	ret := make([]DiscoveryCaptureRecord, 0, len(records))
	for ordinal, record := range records {
		if record.Ordinal != ordinal || record.JobUUID != job || record.CheckpointRevision < 1 || record.Fence < 1 || record.RetainedCapture != nil {
			return nil, models.ErrDiscoveryInvalid
		}
		parent := ""
		if previous := transcript.Records[ordinal].Parent; previous != nil {
			parent = ret[*previous].Input.UUID
		}
		input, reference, err := PrepareContextEnrichmentCapture(job, work, transcript, record, parent)
		if err != nil {
			return nil, err
		}
		if reference == nil || *reference != selected {
			return nil, models.ErrDiscoveryConflict
		}
		ret = append(ret, DiscoveryCaptureRecord{Ordinal: ordinal, Input: *input})
	}
	return ret, nil
}
