package archive

import (
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
)

const DiscoveryPublicationPolicy = "retained-discovery-publication-v1"

type DiscoveryCaptureRecord struct {
	Ordinal int
	Input   models.SourceCaptureInput
}

// PrepareDiscoveryCaptures preserves the original observing producer, times and
// parent contexts. Only the selected post's records are promoted; unrelated
// posts on the shared page remain staged. Equal observations share a capture.
func PrepareDiscoveryCaptures(saved models.DiscoveryPage, postUUID string, reference models.SourcePostIdentifier) ([]DiscoveryCaptureRecord, error) {
	if !translationUUID(saved.JobUUID) || !translationUUID(saved.ProducerUUID) || !translationUUID(postUUID) ||
		!translationUUID(saved.ListingUUID) || saved.Ordinal < 1 || saved.Fence < 1 || translationDigest(saved.Body) != saved.Digest ||
		(reference.Namespace != "native:reddit" && reference.Namespace != "native:twitter") || reference.Value == "" {
		return nil, models.ErrDiscoveryInvalid
	}
	page, err := ParseDiscoveryPage(saved.Body)
	if err != nil {
		return nil, err
	}
	if len(page.Records) != saved.RecordCount || page.Complete != saved.Complete {
		return nil, models.ErrSourcePayloadCorrupt
	}
	ret := []DiscoveryCaptureRecord{}
	ids := make(map[int]string)
	for ordinal, record := range page.Records {
		raw, err := page.Metadata(ordinal)
		if err != nil {
			return nil, err
		}
		post, err := ExtractCapturedPost(raw)
		if err != nil {
			return nil, err
		}
		if post == nil || *post != reference {
			continue
		}
		parent := ""
		if record.Parent != nil {
			parent = ids[*record.Parent]
			if parent == "" {
				return nil, models.ErrDiscoveryConflict
			}
		}
		observed, err := time.Parse(time.RFC3339Nano, record.ObservedAt)
		if err != nil || observed.IsZero() {
			return nil, models.ErrDiscoveryInvalid
		}
		key := "discovery-capture-v1\x00" + saved.ProducerUUID + "\x00" + observed.UTC().Format(time.RFC3339Nano) + "\x00" + translationDigest(raw) + "\x00" + parent
		id := uuid.NewSHA1(uuid.MustParse(saved.JobUUID), []byte(key)).String()
		metadata, err := CapturedMetadata(raw)
		if err != nil {
			return nil, err
		}
		payload, err := PrepareRetainedCapture("gallery-dl", CapturedPostPlatform(reference), raw)
		if err != nil {
			return nil, err
		}
		input := models.SourceCaptureInput{UUID: id, PostUUID: postUUID, Origin: "gallery-dl", Platform: CapturedPostPlatform(reference),
			CapturedAt: observed, ExtractorVersion: &page.ExtractorVersion, RetentionPolicy: page.RetentionPolicy, Metadata: metadata, Payload: *payload}
		link, err := page.transcript.CaptureContext(ordinal, id, parent)
		if err != nil {
			return nil, err
		}
		if link != nil {
			input.RetentionPolicy, input.Contexts = CaptureContextPolicy, []models.SourceCaptureContext{*link}
			if err := CaptureContextRetention(raw, input.Contexts); err != nil {
				return nil, err
			}
		}
		ids[ordinal] = id
		ret = append(ret, DiscoveryCaptureRecord{Ordinal: ordinal, Input: input})
	}
	if len(ret) == 0 {
		return nil, models.ErrDiscoveryConflict
	}
	return ret, nil
}
