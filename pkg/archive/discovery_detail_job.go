package archive

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
)

const MaxDiscoveryDetailJobs = 32

func PrepareDiscoveryDetailJob(input models.DiscoveryDetailJobArguments) (models.ArchiveJobSubmission, error) {
	ret := models.ArchiveJobSubmission{}
	if input.Version != 1 || input.Generation < 1 || !translationUUID(input.TargetUUID) || input.TargetRevision < 1 ||
		!translationUUID(input.PostUUID) || input.PostRevision < 1 || input.CandidateSequence < 1 ||
		!translationUUID(input.ListingUUID) || input.PageOrdinal < 1 || input.PageOrdinal > MaxDiscoveryPages ||
		!translationUUID(input.CollectionUUID) || input.CollectionRevision < 1 ||
		(input.RootUUID != nil && !translationUUID(*input.RootUUID)) || !ValidSHA256(input.SourceSHA256) ||
		!ValidSHA256(input.DefinitionSHA256) || !ValidSHA256(input.PageSHA256) || !ValidSHA256(input.PolicySHA256) ||
		input.CapturePolicy != CaptureContextPolicy || input.ExtractorVersion == "" || len(input.ExtractorVersion) > 128 ||
		!utf8.ValidString(input.ExtractorVersion) || strings.ContainsAny(input.ExtractorVersion, "\r\n\x00") || !validDiscoveryDetailURL(input.PostIdentifier(), input.URL) {
		return ret, models.ErrDiscoveryInvalid
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
	return models.ArchiveJobSubmission{RequestUUID: uuid.NewSHA1(uuid.NameSpaceURL, []byte("urn:stash:discovery-detail-job:v1:"+key)).String(),
		Kind: models.ArchiveJobVerifyCandidate, WorkKey: key, ResourceKey: EnrichmentResource(input.CollectionUUID), Arguments: body, MaxAttempts: 8}, nil
}

func DecodeDiscoveryDetailJob(job *models.ArchiveJob) (*models.DiscoveryDetailJobArguments, error) {
	if job == nil || job.Kind != models.ArchiveJobVerifyCandidate {
		return nil, models.ErrDiscoveryInvalid
	}
	tree, err := DecodeJSONObject(job.Arguments, 16384)
	if err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	canonical, err := EncodeSourceJSON(tree)
	if err != nil {
		return nil, err
	}
	var input models.DiscoveryDetailJobArguments
	decoder := json.NewDecoder(bytes.NewReader(job.Arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	expected, err := PrepareDiscoveryDetailJob(input)
	if err != nil || expected.WorkKey != job.WorkKey || expected.ResourceKey != job.ResourceKey ||
		!bytes.Equal(expected.Arguments, canonical) || job.MaxAttempts != expected.MaxAttempts {
		return nil, models.ErrDiscoveryInvalid
	}
	return &input, nil
}
