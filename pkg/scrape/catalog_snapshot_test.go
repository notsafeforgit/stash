package scrape_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stretchr/testify/require"
)

func snapshotManifest(t *testing.T) ([]byte, *scrape.CatalogSnapshotManifest) {
	t.Helper()
	body, err := os.ReadFile("testdata/catalog_snapshot/manifest.json")
	require.NoError(t, err)
	m, err := scrape.PrepareCatalogSnapshot(body, scrape.CatalogSnapshotSHA(body))
	require.NoError(t, err)
	return body, m
}

func TestCatalogSnapshotPythonBytesAndRecordTypes(t *testing.T) {
	_, m := snapshotManifest(t)
	var previousTable string
	var previousKey []any
	var rows int64
	var binary bool
	for _, chunk := range m.Chunks {
		body, err := os.ReadFile(filepath.Join("testdata/catalog_snapshot", chunk.File))
		require.NoError(t, err)
		require.Equal(t, chunk.SHA256, scrape.CatalogSnapshotSHA(body))
		for _, line := range bytes.SplitAfter(body, []byte{'\n'}) {
			if len(line) == 0 {
				continue
			}
			r, err := m.Record(line)
			require.NoError(t, err)
			require.True(t, scrape.CatalogRecordAfter(r.Table, r.Key, previousTable, previousKey))
			previousTable, previousKey = r.Table, r.Key
			if r.Table == "sidecar_documents" {
				binary = true
				require.Equal(t, "b3JpZ2luYWwA/2RvY3VtZW50", r.Values["raw_content"].(map[string]any)["sqlite_blob_base64"])
			}
			rows++
		}
	}
	require.True(t, binary)
	require.Equal(t, m.Records, rows)
	require.EqualValues(t, 3, m.Captures.Count)
	// Numeric ordering and binary text ordering must not follow JSON spelling.
	require.True(t, scrape.CatalogRecordAfter("t", []any{json.Number("10")}, "t", []any{json.Number("2")}))
	require.False(t, scrape.CatalogRecordAfter("t", []any{json.Number("2")}, "t", []any{json.Number("10")}))
	require.True(t, scrape.CatalogRecordAfter("t", []any{"a"}, "t", []any{"\n"}))
	require.False(t, scrape.CatalogRecordAfter("t", []any{"same"}, "t", []any{"same"}))
}

func TestCatalogSnapshotRejectsUnrecognizedManifest(t *testing.T) {
	body, _ := snapshotManifest(t)
	for _, alter := range []func(map[string]any){
		func(m map[string]any) { m["reader"] = "unknown" },
		func(m map[string]any) { m["extra"] = true },
		func(m map[string]any) { m["application_id"] = 0 },
		func(m map[string]any) { m["schema"].([]any)[0].(map[string]any)["type"] = "unknown" },
		func(m map[string]any) { m["chunks"].([]any)[0].(map[string]any)["file"] = "../outside" },
		func(m map[string]any) { m["chunks"].([]any)[0].(map[string]any)["bytes"] = 17000000 },
		func(m map[string]any) {
			m["tables"].(map[string]any)["unhandled"] = m["tables"].(map[string]any)["posts"]
		},
		func(m map[string]any) {
			m["tables"].(map[string]any)["observations"].(map[string]any)["key"] = []string{"post_key"}
		},
		func(m map[string]any) {
			m["tables"].(map[string]any)["observations"].(map[string]any)["columns"].([]any)[0].(map[string]any)["hidden"] = 1
		},
		func(m map[string]any) { m["references"].(map[string]any)["observations.post_key"] = 99 },
		func(m map[string]any) { m["captures"].(map[string]any)["count"] = 99 },
	} {
		var m map[string]any
		require.NoError(t, json.Unmarshal(body, &m))
		alter(m)
		changed, err := archive.EncodeSourceJSON(m)
		require.NoError(t, err)
		_, err = scrape.PrepareCatalogSnapshot(changed, scrape.CatalogSnapshotSHA(changed))
		require.ErrorIs(t, err, models.ErrCatalogSnapshotInvalid)
	}
	_, err := scrape.PrepareCatalogSnapshot(body, scrape.CatalogSnapshotSHA(nil))
	require.ErrorIs(t, err, models.ErrCatalogSnapshotInvalid)
}

func TestCatalogSnapshotRejectsChangedKeysBinaryAndJSON(t *testing.T) {
	_, m := snapshotManifest(t)
	for _, chunk := range m.Chunks {
		body, err := os.ReadFile(filepath.Join("testdata/catalog_snapshot", chunk.File))
		require.NoError(t, err)
		for _, line := range bytes.SplitAfter(body, []byte{'\n'}) {
			if len(line) == 0 {
				continue
			}
			original, err := archive.DecodeJSONObject(line, scrape.CatalogChunkLimit)
			require.NoError(t, err)
			values := original["values"].(map[string]any)
			switch original["table"] {
			case "observations":
				values["payload_json"] = `{"duplicate":1,"duplicate":2}`
			case "sidecar_documents":
				values["raw_content"] = map[string]any{"sqlite_blob_base64": "Y2hhbmdlZA=="}
			default:
				original["key"] = []any{true}
			}
			changed, err := archive.EncodeSourceJSON(original)
			require.NoError(t, err)
			_, err = m.Record(append(changed, '\n'))
			require.ErrorIs(t, err, models.ErrCatalogSnapshotInvalid)
		}
	}
}

func TestCatalogSnapshotRetainsKnownViewTriggersWithoutExecutingThem(t *testing.T) {
	body, _ := snapshotManifest(t)
	var m map[string]any
	require.NoError(t, json.Unmarshal(body, &m))
	m["schema"] = append(m["schema"].([]any), map[string]any{"type": "trigger", "name": "sidecars_delete", "tbl_name": "sidecars", "sql": "CREATE TRIGGER sidecars_delete INSTEAD OF DELETE ON sidecars BEGIN DELETE FROM sidecar_sources WHERE relpath=OLD.relpath; END"})
	body, err := archive.EncodeSourceJSON(m)
	require.NoError(t, err)
	_, err = scrape.PrepareCatalogSnapshot(body, scrape.CatalogSnapshotSHA(body))
	require.NoError(t, err)
	m["schema"].([]any)[len(m["schema"].([]any))-1].(map[string]any)["tbl_name"] = "unknown_view"
	body, err = archive.EncodeSourceJSON(m)
	require.NoError(t, err)
	_, err = scrape.PrepareCatalogSnapshot(body, scrape.CatalogSnapshotSHA(body))
	require.ErrorIs(t, err, models.ErrCatalogSnapshotInvalid)
}
