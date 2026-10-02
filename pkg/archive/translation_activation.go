package archive

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

const MaxTranslationActivationTargets = 100
const MaxTranslationActivationBytes = 128 << 10

func PrepareTranslationActivation(input models.TranslationActivationInput) (models.TranslationActivationInput, string, error) {
	if !translationUUID(input.UUID) || len(input.Targets) == 0 || len(input.Targets) > MaxTranslationActivationTargets ||
		(input.SnapshotUUID == "") != (input.ManifestSHA256 == "") ||
		(input.SnapshotUUID != "" && (!translationUUID(input.SnapshotUUID) || !ValidSHA256(input.ManifestSHA256))) {
		return input, "", models.ErrTranslationWorkInvalid
	}
	input.Targets = slices.Clone(input.Targets)
	slices.SortFunc(input.Targets, func(a, b models.TranslationTargetRef) int { return strings.Compare(a.TargetUUID, b.TargetUUID) })
	previous := ""
	for _, ref := range input.Targets {
		if !translationUUID(ref.TargetUUID) || ref.TargetUUID == previous || ref.Revision < 1 || ref.Revision == int(^uint(0)>>1) {
			return input, "", models.ErrTranslationWorkInvalid
		}
		previous = ref.TargetUUID
	}
	body, err := json.Marshal(input)
	return input, translationDigest(body), err
}

func TranslationActivationDigest(plan models.TranslationActivationPlan) (string, error) {
	plan.PlanSHA256 = ""
	body, err := json.Marshal(plan)
	if err != nil || len(body) > MaxTranslationActivationBytes {
		return "", models.ErrTranslationWorkInvalid
	}
	return translationDigest(body), nil
}

func DecodeTranslationActivationPlan(body []byte) (*models.TranslationActivationPlan, error) {
	if _, err := DecodeJSONObject(body, MaxTranslationActivationBytes); err != nil {
		return nil, models.ErrTranslationWorkInvalid
	}
	var plan models.TranslationActivationPlan
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(&plan); err != nil {
		return nil, models.ErrTranslationWorkInvalid
	}
	input, _, err := PrepareTranslationActivation(plan.Input)
	if err != nil || plan.Version != 1 || !slices.Equal(input.Targets, plan.Input.Targets) || len(plan.Entries) != len(input.Targets) {
		return nil, models.ErrTranslationWorkInvalid
	}
	for i, entry := range plan.Entries {
		if entry.TranslationTargetRef != input.Targets[i] || !translationUUID(entry.RequestUUID) || !translationUUID(entry.PostUUID) ||
			(entry.CollectionUUID == nil) != (entry.CollectionRevision == nil) ||
			(entry.CollectionUUID != nil && (!translationUUID(*entry.CollectionUUID) || *entry.CollectionRevision < 1)) ||
			(entry.Field != "title" && entry.Field != "caption") || entry.Priority < 0 || entry.Priority > 100 ||
			entry.NotBefore.UnixMilli() <= 0 || entry.NotBefore.UTC().Year() > 9999 {
			return nil, models.ErrTranslationWorkInvalid
		}
	}
	expected, err := TranslationActivationDigest(plan)
	if err != nil || plan.PlanSHA256 != expected {
		return nil, models.ErrTranslationWorkInvalid
	}
	return &plan, nil
}
