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

func automationManifest(t *testing.T) ([]byte, *scrape.AutomationSnapshotManifest) {
	t.Helper()
	body, err := os.ReadFile("testdata/automation_snapshot/manifest.json")
	require.NoError(t, err)
	m, err := scrape.PrepareAutomationSnapshot(body, scrape.CatalogSnapshotSHA(body))
	require.NoError(t, err)
	return body, m
}

func TestAutomationSnapshotPythonSQLiteValuesAndEvidence(t *testing.T) {
	_, m := automationManifest(t)
	require.Len(t, m.Tables, 10)
	require.EqualValues(t, 1, m.Integrity.ForeignKeyViolations)
	var previousTable string
	var previousKey []any
	var rows int64
	var cached, binary bool
	for _, chunk := range m.Chunks {
		body, err := os.ReadFile(filepath.Join("testdata/automation_snapshot", chunk.File))
		require.NoError(t, err)
		require.Equal(t, chunk.SHA256, scrape.CatalogSnapshotSHA(body))
		require.Equal(t, chunk.Bytes, len(body))
		for _, line := range bytes.SplitAfter(body, []byte{'\n'}) {
			if len(line) == 0 {
				continue
			}
			r, err := m.Record(line)
			require.NoError(t, err)
			require.Equal(t, line, r.Line)
			require.True(t, scrape.CatalogRecordAfter(r.Table, r.Key, previousTable, previousKey))
			previousTable, previousKey = r.Table, r.Key
			if r.Table == "translation_jobs" && r.Key[0] == "cached" {
				cached = true
				require.Equal(t, `{"unfinished":`, r.Values["result_json"])
				require.Equal(t, "Exact original 🌿\n", r.Values["original_text"])
				require.Equal(t, json.Number("1790949970.125"), r.Values["next_attempt"])
			}
			if r.Table == "maintenance" {
				binary = true
				require.Equal(t, "AP9vcmlnaW5hbA==", r.Values["value"].(map[string]any)["sqlite_blob_base64"])
			}
			if r.Table == "enrichment_jobs" {
				require.Equal(t, json.Number("9223372036854775807"), r.Values["attempts"])
				require.Equal(t, `  {"a":1, "a":2}  `, r.Values["staged_json"])
			}
			rows++
		}
	}
	require.True(t, cached && binary)
	require.EqualValues(t, 14, rows)
	require.Equal(t, m.Records, rows)
}

func TestAutomationSnapshotRejectsManifestDefects(t *testing.T) {
	body, _ := automationManifest(t)
	for _, test := range []struct {
		name  string
		alter func(map[string]any)
	}{
		{"reader", func(m map[string]any) { m["reader"] = "unknown" }},
		{"extra", func(m map[string]any) { m["extra"] = true }},
		{"application", func(m map[string]any) { m["application_id"] = 1 }},
		{"null application", func(m map[string]any) { m["application_id"] = nil }},
		{"source checksum", func(m map[string]any) { m["source_sha256"] = "bad" }},
		{"null chunks", func(m map[string]any) { m["chunks"] = nil }},
		{"record count", func(m map[string]any) { m["records"] = 1 }},
		{"unknown schema", func(m map[string]any) { m["schema"].([]any)[0].(map[string]any)["type"] = "view" }},
		{"outside chunk", func(m map[string]any) { m["chunks"].([]any)[0].(map[string]any)["file"] = "../outside" }},
		{"chunk limit", func(m map[string]any) { m["chunks"].([]any)[0].(map[string]any)["bytes"] = 17000000 }},
		{"integrity", func(m map[string]any) { m["integrity"].(map[string]any)["quick_check"] = "corrupt" }},
		{"missing integrity", func(m map[string]any) { delete(m["integrity"].(map[string]any), "foreign_key_violations") }},
		{"unknown table", func(m map[string]any) {
			m["tables"].(map[string]any)["unhandled"] = m["tables"].(map[string]any)["maintenance"]
		}},
		{"null count", func(m map[string]any) { m["tables"].(map[string]any)["maintenance"].(map[string]any)["rows"] = nil }},
		{"column default", func(m map[string]any) {
			m["tables"].(map[string]any)["maintenance"].(map[string]any)["columns"].([]any)[0].(map[string]any)["dflt_value"] = true
		}},
		{"null column", func(m map[string]any) {
			m["tables"].(map[string]any)["maintenance"].(map[string]any)["columns"].([]any)[0].(map[string]any)["hidden"] = nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var object map[string]any
			require.NoError(t, json.Unmarshal(body, &object))
			test.alter(object)
			changed, err := archive.EncodeSourceJSON(object)
			require.NoError(t, err)
			_, err = scrape.PrepareAutomationSnapshot(changed, scrape.CatalogSnapshotSHA(changed))
			require.ErrorIs(t, err, models.ErrAutomationSnapshotInvalid)
		})
	}
	_, err := scrape.PrepareAutomationSnapshot(body, scrape.CatalogSnapshotSHA(nil))
	require.ErrorIs(t, err, models.ErrAutomationSnapshotInvalid)
}

func TestAutomationSnapshotEmptyInputAndRecordBoundaries(t *testing.T) {
	body, m := automationManifest(t)
	var object map[string]any
	require.NoError(t, json.Unmarshal(body, &object))
	for _, table := range object["tables"].(map[string]any) {
		table := table.(map[string]any)
		table["rows"], table["bytes"], table["sha256"] = 0, 0, scrape.CatalogSnapshotSHA(nil)
	}
	object["records"], object["chunks"] = 0, []any{}
	object["integrity"].(map[string]any)["foreign_key_violations"] = 0
	changed, err := archive.EncodeSourceJSON(object)
	require.NoError(t, err)
	empty, err := scrape.PrepareAutomationSnapshot(changed, scrape.CatalogSnapshotSHA(changed))
	require.NoError(t, err)
	require.Empty(t, empty.Chunks)
	for _, value := range []string{`true`, `[]`, `{"sqlite_blob_base64":"AB=="}`, `{"sqlite_blob_base64":"AA==","extra":0}`, `9223372036854775808`, `1e999`} {
		line := []byte(`{"table":"maintenance","key":["cursor"],"values":{"key":"cursor","value":` + value + "}}\n")
		_, err := m.Record(line)
		require.ErrorIs(t, err, models.ErrAutomationSnapshotInvalid, value)
	}
	for _, line := range []string{
		`{"table":"maintenance","key":["cursor"],"values":{"key":"different","value":null}}` + "\n",
		`{"table":"maintenance","key":[true],"values":{"key":true,"value":null}}` + "\n",
		`{"table":"maintenance","key":["cursor"],"values":{"key":"cursor","value":null}}`,
		`{"table":"maintenance","key":["cursor"],"values":{"key":"cursor","value":null}}` + "\n\n",
	} {
		_, err := m.Record([]byte(line))
		require.ErrorIs(t, err, models.ErrAutomationSnapshotInvalid)
	}
}
