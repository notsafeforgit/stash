package sqlite_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func discoveryPreviewBody(t *testing.T, pageBody json.RawMessage, url, extractor string) json.RawMessage {
	t.Helper()
	page, err := archive.DecodeJSONObject(pageBody, archive.MaxDiscoveryPageBytes)
	require.NoError(t, err)
	for _, row := range page["records"].([]any) {
		patch := row.(map[string]any)["patch"].(map[string]any)
		if _, exists := patch["source_extractor_url"]; exists {
			patch["source_extractor_url"] = url
		}
	}
	page["records"].([]any)[0].(map[string]any)["patch"].(map[string]any)["date"] = "2026-10-03"
	body, err := archive.EncodeSourceJSON(map[string]any{"schema": archive.EnrichmentTranscriptSchema, "url": url,
		"extractor_version": extractor, "retention_policy": archive.SourceRetentionVersion, "records": page["records"], "pending": []any{}, "unresolved": []any{}})
	require.NoError(t, err)
	return body
}

func TestDiscoveryDetailPreviewDoesNotAcceptIdentityOrClearBlockers(t *testing.T) {
	f := newDiscoveryMatchFixture(t)
	f.append(t, f.page)
	f.advance(t, 0)
	before := f.review(t)
	require.True(t, before.Candidate.NeedsDetail)
	body := discoveryPreviewBody(t, f.page, before.Candidate.URL, f.listing.ExtractorVersion)
	input := models.DiscoveryDetailPreviewInput{TargetUUID: f.target.UUID, ExpectedTargetRevision: before.Target.Revision,
		CandidateSequence: before.Candidate.Sequence, ExtractorVersion: f.listing.ExtractorVersion, Body: body}
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	counts := map[string]uint{}
	for _, table := range []string{"source_captures", "source_post_identifiers", "archive_jobs", "discovery_match_evidence", "discovery_match_publications"} {
		counts[table] = queryUint(t, raw, "SELECT count(*) FROM "+table)
	}
	var preview *models.DiscoveryDetailPreview
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		preview, err = f.repo.DiscoveryMatch.PreviewDetail(ctx, input)
		return err
	}))
	require.True(t, preview.PreviewOnly)
	require.Equal(t, "corroborated", preview.Evidence.Status)
	require.Equal(t, "exact-title-and-date", preview.Evidence.Basis)
	require.Equal(t, before.Target.UUID, preview.TargetUUID)
	require.Equal(t, before.Target.Revision, preview.TargetRevision)
	require.Equal(t, before.Blockers, preview.Blockers)
	require.Contains(t, preview.Blockers, "detail_required")
	require.Contains(t, preview.Blockers, "listing_incomplete")
	require.Equal(t, before, f.review(t))
	for table, count := range counts {
		require.Equal(t, count, queryUint(t, raw, "SELECT count(*) FROM "+table), table)
	}
	for _, test := range []struct {
		name   string
		change func(*models.DiscoveryDetailPreviewInput)
	}{
		{"stale target revision", func(i *models.DiscoveryDetailPreviewInput) { i.ExpectedTargetRevision++ }},
		{"different selected candidate", func(i *models.DiscoveryDetailPreviewInput) { i.CandidateSequence++ }},
		{"different detail runtime", func(i *models.DiscoveryDetailPreviewInput) { i.ExtractorVersion = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := input
			test.change(&changed)
			err := f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				_, err := f.repo.DiscoveryMatch.PreviewDetail(ctx, changed)
				return err
			})
			require.ErrorIs(t, err, models.ErrDiscoveryConflict)
		})
	}
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		input.TargetUUID = uuid.NewString()
		missing, err := f.repo.DiscoveryMatch.PreviewDetail(ctx, input)
		require.NoError(t, err)
		require.Nil(t, missing)
		input.TargetUUID = "bad"
		_, err = f.repo.DiscoveryMatch.PreviewDetail(ctx, input)
		require.ErrorIs(t, err, models.ErrDiscoveryInvalid)
		return nil
	}))
}
