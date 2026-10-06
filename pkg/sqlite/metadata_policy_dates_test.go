package sqlite_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestMetadataPolicyConvertsSourceDatesWithoutChangingEvidence(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	capture, attachment := attachmentFixture(t, f.repo)
	require.NoError(t, applyMediaChoice(f.repo, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision,
		State: "linked", MediaUUID: f.entity.UUID, ExpectedMediaRevision: f.entity.Revision, Origin: "review"}))
	policy := f.put(t, 0, models.MetadataPolicyRule{OnCreate: true, OnExisting: true, MarkOrganized: true, Mappings: map[string]models.MetadataMapping{
		"date": {JQ: `.source.metadata.published_at | utc_date | select(. != null)`},
	}})
	input := f.input(policy)
	manual := policyPreview(t, f.repo, input)
	require.Equal(t, "omitted", policyChange(t, manual, "date").Status)
	assertOriginal := func(id, original string) {
		t.Helper()
		require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			stored, err := f.repo.SourceEvidence.FindCapture(ctx, id)
			require.NoError(t, err)
			require.Equal(t, original, *stored.Metadata.PublishedAt)
			return nil
		}))
	}
	for index, tc := range []struct {
		original string
		want     string
	}{
		{"2026-09-29T22:37:08.123456789-07:00", "2026-09-30"},
		{"2026-02-30", ""},
		{"2026-09-29T22:37:08", ""},
		{"2026-07-05", "2026-07-05"},
	} {
		candidate := sourceTestCapture(t, capture.PostUUID, index+2, "Profile")
		candidate.CapturedAt = candidate.CapturedAt.Add(time.Duration(index+1) * time.Second)
		candidate.Metadata.PublishedAt = &tc.original
		selected := recordSourceTestCapture(t, f.repo, candidate)
		recordAttachmentManifest(t, f.repo, models.SourceAttachmentManifestInput{CaptureUUID: selected.UUID, Complete: true,
			Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, attachment.Reference.Value)}})
		require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			return f.repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, CaptureUUID: selected.UUID})
		}))
		input.Source = &metadata.Source{CaptureUUID: selected.UUID, AttachmentUUID: attachment.UUID}
		preview := policyPreview(t, f.repo, input)
		change := policyChange(t, preview, "date")
		if tc.want == "" {
			require.Equal(t, "review", change.Status)
			require.Contains(t, change.Message, "utc_date")
			applied, err := applyPolicy(f.repo, input, preview.Digest)
			require.NoError(t, err)
			require.Empty(t, applied.AppliedFields())
			require.Equal(t, `"2026-09-30"`, string(metadataState(t, f.repo, f.entity.UUID, "date").Value))
		} else {
			require.Equal(t, "ready", change.Status)
			want, err := json.Marshal(tc.want)
			require.NoError(t, err)
			require.JSONEq(t, string(want), string(change.Value))
			_, err = applyPolicy(f.repo, input, preview.Digest)
			require.NoError(t, err)
			state := metadataState(t, f.repo, f.entity.UUID, "date")
			require.JSONEq(t, string(want), string(state.Value))
			require.Equal(t, selected.UUID, *state.Decision.CaptureUUID)
		}
		assertOriginal(selected.UUID, tc.original)
	}
	_, err := applyMetadata(f.repo, metadataInput(metadataState(t, f.repo, f.entity.UUID, "date"), "clear", "review", ""), false)
	require.NoError(t, err)
	preview := policyPreview(t, f.repo, input)
	require.Equal(t, "protected", policyChange(t, preview, "date").Status)
	_, err = applyPolicy(f.repo, input, preview.Digest)
	require.NoError(t, err)
	require.Equal(t, "null", string(metadataState(t, f.repo, f.entity.UUID, "date").Value))
}
