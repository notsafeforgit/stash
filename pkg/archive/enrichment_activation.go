package archive

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

const MaxEnrichmentActivationTargets = 100
const MaxEnrichmentActivationBytes = 8 << 20

func PrepareEnrichmentActivation(input models.EnrichmentActivationInput) (models.EnrichmentActivationInput, string, error) {
	if !translationUUID(input.UUID) || len(input.Targets) == 0 || len(input.Targets) > MaxEnrichmentActivationTargets ||
		(input.SnapshotUUID == "") != (input.ManifestSHA256 == "") ||
		(input.SnapshotUUID != "" && (!translationUUID(input.SnapshotUUID) || !ValidSHA256(input.ManifestSHA256))) {
		return input, "", models.ErrEnrichmentInvalid
	}
	input.Targets = slices.Clone(input.Targets)
	slices.SortFunc(input.Targets, func(a, b models.EnrichmentActivationSelection) int {
		return strings.Compare(a.TargetUUID, b.TargetUUID)
	})
	previous := ""
	for _, ref := range input.Targets {
		if !translationUUID(ref.TargetUUID) || ref.TargetUUID == previous || ref.Revision < 1 || ref.CollectionRevision < 1 || ref.Revision == int(^uint(0)>>1) {
			return input, "", models.ErrEnrichmentInvalid
		}
		previous = ref.TargetUUID
	}
	body, err := json.Marshal(input)
	return input, translationDigest(body), err
}

func EnrichmentActivationDigest(plan models.EnrichmentActivationPlan) (string, error) {
	plan.PlanSHA256 = ""
	body, err := json.Marshal(plan)
	if err != nil || len(body) > MaxEnrichmentActivationBytes {
		return "", models.ErrEnrichmentInvalid
	}
	return translationDigest(body), nil
}

func DecodeEnrichmentActivationPlan(body []byte) (*models.EnrichmentActivationPlan, error) {
	if _, err := DecodeJSONObject(body, MaxEnrichmentActivationBytes); err != nil {
		return nil, models.ErrEnrichmentInvalid
	}
	var plan models.EnrichmentActivationPlan
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(&plan); err != nil {
		return nil, models.ErrEnrichmentInvalid
	}
	input, _, err := PrepareEnrichmentActivation(plan.Input)
	if err != nil || plan.Version != 1 || !slices.Equal(input.Targets, plan.Input.Targets) || len(plan.Entries) != len(input.Targets) {
		return nil, models.ErrEnrichmentInvalid
	}
	releases := map[string]bool{}
	for i, entry := range plan.Entries {
		if entry.EnrichmentTargetRef != input.Targets[i].EnrichmentTargetRef || entry.ActivationCollectionRevision != input.Targets[i].CollectionRevision || entry.ActivationCollectionRevision < entry.CollectionRevision || !translationUUID(entry.PostUUID) || entry.PostRevision < 1 ||
			!translationUUID(entry.URLUUID) || !EnrichmentPostURL(entry.URL) || !translationUUID(entry.CollectionUUID) || entry.CollectionRevision < 1 ||
			entry.Policy != models.EnrichmentGalleryMetadataV1 || entry.Priority < 0 || entry.Priority > 100 ||
			entry.NotBefore.UnixMilli() <= 0 || entry.NotBefore.UTC().Year() > 9999 {
			return nil, models.ErrEnrichmentInvalid
		}
		original := models.EnrichmentTargetInput{PostUUID: entry.PostUUID, URLUUID: entry.URLUUID, CollectionUUID: entry.CollectionUUID,
			CollectionRevision: entry.CollectionRevision, Policy: entry.Policy, Origin: "review"}
		id, err := EnrichmentTargetIdentity(original)
		if err != nil || id != entry.TargetUUID {
			return nil, models.ErrEnrichmentInvalid
		}
		original.CollectionRevision = entry.ActivationCollectionRevision
		released, err := EnrichmentTargetIdentity(original)
		revision := 1
		if released == entry.TargetUUID {
			revision = entry.Revision + 1
		}
		if err != nil || released != entry.ReleasedTargetUUID || revision != entry.ReleasedRevision || releases[released] {
			return nil, models.ErrEnrichmentInvalid
		}
		releases[released] = true
	}
	expected, err := EnrichmentActivationDigest(plan)
	if err != nil || plan.PlanSHA256 != expected {
		return nil, models.ErrEnrichmentInvalid
	}
	return &plan, nil
}
