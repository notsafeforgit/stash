package archive

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
)

const (
	MaxDiscoveryJobs        = 16
	MaxDiscoveryPages       = 10000
	MaxDiscoveryStoredBytes = 2 << 30
)

func PrepareDiscoveryListing(input models.DiscoveryListingInput) (json.RawMessage, string, error) {
	platform, err := DiscoveryProfilePlatform(input.ProfileURL)
	if err != nil || !translationUUID(input.UUID) || !translationUUID(input.AccountUUID) ||
		!translationUUID(input.CollectionUUID) || input.CollectionRevision < 1 ||
		(input.RootUUID != nil && !translationUUID(*input.RootUUID)) || !ValidSHA256(input.PolicySHA256) ||
		input.ExtractorVersion == "" || len(input.ExtractorVersion) > 128 || !utf8.ValidString(input.ExtractorVersion) || strings.ContainsAny(input.ExtractorVersion, "\r\n\x00") ||
		input.HistoricalPages < 0 || input.HistoricalPages > 10000000 || input.NotBefore.UnixMilli() <= 0 || input.NotBefore.UTC().Year() > 9999 ||
		!input.NotBefore.Equal(input.NotBefore.Truncate(time.Millisecond)) {
		return nil, "", models.ErrDiscoveryInvalid
	}
	if input.Legacy == nil {
		if input.InitialCursor != nil || input.HistoricalPages != 0 {
			return nil, "", models.ErrDiscoveryInvalid
		}
	} else if !translationUUID(input.Legacy.SnapshotUUID) || input.Legacy.AccountOrdinal < 1 {
		return nil, "", models.ErrDiscoveryInvalid
	}
	if recovery := input.RecoveryOf; recovery != nil {
		if input.Legacy == nil || !translationUUID(recovery.ListingUUID) || recovery.ListingUUID == input.UUID ||
			!ValidSHA256(recovery.SHA256) || input.InitialCursor != nil || input.HistoricalPages != 0 {
			return nil, "", models.ErrDiscoveryInvalid
		}
	}
	if input.InitialCursor != nil {
		cursor := sourceObject{}
		for key, value := range input.InitialCursor {
			cursor[key] = value
		}
		if _, err := discoveryCursor(platform, cursor); err != nil {
			return nil, "", err
		}
	}
	input.NotBefore = input.NotBefore.UTC()
	body, err := json.Marshal(input)
	if err != nil {
		return nil, "", err
	}
	tree, err := DecodeJSONObject(body, 32768)
	if err != nil {
		return nil, "", models.ErrDiscoveryInvalid
	}
	body, err = EncodeSourceJSON(tree)
	if err != nil {
		return nil, "", err
	}
	return body, translationDigest(body), nil
}

func PrepareDiscoveryJob(input models.DiscoveryJobArguments) (models.ArchiveJobSubmission, error) {
	ret := models.ArchiveJobSubmission{}
	if input.Version != 1 || !translationUUID(input.ListingUUID) || input.Generation < 1 || input.PageOrdinal < 1 || input.PageOrdinal > MaxDiscoveryPages || !ValidSHA256(input.DefinitionSHA256) || !translationUUID(input.CollectionUUID) {
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
	return models.ArchiveJobSubmission{RequestUUID: uuid.NewSHA1(uuid.NameSpaceURL, []byte("urn:stash:discovery-listing-job:v1:"+key)).String(),
		Kind: models.ArchiveJobListAccount, WorkKey: key, ResourceKey: EnrichmentResource(input.CollectionUUID), Arguments: body, MaxAttempts: 8}, nil
}

func DecodeDiscoveryJob(job *models.ArchiveJob) (*models.DiscoveryJobArguments, error) {
	if job == nil || job.Kind != models.ArchiveJobListAccount {
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
	var input models.DiscoveryJobArguments
	decoder := json.NewDecoder(bytes.NewReader(job.Arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	expected, err := PrepareDiscoveryJob(input)
	if err != nil || expected.WorkKey != job.WorkKey || expected.ResourceKey != job.ResourceKey ||
		!bytes.Equal(expected.Arguments, canonical) || job.MaxAttempts != expected.MaxAttempts {
		return nil, models.ErrDiscoveryInvalid
	}
	return &input, nil
}
