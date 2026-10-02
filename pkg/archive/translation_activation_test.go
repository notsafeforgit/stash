package archive_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestTranslationActivationPlanBoundsAndCanonicalIdentity(t *testing.T) {
	input := models.TranslationActivationInput{UUID: uuid.NewString(), Targets: []models.TranslationTargetRef{{TargetUUID: uuid.NewString(), Revision: 2}, {TargetUUID: uuid.NewString(), Revision: 1}}}
	original := slices.Clone(input.Targets)
	prepared, digest, err := archive.PrepareTranslationActivation(input)
	require.NoError(t, err)
	require.Equal(t, original, input.Targets)
	slices.Reverse(input.Targets)
	reversed, other, err := archive.PrepareTranslationActivation(input)
	require.NoError(t, err)
	require.Equal(t, prepared, reversed)
	require.Equal(t, digest, other)
	for _, invalid := range []models.TranslationActivationInput{
		{}, {UUID: input.UUID}, {UUID: "invalid", Targets: input.Targets},
		{UUID: input.UUID, Targets: []models.TranslationTargetRef{{TargetUUID: uuid.NewString()}}},
		{UUID: input.UUID, Targets: []models.TranslationTargetRef{input.Targets[0], input.Targets[0]}},
		{UUID: input.UUID, SnapshotUUID: uuid.NewString(), Targets: input.Targets},
		{UUID: input.UUID, ManifestSHA256: strings.Repeat("a", 64), Targets: input.Targets},
		{UUID: input.UUID, Targets: make([]models.TranslationTargetRef, 101)},
	} {
		_, _, err := archive.PrepareTranslationActivation(invalid)
		require.ErrorIs(t, err, models.ErrTranslationWorkInvalid)
	}
	plan := models.TranslationActivationPlan{Version: 1, Input: prepared}
	for _, ref := range prepared.Targets {
		plan.Entries = append(plan.Entries, models.TranslationActivationEntry{TranslationTargetRef: ref, RequestUUID: uuid.NewString(), PostUUID: uuid.NewString(), Field: "caption", Priority: 20, NotBefore: time.Now().UTC()})
	}
	plan.PlanSHA256, err = archive.TranslationActivationDigest(plan)
	require.NoError(t, err)
	body, err := json.Marshal(plan)
	require.NoError(t, err)
	decoded, err := archive.DecodeTranslationActivationPlan(body)
	require.NoError(t, err)
	require.Equal(t, plan, *decoded)
	for _, invalid := range []string{
		`null`, `[]`, string(body) + `{}`, strings.Replace(string(body), `"version":1`, `"version":1,"version":1`, 1),
		strings.Replace(string(body), `"version":1`, `"version":1,"unknown":true`, 1),
		strings.Replace(string(body), `"priority":20`, `"priority":21`, 1),
	} {
		_, err := archive.DecodeTranslationActivationPlan([]byte(invalid))
		require.ErrorIs(t, err, models.ErrTranslationWorkInvalid)
	}
	plan.Entries[0].CollectionUUID = &input.UUID
	plan.PlanSHA256, err = archive.TranslationActivationDigest(plan)
	require.NoError(t, err)
	body, err = json.Marshal(plan)
	require.NoError(t, err)
	_, err = archive.DecodeTranslationActivationPlan(body)
	require.ErrorIs(t, err, models.ErrTranslationWorkInvalid, "a valid hash does not excuse an invalid entry")
}
