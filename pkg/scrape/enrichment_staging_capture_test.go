package scrape_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stretchr/testify/require"
)

func TestStagedCapturesPreserveParentsDeltasAndUnknownObservationTime(t *testing.T) {
	parent := map[string]any{"category": "reddit", "id": "album", "title": strings.Repeat("Retained caption ", 40),
		"author": map[string]any{"id": json.Number("9007199254740993")}, "removed": "original", "nullable": "original",
		"media_metadata": map[string]any{"one": map[string]any{"p": []any{"historical preview"}}}}
	next := map[string]any{}
	for key, value := range parent {
		next[key] = value
	}
	delete(next, "removed")
	next["nullable"] = nil
	child := map[string]any{"category": "redgifs", "id": "host-id", "_reddit": parent, "_parent": next}
	metadata := []map[string]any{parent, next, child, child}
	records := []any{}
	for _, body := range metadata {
		records = append(records, map[string]any{"kind": "media", "metadata": body})
	}
	raw, err := archive.EncodeSourceJSON(map[string]any{"records": records})
	require.NoError(t, err)
	_, normalized, reason := scrape.ConvertEnrichmentStaging(raw)
	require.Empty(t, reason)
	staged, err := scrape.DecodeStaging(normalized)
	require.NoError(t, err)
	require.NotNil(t, staged.Bodies[1].Base)
	require.Equal(t, []string{"removed"}, staged.Bodies[1].Removed)
	snapshot, post := uuid.NewString(), uuid.NewString()
	now := time.Date(2026, 10, 3, 10, 0, 0, 0, time.FixedZone("offset", -7*3600))
	captures, err := scrape.PrepareStagedCaptures(snapshot, 7, post, staged, now)
	require.NoError(t, err)
	require.Len(t, captures, 3, "duplicate record slots share one capture")
	for index, record := range staged.Records {
		capture := captures[record.Body]
		require.Equal(t, "legacy-enrichment", capture.Origin)
		require.Equal(t, "legacy-retained-v1", capture.RetentionPolicy)
		require.True(t, capture.CapturedAt.IsZero())
		require.Nil(t, capture.ExtractorVersion)
		require.Equal(t, now.UTC(), *capture.RecordedAt)
		body, err := archive.RestoreCapture(&capture.Payload)
		require.NoError(t, err)
		expected, err := archive.EncodeSourceJSON(metadata[index])
		require.NoError(t, err)
		require.Equal(t, string(expected), string(body))
	}
	later, err := scrape.PrepareStagedCaptures(snapshot, 7, post, staged, now.Add(time.Hour))
	require.NoError(t, err)
	for i, capture := range captures {
		require.Equal(t, capture.UUID, later[i].UUID, "evidence identity does not depend on review time")
	}
}

func TestStagedCapturesRejectInvalidGraphAndUnidentifiedBodies(t *testing.T) {
	_, body, reason := scrape.ConvertEnrichmentStaging([]byte(`{"records":[{"kind":"post","metadata":{"category":"reddit","id":"album"}}]}`))
	require.Empty(t, reason)
	for name, alter := range map[string]func(*scrape.EnrichmentStaging){
		"unknown format":   func(s *scrape.EnrichmentStaging) { s.Format = "future" },
		"invented time":    func(s *scrape.EnrichmentStaging) { s.ObservationTimeBasis = "observed" },
		"self base":        func(s *scrape.EnrichmentStaging) { index := 0; s.Bodies[0].Base = &index },
		"self parent":      func(s *scrape.EnrichmentStaging) { s.Bodies[0].Parents = map[string]int{"_reddit": 0} },
		"unknown parent":   func(s *scrape.EnrichmentStaging) { s.Bodies[0].Parents = map[string]int{"_other": 0} },
		"missing removal":  func(s *scrape.EnrichmentStaging) { s.Bodies[0].Removed = []string{"absent"} },
		"missing identity": func(s *scrape.EnrichmentStaging) { delete(s.Bodies[0].Patch, "id") },
		"oversized body": func(s *scrape.EnrichmentStaging) {
			s.Bodies[0].Patch["title"] = strings.Repeat("x", archive.MaxSourcePayloadBytes)
		},
	} {
		t.Run(name, func(t *testing.T) {
			staged, err := scrape.DecodeStaging(body)
			require.NoError(t, err)
			alter(staged)
			_, err = scrape.PrepareStagedCaptures(uuid.NewString(), 1, uuid.NewString(), staged, time.Now())
			require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
		})
	}
}
