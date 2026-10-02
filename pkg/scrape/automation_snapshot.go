package scrape

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type AutomationSnapshotManifest struct {
	Format       string                          `json:"format"`
	Reader       string                          `json:"reader"`
	UUID         string                          `json:"snapshot_uuid"`
	SourceUUID   string                          `json:"registry_source_uuid"`
	SourceSHA256 string                          `json:"source_sha256"`
	CapturedAt   string                          `json:"captured_at"`
	Application  int                             `json:"application_id"`
	Version      int                             `json:"user_version"`
	Tables       map[string]CatalogSnapshotTable `json:"tables"`
	Records      int64                           `json:"records"`
	Chunks       []CatalogSnapshotChunk          `json:"chunks"`
	Stage        string                          `json:"stage"`
	Schema       []struct {
		Type  string `json:"type"`
		Name  string `json:"name"`
		Table string `json:"tbl_name"`
		SQL   string `json:"sql"`
	} `json:"schema"`
	Integrity struct {
		QuickCheck           string `json:"quick_check"`
		ForeignKeyViolations int64  `json:"foreign_key_violations"`
	} `json:"integrity"`
}

var automationSnapshotColumns = map[string]string{
	"maintenance":                "key value",
	"translation_jobs":           "job_key original_text target_language priority source_hint status result_json attempts next_attempt last_error created_at updated_at",
	"translation_targets":        "job_key catalog_id post_key field applied",
	"enrichment_jobs":            "catalog_id post_key version platform account_key url status priority attempts next_attempt last_error staged_json created_at updated_at",
	"enrichment_cooldowns":       "scope until_time reason",
	"enrichment_seed_progress":   "catalog_id last_post_key complete counts_json",
	"enrichment_source_progress": "platform last_attempt",
	"discovery_accounts":         "job_key platform account_key profile_url status cursor_json staged_json pages attempts next_attempt last_error created_at updated_at",
	"discovery_targets":          "catalog_id post_key job_key evidence_json status",
	"discovery_candidates":       "catalog_id post_key url basis payload_json",
}

var automationSnapshotKeys = map[string]string{
	"maintenance": "key", "translation_jobs": "job_key", "translation_targets": "job_key catalog_id post_key field",
	"enrichment_jobs": "catalog_id post_key version", "enrichment_cooldowns": "scope", "enrichment_seed_progress": "catalog_id",
	"enrichment_source_progress": "platform", "discovery_accounts": "job_key", "discovery_targets": "catalog_id post_key",
	"discovery_candidates": "catalog_id post_key url",
}

// The complete recognized schema is inventoried. Its DDL is retained evidence,
// never SQL to execute. Semantic defects remain input for the domain importers.
func PrepareAutomationSnapshot(body []byte, expected string) (*AutomationSnapshotManifest, error) {
	bad := models.ErrAutomationSnapshotInvalid
	object, err := archive.DecodeJSONObject(body, CatalogManifestLimit)
	if err != nil || len(object) != 14 || !validAutomationInventory(object) || !archive.ValidSHA256(expected) || CatalogSnapshotSHA(body) != expected {
		return nil, bad
	}
	m := &AutomationSnapshotManifest{}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(m); err != nil || m.Format != "stash-automation-snapshot-v1" || m.Reader != "automation-sqlite-v1" ||
		m.Stage != "prepared" || !catalogIdentityUUID(m.UUID) || !catalogIdentityUUID(m.SourceUUID) || !archive.ValidSHA256(m.SourceSHA256) ||
		!journalTime(m.CapturedAt) || m.Application != 0x53435043 || m.Version < 1 || m.Version > 3 || m.Records < 0 || m.Records > 10000000 || len(m.Chunks) > 100000 ||
		m.Integrity.QuickCheck != "ok" || m.Integrity.ForeignKeyViolations < 0 || m.Integrity.ForeignKeyViolations > m.Records || len(m.Tables) != len(automationSnapshotColumns) {
		return nil, bad
	}
	var rows, size int64
	for name, descriptor := range m.Tables {
		if !validSnapshotTable(descriptor, strings.Fields(automationSnapshotColumns[name]), strings.Fields(automationSnapshotKeys[name])) {
			return nil, bad
		}
		rows += descriptor.Rows
		size += descriptor.Bytes
	}
	if rows != m.Records || !m.validSchema() {
		return nil, bad
	}
	var chunkRows, chunkSize int64
	for index, c := range m.Chunks {
		if c.File != fmt.Sprintf("records-%06d.jsonl", index) || c.Rows < 1 || c.Rows > CatalogChunkRows || c.Bytes < c.Rows || c.Bytes > CatalogChunkLimit || !archive.ValidSHA256(c.SHA256) {
			return nil, bad
		}
		chunkRows += int64(c.Rows)
		chunkSize += int64(c.Bytes)
	}
	if chunkRows != rows || chunkSize != size {
		return nil, bad
	}
	return m, nil
}

// A required zero-valued field must still be present with its declared type.
// encoding/json alone would accept null or a missing integer as zero.
func validAutomationInventory(object map[string]any) bool {
	for _, name := range []string{"application_id", "user_version", "records"} {
		if _, ok := object[name].(json.Number); !ok {
			return false
		}
	}
	for _, name := range []string{"chunks", "schema"} {
		if _, ok := object[name].([]any); !ok {
			return false
		}
	}
	integrity, ok := object["integrity"].(map[string]any)
	if !ok || len(integrity) != 2 {
		return false
	}
	if _, ok := integrity["foreign_key_violations"].(json.Number); !ok {
		return false
	}
	tables, ok := object["tables"].(map[string]any)
	if !ok {
		return false
	}
	for _, raw := range tables {
		table, ok := raw.(map[string]any)
		if !ok || len(table) != 5 {
			return false
		}
		for _, name := range []string{"rows", "bytes"} {
			if _, ok := table[name].(json.Number); !ok {
				return false
			}
		}
		columns, ok := table["columns"].([]any)
		if !ok {
			return false
		}
		for _, raw := range columns {
			column, ok := raw.(map[string]any)
			if !ok || len(column) != 7 {
				return false
			}
			for _, name := range []string{"cid", "notnull", "pk", "hidden"} {
				if _, ok := column[name].(json.Number); !ok {
					return false
				}
			}
			if _, ok := column["type"].(string); !ok {
				return false
			}
			if value, exists := column["dflt_value"]; !exists {
				return false
			} else if value != nil {
				if _, ok := value.(string); !ok {
					return false
				}
			}
		}
	}
	return true
}

func (m *AutomationSnapshotManifest) validSchema() bool {
	tables, names := map[string]bool{}, map[string]bool{}
	for _, item := range m.Schema {
		if names[item.Name] || item.Name == "" || len(item.SQL) == 0 || len(item.SQL) > 1<<20 {
			return false
		}
		names[item.Name] = true
		if _, ok := m.Tables[item.Table]; !ok {
			return false
		}
		switch item.Type {
		case "table":
			if item.Name != item.Table {
				return false
			}
			tables[item.Name] = true
		case "index", "trigger":
		default:
			return false
		}
	}
	return len(tables) == len(m.Tables)
}

func (m *AutomationSnapshotManifest) Record(line []byte) (*CatalogSnapshotRecord, error) {
	r, err := prepareSQLiteSnapshotRecord(line, m.Tables, validAutomationValue)
	if err != nil {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	return r, nil
}

func validAutomationValue(_, _ string, value any, _ map[string]any) bool {
	switch v := value.(type) {
	case nil, string:
		// JSON stored in a legacy TEXT column remains exact source text, even
		// if malformed. A domain importer must retain or review that defect.
		return true
	case json.Number:
		if !strings.ContainsAny(string(v), ".eE") {
			_, err := v.Int64()
			return err == nil
		}
		number, err := v.Float64()
		return err == nil && !math.IsInf(number, 0)
	case map[string]any:
		encoded, ok := v["sqlite_blob_base64"].(string)
		if !ok || len(v) != 1 {
			return false
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
		return err == nil && base64.StdEncoding.EncodeToString(decoded) == encoded
	default:
		return false
	}
}
