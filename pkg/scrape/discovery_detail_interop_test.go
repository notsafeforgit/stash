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

func TestDiscoveryDetailThroughRealPythonCollector(t *testing.T) {
	python := os.Getenv("PRODUCER_PYTHON")
	if python == "" {
		t.Skip("PRODUCER_PYTHON is required for the producer integration fixture")
	}
	root, err := filepath.Abs("../../integrations/gallery-dl")
	require.NoError(t, err)
	for _, platform := range []string{"reddit", "twitter"} {
		t.Run(platform, func(t *testing.T) {
			values, page, detail, selected := detailFixture(t, platform)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, python, filepath.Join(root, "tests/discovery_detail_contract.py"))
			command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "src"), "PYTHONDONTWRITEBYTECODE=1")
			command.Stdin = bytes.NewReader(detailJSON(t, map[string]any{"platform": platform, "url": detail["url"], "metadata": detailPatch(detail)}))
			var errors bytes.Buffer
			command.Stderr = &errors
			body, err := command.Output()
			require.NoError(t, err, errors.String())
			var result struct {
				Runtime  string          `json:"runtime"`
				Weak     json.RawMessage `json:"weak"`
				Partial  json.RawMessage `json:"partial"`
				Complete json.RawMessage `json:"complete"`
			}
			require.NoError(t, json.Unmarshal(body, &result))
			pageBody := detailJSON(t, page)
			weak, err := scrape.MatchDiscoveryDetail(values, pageBody, selected, result.Runtime, result.Weak)
			require.NoError(t, err)
			require.Equal(t, "uncorroborated", weak.Status)
			if platform == "reddit" {
				partial, err := scrape.MatchDiscoveryDetail(values, pageBody, selected, result.Runtime, result.Partial)
				require.NoError(t, err)
				require.Equal(t, "pending", partial.Status)
				require.Nil(t, partial.WitnessOrdinal)
			}
			complete, err := scrape.MatchDiscoveryDetail(values, pageBody, selected, result.Runtime, result.Complete)
			require.NoError(t, err)
			require.Equal(t, "corroborated", complete.Status)
			require.Equal(t, "exact-title-and-date", complete.Basis)
			parsed, err := archive.ParseEnrichmentTranscript(result.Complete)
			require.NoError(t, err)
			require.Equal(t, len(parsed.Records), len(complete.RecordOrdinals))
			require.Len(t, parsed.Records[1].Patch, 2, "attachments share post metadata through their base record")
			job, post, producer := uuid.NewString(), uuid.NewString(), uuid.NewString()
			records := []models.EnrichmentCheckpointRecord{}
			for i := range parsed.Records {
				digest, err := parsed.RecordDigest(i)
				require.NoError(t, err)
				records = append(records, models.EnrichmentCheckpointRecord{JobUUID: job, Ordinal: i, CheckpointRevision: 1, Fence: 1, ProducerUUID: producer, Digest: digest})
			}
			captures, err := archive.PrepareDiscoveryDetailCaptures(job, post, selected, complete.URL, result.Runtime, result.Complete, records)
			require.NoError(t, err)
			require.Len(t, captures, len(parsed.Records))
			for i, record := range captures {
				raw, err := parsed.Metadata(i)
				require.NoError(t, err)
				restored, err := archive.RestoreCapture(&record.Input.Payload)
				require.NoError(t, err)
				require.Equal(t, raw, restored)
				if platform == "reddit" && i == len(captures)-1 {
					require.Contains(t, string(restored), "9223372036854775815")
					require.NotEmpty(t, record.Input.Contexts)
				}
			}
		})
	}
}
