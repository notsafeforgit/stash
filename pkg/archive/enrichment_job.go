package archive

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
)

const (
	MaxEnrichmentJobs                = 64
	MaxEnrichmentCheckpointRevisions = 128
	MaxEnrichmentStoredBytes         = 2 << 30
)

func EnrichmentResource(collection string) string {
	return translationDigest([]byte("enrichment-collection\x00" + collection))
}

func PrepareEnrichmentJob(input models.EnrichmentJobArguments) (models.ArchiveJobSubmission, error) {
	ret := models.ArchiveJobSubmission{}
	if input.Version != 1 || !translationUUID(input.TargetUUID) || input.TargetRevision < 1 ||
		!translationUUID(input.PostUUID) || !translationUUID(input.CollectionUUID) || input.CollectionRevision < 1 ||
		(input.RootUUID != nil && !translationUUID(*input.RootUUID)) || !ValidSHA256(input.PolicySHA256) ||
		input.ExtractorVersion == "" || len(input.ExtractorVersion) > 128 || strings.ContainsAny(input.ExtractorVersion, "\r\n\x00") {
		return ret, models.ErrEnrichmentInvalid
	}
	body, err := json.Marshal(input)
	if err != nil {
		return ret, err
	}
	tree, err := DecodeJSONObject(body, 16384)
	if err != nil {
		return ret, err
	}
	body, err = EncodeSourceJSON(tree)
	if err != nil {
		return ret, err
	}
	key := translationDigest(body)
	return models.ArchiveJobSubmission{RequestUUID: uuid.NewSHA1(uuid.NameSpaceURL, []byte("urn:stash:enrichment-job:v1:"+key)).String(),
		Kind: models.ArchiveJobEnrichPost, WorkKey: key, ResourceKey: EnrichmentResource(input.CollectionUUID), Arguments: body, MaxAttempts: 8}, nil
}

func DecodeEnrichmentJob(current *models.ArchiveJob) (*models.EnrichmentJobArguments, error) {
	if current == nil || current.Kind != models.ArchiveJobEnrichPost {
		return nil, models.ErrEnrichmentInvalid
	}
	tree, err := DecodeJSONObject(current.Arguments, 16384)
	if err != nil {
		return nil, models.ErrEnrichmentInvalid
	}
	canonical, err := EncodeSourceJSON(tree)
	if err != nil {
		return nil, err
	}
	var input models.EnrichmentJobArguments
	d := json.NewDecoder(bytes.NewReader(current.Arguments))
	d.DisallowUnknownFields()
	if err := d.Decode(&input); err != nil {
		return nil, models.ErrEnrichmentInvalid
	}
	expected, err := PrepareEnrichmentJob(input)
	if err != nil || expected.WorkKey != current.WorkKey || expected.ResourceKey != current.ResourceKey ||
		!bytes.Equal(expected.Arguments, canonical) || current.MaxAttempts != expected.MaxAttempts {
		return nil, models.ErrEnrichmentInvalid
	}
	return &input, nil
}

// RecordDigest includes the original compact representation and observation
// time. It lets the native store retain per-record attempt provenance without
// saving a full copy of every historical checkpoint.
func (t *EnrichmentTranscript) RecordDigest(index int) (string, error) {
	if index < 0 || index >= len(t.Records) {
		return "", models.ErrEnrichmentInvalid
	}
	r := t.Records[index]
	body, err := EncodeSourceJSON(sourceObject{"kind": r.Kind, "base": r.Base, "parent": r.Parent,
		"patch": r.Patch, "removed": r.Removed, "observed_at": r.ObservedAt})
	if err != nil {
		return "", err
	}
	return translationDigest(body), nil
}
