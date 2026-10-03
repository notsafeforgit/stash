package scrape_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stretchr/testify/require"
)

func TestLegacyEnrichmentSeedResumesThroughRealPythonCollector(t *testing.T) {
	python := os.Getenv("PRODUCER_PYTHON")
	if python == "" {
		t.Skip("PRODUCER_PYTHON is required for the producer integration fixture")
	}
	accepted, body := legacySeedFixture(t, nil)
	// Obtain the pinned producer runtime rather than duplicating it in Go.
	root, err := filepath.Abs("../../integrations/gallery-dl")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	version := exec.CommandContext(ctx, python, "-c", "from stash_ingest.gallery import SUPPORTED_VERSION; print(SUPPORTED_VERSION)")
	version.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "src"), "PYTHONDONTWRITEBYTECODE=1")
	stamp, err := version.Output()
	require.NoError(t, err)
	seed, err := scrape.PrepareLegacyEnrichmentSeed(accepted, body, string(bytes.TrimSpace(stamp)))
	require.NoError(t, err)
	command := exec.CommandContext(ctx, python, filepath.Join(root, "tests", "retained_seed_roundtrip.py"))
	command.Env, command.Stdin = version.Env, bytes.NewReader(seed)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	result, err := command.Output()
	require.NoError(t, err, stderr.String())
	before, err := archive.ParseEnrichmentTranscript(seed)
	require.NoError(t, err)
	after, err := archive.ParseEnrichmentTranscript(result)
	require.NoError(t, err)
	require.True(t, after.Extends(before))
	require.Len(t, after.Records, len(before.Records)+1)
	require.Empty(t, after.Pending)
	for i := range before.Records {
		prior, err := before.Metadata(i)
		require.NoError(t, err)
		actual, err := after.Metadata(i)
		require.NoError(t, err)
		require.Equal(t, prior, actual)
	}
	last := len(after.Records) - 1
	require.NotEmpty(t, after.Records[last].ObservedAt)
	require.Nil(t, after.Records[last].RetainedCapture)
	child, err := after.Metadata(last)
	require.NoError(t, err)
	require.Contains(t, string(child), "9007199254740993")
	require.Contains(t, string(child), "old preview")
	link, err := after.CaptureContext(last, uuid.NewString(), accepted.Captures[1].CaptureUUID)
	require.NoError(t, err)
	require.Equal(t, "/_parent", link.Path)
	require.NoError(t, archive.CaptureContextRetention(child, []models.SourceCaptureContext{*link}))
}

func legacySeedFixture(t *testing.T, change func(map[string]any)) (*models.CheckpointEvidenceAcceptance, []byte) {
	t.Helper()
	parent := map[string]any{"category": "reddit", "id": "saved", "title": "Retained caption",
		"author":         map[string]any{"id": json.Number("9007199254740993")},
		"media_metadata": map[string]any{"one": map[string]any{"p": []any{"old preview"}}}}
	child := map[string]any{"category": "redgifs", "id": "oldchild", "_reddit": parent, "_parent": parent}
	value := map[string]any{"extractor_version": "old-runtime", "records": []any{
		map[string]any{"kind": "post", "metadata": parent}, map[string]any{"kind": "media", "metadata": child},
		map[string]any{"kind": "media", "metadata": child}},
		"pending_children": []any{
			map[string]any{"url": "https://imgur.com/a/pending", "parent": child, "depth": 2},
			map[string]any{"url": "https://imgur.com/a/pending", "parent": child, "depth": 2}},
		"unresolved": []any{map[string]any{"url": "https://example.invalid/unknown", "reason": "historical-reason"}}}
	if change != nil {
		change(value)
	}
	raw, err := archive.EncodeSourceJSON(value)
	require.NoError(t, err)
	staged, body, reason := scrape.ConvertEnrichmentStaging(raw)
	require.Empty(t, reason)
	accepted := &models.CheckpointEvidenceAcceptance{CheckpointEvidencePlan: models.CheckpointEvidencePlan{
		Version: 1, Input: models.CheckpointEvidenceInput{SnapshotUUID: uuid.NewString(), Ordinal: 7},
		BodySHA256: scrape.CatalogSnapshotSHA(body),
		Target: models.EnrichmentTarget{EnrichmentTargetInput: models.EnrichmentTargetInput{PostUUID: uuid.NewString()},
			URL: "https://www.reddit.com/comments/saved"},
		RecordCount: len(staged.Records), PendingCount: len(staged.Pending), UnscopedCount: len(staged.Unresolved)},
		CreatedAt: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)}
	captures, err := scrape.PrepareStagedCaptures(accepted.Input.SnapshotUUID, accepted.Input.Ordinal, accepted.Target.PostUUID, staged, accepted.CreatedAt)
	require.NoError(t, err)
	for i, capture := range captures {
		raw, err := archive.RestoreCapture(&capture.Payload)
		require.NoError(t, err)
		accepted.Captures = append(accepted.Captures, models.CheckpointEvidenceCapture{
			BodyIndex: i, CaptureUUID: capture.UUID, PayloadSHA: scrape.CatalogSnapshotSHA(raw)})
	}
	return accepted, body
}

func TestLegacyEnrichmentSeedKeepsAcceptedCapturesAndScopedChildren(t *testing.T) {
	accepted, body := legacySeedFixture(t, nil)
	before := string(body)
	seed, err := scrape.PrepareLegacyEnrichmentSeed(accepted, body, "current-runtime")
	require.NoError(t, err)
	parsed, err := archive.ParseEnrichmentTranscript(seed)
	require.NoError(t, err)
	require.Equal(t, archive.EnrichmentRetainedSchema, parsed.Schema)
	require.Len(t, parsed.Records, 2, "duplicate old slots share their original capture")
	require.Len(t, parsed.Pending, 1, "identical child work is scheduled once")
	require.Equal(t, archive.EnrichmentReference{URL: "https://imgur.com/a/pending", Parent: 1, Depth: 2, Reason: "legacy_pending"}, parsed.Pending[0])
	require.Empty(t, parsed.Unresolved, "unscoped old references cannot be assigned an invented parent")
	require.Equal(t, 1, accepted.UnscopedCount, "unscoped evidence remains independently visible")
	for i, record := range parsed.Records {
		require.Equal(t, accepted.Captures[i].CaptureUUID, *record.RetainedCapture)
		require.Empty(t, record.ObservedAt)
		restored, err := parsed.Metadata(i)
		require.NoError(t, err)
		require.Equal(t, accepted.Captures[i].PayloadSHA, scrape.CatalogSnapshotSHA(restored))
		require.Contains(t, string(restored), "9007199254740993")
		require.Contains(t, string(restored), "old preview")
	}
	child, err := parsed.Metadata(1)
	require.NoError(t, err)
	require.Contains(t, string(child), `"_reddit"`)
	require.Contains(t, string(child), `"_parent"`)
	require.Equal(t, before, string(body))
}

func TestLegacyEnrichmentSeedRejectsReboundOrCorruptEvidence(t *testing.T) {
	for name, change := range map[string]func(*models.CheckpointEvidenceAcceptance){
		"body changed": func(a *models.CheckpointEvidenceAcceptance) {
			a.BodySHA256 = scrape.CatalogSnapshotSHA([]byte("other"))
		},
		"capture rebound": func(a *models.CheckpointEvidenceAcceptance) { a.Captures[0].CaptureUUID = uuid.NewString() },
		"payload changed": func(a *models.CheckpointEvidenceAcceptance) {
			a.Captures[0].PayloadSHA = scrape.CatalogSnapshotSHA([]byte("other"))
		},
		"body order changed":  func(a *models.CheckpointEvidenceAcceptance) { a.Captures[0].BodyIndex = 1 },
		"missing capture":     func(a *models.CheckpointEvidenceAcceptance) { a.Captures = a.Captures[:1] },
		"missing record slot": func(a *models.CheckpointEvidenceAcceptance) { a.RecordCount-- },
		"missing pending":     func(a *models.CheckpointEvidenceAcceptance) { a.PendingCount-- },
		"missing unscoped":    func(a *models.CheckpointEvidenceAcceptance) { a.UnscopedCount-- },
	} {
		t.Run(name, func(t *testing.T) {
			accepted, body := legacySeedFixture(t, nil)
			change(accepted)
			_, err := scrape.PrepareLegacyEnrichmentSeed(accepted, body, "current-runtime")
			require.ErrorIs(t, err, models.ErrSourcePayloadCorrupt)
		})
	}
}

func TestLegacyEnrichmentSeedDoesNotInventSafeExecutionForInvalidPending(t *testing.T) {
	for name, change := range map[string]func(map[string]any){
		"wrong depth": func(v map[string]any) { v["pending_children"].([]any)[0].(map[string]any)["depth"] = 1 },
		"private URL": func(v map[string]any) {
			v["pending_children"].([]any)[0].(map[string]any)["url"] = "https://user:secret@example.invalid/a"
		},
		"competing ancestry": func(v map[string]any) {
			v["records"].([]any)[1].(map[string]any)["metadata"].(map[string]any)["_parent"] = map[string]any{"category": "twitter", "tweet_id": "123"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			accepted, body := legacySeedFixture(t, change)
			_, err := scrape.PrepareLegacyEnrichmentSeed(accepted, body, "current-runtime")
			require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
		})
	}
}
