package archive

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
)

const MaxEnrichmentRebindBytes = 8 << 20

func PrepareEnrichmentRebind(input models.EnrichmentRebindInput) (models.EnrichmentRebindInput, string, error) {
	if !translationUUID(input.UUID) || !translationUUID(input.CollectionUUID) || input.CollectionRevision < 1 ||
		len(input.Targets) < 1 || len(input.Targets) > MaxEnrichmentActivationTargets || len(input.Reason) > 4096 || !utf8.ValidString(input.Reason) {
		return input, "", models.ErrEnrichmentInvalid
	}
	input.Targets = slices.Clone(input.Targets)
	slices.SortFunc(input.Targets, func(a, b models.EnrichmentTargetRef) int { return strings.Compare(a.TargetUUID, b.TargetUUID) })
	previous := ""
	for _, ref := range input.Targets {
		// Holding and consuming the old target each retain a history revision.
		if !translationUUID(ref.TargetUUID) || ref.TargetUUID == previous || ref.Revision < 1 || ref.Revision > int(^uint(0)>>1)-2 {
			return input, "", models.ErrEnrichmentInvalid
		}
		previous = ref.TargetUUID
	}
	body, err := json.Marshal(input)
	return input, translationDigest(body), err
}

func EnrichmentRebindActivationUUID(id string) string {
	namespace, err := uuid.Parse(id)
	if err != nil || namespace == uuid.Nil || namespace.String() != id {
		return ""
	}
	return uuid.NewSHA1(namespace, []byte("enrichment-collection-rebind-v1")).String()
}

func EnrichmentRebindDigest(plan models.EnrichmentRebindPlan) (string, error) {
	plan.PlanSHA256 = ""
	body, err := json.Marshal(plan)
	if err != nil || len(body) > MaxEnrichmentRebindBytes {
		return "", models.ErrEnrichmentInvalid
	}
	return translationDigest(body), nil
}

func DecodeEnrichmentRebindPlan(body []byte) (*models.EnrichmentRebindPlan, error) {
	if _, err := DecodeJSONObject(body, MaxEnrichmentRebindBytes); err != nil {
		return nil, models.ErrEnrichmentInvalid
	}
	var plan models.EnrichmentRebindPlan
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return nil, models.ErrEnrichmentInvalid
	}
	input, _, err := PrepareEnrichmentRebind(plan.Input)
	if err != nil || plan.Version != 1 || !slices.Equal(input.Targets, plan.Input.Targets) ||
		plan.Collection.UUID != input.CollectionUUID || plan.Collection.Revision != input.CollectionRevision || plan.Collection.State != "active" {
		return nil, models.ErrEnrichmentInvalid
	}
	nested, err := json.Marshal(plan.Activation)
	if err != nil {
		return nil, err
	}
	activation, err := DecodeEnrichmentActivationPlan(nested)
	if err != nil || activation.Input.UUID != EnrichmentRebindActivationUUID(input.UUID) ||
		activation.Input.SnapshotUUID != "" || activation.Input.ManifestSHA256 != "" || len(activation.Entries) != len(input.Targets) {
		return nil, models.ErrEnrichmentInvalid
	}
	for i, entry := range activation.Entries {
		ref := input.Targets[i]
		if entry.TargetUUID != ref.TargetUUID || entry.Revision != ref.Revision+1 || entry.CollectionUUID != input.CollectionUUID ||
			entry.CollectionRevision >= input.CollectionRevision || entry.ActivationCollectionRevision != input.CollectionRevision ||
			entry.ReleasedTargetUUID == entry.TargetUUID || entry.ReleasedRevision != 1 {
			return nil, models.ErrEnrichmentInvalid
		}
	}
	digest, err := EnrichmentRebindDigest(plan)
	if err != nil || digest != plan.PlanSHA256 {
		return nil, models.ErrEnrichmentInvalid
	}
	return &plan, nil
}
