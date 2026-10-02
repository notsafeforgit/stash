package scrape_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stretchr/testify/require"
)

func catalogCaptureFixture(t *testing.T) (*scrape.CatalogSnapshotManifest, map[string][]map[string]any) {
	t.Helper()
	_, m := snapshotManifest(t)
	rows := map[string][]map[string]any{}
	for _, chunk := range m.Chunks {
		body, err := os.ReadFile(filepath.Join("testdata/catalog_snapshot", chunk.File))
		require.NoError(t, err)
		for _, line := range bytes.SplitAfter(body, []byte{'\n'}) {
			if len(line) == 0 {
				continue
			}
			row, err := m.Record(line)
			require.NoError(t, err)
			rows[row.Table] = append(rows[row.Table], row.Values)
		}
	}
	return m, rows
}

func TestCatalogCaptureReconstructsPythonInventoryAndNativePayload(t *testing.T) {
	m, rows := catalogCaptureFixture(t)
	load := func(key string) (map[string]any, error) {
		require.Equal(t, key, rows["account_snapshots"][0]["snapshot_id"])
		return scrape.CatalogProfile(rows["account_snapshots"][0])
	}
	sort.Slice(rows["observations"], func(i, j int) bool {
		a, b := rows["observations"][i], rows["observations"][j]
		if a["post_key"] != b["post_key"] {
			return a["post_key"].(string) < b["post_key"].(string)
		}
		return a["observation_id"].(string) < b["observation_id"].(string)
	})
	sort.Slice(rows["observation_details"], func(i, j int) bool {
		a, b := rows["observation_details"][i], rows["observation_details"][j]
		if a["captured_at"] != b["captured_at"] {
			return a["captured_at"].(string) < b["captured_at"].(string)
		}
		return a["capture_id"].(string) < b["capture_id"].(string)
	})
	hashed := sha256.New()
	var count, refs, flat int64
	for _, observation := range rows["observations"] {
		var details []map[string]any
		for _, detail := range rows["observation_details"] {
			if detail["observation_id"] == observation["observation_id"] {
				details = append(details, detail)
			}
		}
		if len(details) == 0 {
			details = append(details, nil)
		}
		for _, detail := range details {
			capture, err := scrape.ReconstructCatalogCapture(observation, detail, "reddit", load)
			require.NoError(t, err)
			hashed.Write(capture.Header)
			hashed.Write([]byte{'\n'})
			count++
			refs += int64(capture.ProfileReferences)
			if capture.Flat {
				flat++
			}
			input, err := capture.NativeInput("33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444")
			require.NoError(t, err)
			reconstructed, err := archive.RestoreCapture(&input.Payload)
			require.NoError(t, err)
			value, err := archive.DecodeJSONObject(reconstructed, archive.MaxSourcePayloadBytes)
			require.NoError(t, err)
			legacy, err := scrape.LegacyCatalogJSON(value, archive.MaxSourcePayloadBytes)
			require.NoError(t, err)
			require.Equal(t, []byte(capture.Payload), legacy)
		}
	}
	require.Equal(t, m.Captures.SHA256, hex.EncodeToString(hashed.Sum(nil)))
	require.Equal(t, m.Captures.Count, count)
	require.Equal(t, m.Captures.Flat, flat)
	require.Equal(t, m.Captures.ProfileRefs, refs)
}

func TestCatalogCaptureProfileChecksAndParentMerge(t *testing.T) {
	_, rows := catalogCaptureFixture(t)
	profile := rows["account_snapshots"][0]
	load := func(string) (map[string]any, error) { return scrape.CatalogProfile(profile) }
	observation := rows["observations"][1]
	observation["payload_json"] = `{"_reddit":{"id":"album","items":[{"user":null}]}}`
	refs, err := json.Marshal([]any{[]any{[]any{"_reddit", "items", 0, "user"}, profile["snapshot_id"]}})
	require.NoError(t, err)
	observation["account_refs_json"] = string(refs)
	detail := rows["observation_details"][0]
	detail["payload_patch"] = `{"_reddit":{"num":2},"filename":"nested"}`
	capture, err := scrape.ReconstructCatalogCapture(observation, detail, "reddit", load)
	require.NoError(t, err)
	var value map[string]any
	require.NoError(t, json.Unmarshal(capture.Payload, &value))
	parent := value["_reddit"].(map[string]any)
	require.Equal(t, "album", parent["id"])
	require.EqualValues(t, 2, parent["num"])
	require.Equal(t, "Juniper 🌿", parent["items"].([]any)[0].(map[string]any)["user"].(map[string]any)["name"])
	for _, path := range []any{[]any{"_reddit", "items", -1, "user"}, []any{"_reddit", "id"}, []any{"_reddit", "items", true, "user"}} {
		refs, err := json.Marshal([]any{[]any{path, profile["snapshot_id"]}})
		require.NoError(t, err)
		observation["account_refs_json"] = string(refs)
		_, err = scrape.ReconstructCatalogCapture(observation, detail, "reddit", load)
		require.Error(t, err)
	}
	observation["account_refs_json"] = string(refs)
	profile["payload_json"] = `{"id":"wrong"}`
	_, err = scrape.ReconstructCatalogCapture(observation, detail, "reddit", load)
	require.Error(t, err)
}

func TestLegacyCatalogJSONMatchesPythonScalarConventions(t *testing.T) {
	input := []byte(`{"values":[-0,0.0,-0.0,1e0,1e-5,1e-4,1e6,1e15,1e16,123456789012345678901234567890,"\u2028\u2029🌿\n\u0000<&>"]}`)
	value, err := archive.DecodeJSONObject(input, 4096)
	require.NoError(t, err)
	body, err := scrape.LegacyCatalogJSON(value, 4096)
	require.NoError(t, err)
	require.Equal(t, "{\"values\":[0,0.0,-0.0,1.0,1e-05,0.0001,1000000.0,1000000000000000.0,1e+16,123456789012345678901234567890,\"\u2028\u2029🌿\\n\\u0000<&>\"]}", string(body))
	_, err = scrape.LegacyCatalogJSON(value, 16)
	require.Error(t, err)
}
