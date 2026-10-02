package scrape

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

const (
	CatalogManifestLimit = 8 << 20
	CatalogChunkLimit    = 16 << 20
	CatalogChunkRows     = 1000
)

type CatalogSnapshotColumn struct {
	CID     int             `json:"cid"`
	Name    string          `json:"name"`
	Type    string          `json:"type"`
	NotNull int             `json:"notnull"`
	Default json.RawMessage `json:"dflt_value"`
	PK      int             `json:"pk"`
	Hidden  int             `json:"hidden"`
}

type CatalogSnapshotTable struct {
	Rows    int64                   `json:"rows"`
	Bytes   int64                   `json:"bytes"`
	SHA256  string                  `json:"sha256"`
	Columns []CatalogSnapshotColumn `json:"columns"`
	Key     []string                `json:"key"`
}

type CatalogSnapshotChunk struct {
	File   string `json:"file"`
	Rows   int    `json:"rows"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type CatalogSnapshotManifest struct {
	Format      string                          `json:"format"`
	Reader      string                          `json:"reader"`
	UUID        string                          `json:"snapshot_uuid"`
	SourceUUID  string                          `json:"registry_source_uuid"`
	CatalogID   string                          `json:"catalog_id"`
	CapturedAt  string                          `json:"captured_at"`
	Application int                             `json:"application_id"`
	Version     int                             `json:"user_version"`
	CatalogInfo map[string]string               `json:"catalog_info"`
	Tables      map[string]CatalogSnapshotTable `json:"tables"`
	Records     int64                           `json:"records"`
	Chunks      []CatalogSnapshotChunk          `json:"chunks"`
	References  map[string]int64                `json:"references"`
	Stage       string                          `json:"stage"`
	Schema      []struct {
		Type  string `json:"type"`
		Name  string `json:"name"`
		Table string `json:"tbl_name"`
		SQL   string `json:"sql"`
	} `json:"schema"`
	Captures struct {
		Count       int64  `json:"count"`
		Flat        int64  `json:"flat_observations"`
		ProfileRefs int64  `json:"profile_references"`
		MaxBytes    int    `json:"max_payload_bytes"`
		SHA256      string `json:"sha256"`
		Encoding    string `json:"encoding"`
	} `json:"captures"`
}

var catalogSnapshotColumns = map[string]string{
	"catalog_info": "key value",
	"accounts":     "account_key platform source_id identity_basis created_at",
	"handles":      "account_key handle first_observed",
	"posts":        "post_key platform source_id account_key identity_basis created_at",
	"post_urls":    "post_key url", "post_aliases": "alias_key post_key",
	"observations":         "observation_id post_key origin captured_at payload_json title original_text published_at date_basis language extractor_version",
	"observation_details":  "capture_id observation_id captured_at extractor_version payload_patch",
	"account_snapshots":    "snapshot_id platform payload_json",
	"assets":               "asset_id digest_algorithm digest byte_size created_at",
	"files":                "relpath asset_id state byte_size mtime_ns first_observed survivor_relpath role",
	"appearances":          "post_key attachment_key asset_id source_media_id position source_relpath",
	"memberships":          "post_key collection_key kind label",
	"translations":         "translation_id post_key input_hash original_text translated_text source_language target_language provider provenance captured_at",
	"sidecars":             "relpath content_sha256 raw_content encoding parse_status warnings_json parsed_json post_key captured_at",
	"sidecar_documents":    "document_id content_sha256 raw_content encoding parse_status warnings_json parsed_json",
	"sidecar_sources":      "relpath content_sha256 document_id post_key captured_at",
	"sidecar_heads":        "relpath content_sha256 observed_at",
	"metadata_edits":       "edit_id relpath fields_json created_at",
	"dedupe_events":        "event_id asset_id paths_json survivor_relpath stage created_at",
	"file_events":          "event_id relpath old_state new_state reason observed_at",
	"metadata_prune_queue": "post_key pruned_at",
	"enrichment_receipts":  "post_key version completed_at details_json",
}

var catalogSnapshotKeys = map[string]string{
	"catalog_info": "key", "accounts": "account_key", "handles": "account_key handle",
	"posts": "post_key", "post_urls": "post_key url", "post_aliases": "alias_key",
	"observations": "observation_id", "observation_details": "capture_id", "account_snapshots": "snapshot_id",
	"assets": "asset_id", "files": "relpath", "appearances": "post_key attachment_key asset_id",
	"memberships": "post_key collection_key", "translations": "translation_id",
	"sidecars": "relpath content_sha256", "sidecar_documents": "document_id",
	"sidecar_sources": "relpath content_sha256", "sidecar_heads": "relpath",
	"metadata_edits": "edit_id", "dedupe_events": "event_id", "file_events": "event_id",
	"metadata_prune_queue": "post_key", "enrichment_receipts": "post_key version",
}

var catalogSnapshotRefs = map[string][]string{
	"handles": {"account_key"}, "posts": {"account_key"}, "observations": {"post_key"},
	"observation_details": {"observation_id"}, "post_urls": {"post_key"}, "post_aliases": {"post_key"},
	"files": {"asset_id"}, "appearances": {"post_key", "asset_id"}, "memberships": {"post_key"},
	"translations": {"post_key"}, "sidecars": {"post_key"}, "sidecar_sources": {"post_key", "document_id"},
	"metadata_edits": {"relpath"}, "file_events": {"relpath"}, "dedupe_events": {"asset_id"}, "enrichment_receipts": {"post_key"},
}

var catalogSnapshotID = regexp.MustCompile(`^c_[0-9a-f]{32}$`)

func CatalogSnapshotSHA(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// PrepareCatalogSnapshot validates a versioned data description. Its retained
// SQLite DDL is evidence only and must never be executed on the native database.
func PrepareCatalogSnapshot(body []byte, expected string) (*CatalogSnapshotManifest, error) {
	bad := models.ErrCatalogSnapshotInvalid
	object, err := archive.DecodeJSONObject(body, CatalogManifestLimit)
	if err != nil || len(object) != 16 || !archive.ValidSHA256(expected) || CatalogSnapshotSHA(body) != expected {
		return nil, bad
	}
	m := &CatalogSnapshotManifest{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(m) != nil || m.Format != "stash-catalog-snapshot-v1" || m.Reader != "catalog-sqlite-v1" || m.Stage != "prepared" ||
		!catalogIdentityUUID(m.UUID) || !catalogIdentityUUID(m.SourceUUID) || !catalogSnapshotID.MatchString(m.CatalogID) || !journalTime(m.CapturedAt) ||
		m.Application != 0x53435043 || m.Version < 1 || m.Version > 3 || m.Records < 1 || m.Records > 10000000 || len(m.Chunks) > 100000 {
		return nil, bad
	}
	if m.CatalogInfo["id"] != m.CatalogID || m.CatalogInfo["schema_version"] != strconv.Itoa(m.Version) || m.CatalogInfo["path_base"] != "media-root-relative" ||
		len(m.CatalogInfo) > 256 || m.Tables["catalog_info"].Rows != int64(len(m.CatalogInfo)) {
		return nil, bad
	}
	for _, name := range strings.Fields("catalog_info accounts handles posts observations post_urls assets files appearances memberships translations dedupe_events") {
		if _, ok := m.Tables[name]; !ok {
			return nil, bad
		}
	}
	_, documents := m.Tables["sidecar_documents"]
	_, sources := m.Tables["sidecar_sources"]
	_, sidecars := m.Tables["sidecars"]
	if documents != sources || sidecars == documents {
		return nil, bad
	}
	var rows, size int64
	refs := 0
	for name, table := range m.Tables {
		if !validCatalogTable(name, table, m.Version) {
			return nil, bad
		}
		rows += table.Rows
		size += table.Bytes
		for _, column := range catalogSnapshotRefs[name] {
			count, ok := m.References[name+"."+column]
			if !ok || count < 0 || count > table.Rows {
				return nil, bad
			}
			refs++
		}
	}
	if rows != m.Records || len(m.References) != refs || !m.validSchema() {
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
	c := m.Captures
	if chunkRows != rows || chunkSize != size || c.Encoding != "legacy-python-json-v1" || !archive.ValidSHA256(c.SHA256) ||
		c.Flat < 0 || c.Flat > m.Tables["observations"].Rows || c.Count != m.Tables["observation_details"].Rows+c.Flat ||
		c.ProfileRefs < 0 || c.ProfileRefs > c.Count*2048 || c.MaxBytes < 0 || c.MaxBytes > 4<<20 {
		return nil, bad
	}
	return m, nil
}

func validCatalogTable(name string, table CatalogSnapshotTable, version int) bool {
	columns := strings.Fields(catalogSnapshotColumns[name])
	if name == "observations" || name == "observation_details" {
		if version == 3 || len(table.Columns) == len(columns)+1 {
			columns = append(columns, "account_refs_json")
		}
	}
	return validSnapshotTable(table, columns, strings.Fields(catalogSnapshotKeys[name]))
}

func validSnapshotTable(table CatalogSnapshotTable, columns, key []string) bool {
	if len(columns) == 0 || !slices.Equal(table.Key, key) || table.Rows < 0 || table.Rows > 10000000 ||
		table.Bytes < table.Rows || table.Bytes > table.Rows*CatalogChunkLimit || !archive.ValidSHA256(table.SHA256) {
		return false
	}
	if table.Rows == 0 && (table.Bytes != 0 || table.SHA256 != CatalogSnapshotSHA(nil)) {
		return false
	}
	if len(columns) != len(table.Columns) {
		return false
	}
	seen := map[string]bool{}
	for i, column := range table.Columns {
		if column.CID != i || column.Hidden != 0 || column.NotNull < 0 || column.NotNull > 1 || seen[column.Name] || !slices.Contains(columns, column.Name) ||
			column.PK != slices.Index(table.Key, column.Name)+1 || len(column.Default) == 0 {
			return false
		}
		seen[column.Name] = true
	}
	return true
}

func (m *CatalogSnapshotManifest) validSchema() bool {
	tables, names := map[string]bool{}, map[string]bool{}
	sidecarViewPresent := false
	for _, item := range m.Schema {
		if item.Type == "view" && item.Name == "sidecars" {
			sidecarViewPresent = true
		}
	}
	for _, item := range m.Schema {
		if names[item.Name] || len(item.SQL) == 0 || len(item.SQL) > 1<<20 {
			return false
		}
		names[item.Name] = true
		switch item.Type {
		case "table":
			if _, ok := m.Tables[item.Name]; !ok || item.Table != item.Name {
				return false
			}
			tables[item.Name] = true
		case "index", "trigger":
			if _, ok := m.Tables[item.Table]; !ok && (item.Type != "trigger" || item.Table != "sidecars" || !sidecarViewPresent) {
				return false
			}
		case "view":
			const sidecarView = "createviewsidecarsasselects.relpath,s.content_sha256,d.raw_content,d.encoding,d.parse_status,d.warnings_json,d.parsed_json,s.post_key,s.captured_atfromsidecar_sourcessjoinsidecar_documentsdusing(document_id)"
			if item.Name != "sidecars" || item.Table != "sidecars" || strings.TrimSuffix(strings.ToLower(strings.Join(strings.Fields(item.SQL), "")), ";") != sidecarView {
				return false
			}
		default:
			return false
		}
	}
	return len(tables) == len(m.Tables)
}

type CatalogSnapshotRecord struct {
	Table   string
	Key     []any
	KeyJSON string
	Values  map[string]any
	Line    []byte
}

// Each original line survives staging verbatim. Native source payload encoding
// is a separate operation; re-encoding here would lose original digest evidence.
func (m *CatalogSnapshotManifest) Record(line []byte) (*CatalogSnapshotRecord, error) {
	r, err := prepareSQLiteSnapshotRecord(line, m.Tables, validCatalogValue)
	if err != nil {
		return nil, err
	}
	if r.Table == "catalog_info" {
		name, ok := r.Values["key"].(string)
		expected, exists := m.CatalogInfo[name]
		if !ok || !exists || r.Values["value"] != expected {
			return nil, models.ErrCatalogSnapshotInvalid
		}
	}
	return r, nil
}

func prepareSQLiteSnapshotRecord(line []byte, tables map[string]CatalogSnapshotTable, validValue func(string, string, any, map[string]any) bool) (*CatalogSnapshotRecord, error) {
	bad := models.ErrCatalogSnapshotInvalid
	if len(line) == 0 || line[len(line)-1] != '\n' || bytes.Count(line, []byte{'\n'}) != 1 {
		return nil, bad
	}
	object, err := archive.DecodeJSONObject(line, CatalogChunkLimit)
	if err != nil || len(object) != 3 {
		return nil, bad
	}
	table, _ := object["table"].(string)
	descriptor, exists := tables[table]
	key, keyOK := object["key"].([]any)
	values, valuesOK := object["values"].(map[string]any)
	if !exists || !keyOK || !valuesOK || len(key) != len(descriptor.Key) || len(values) != len(descriptor.Columns) {
		return nil, bad
	}
	for index, part := range key {
		switch value := part.(type) {
		case string:
		case json.Number:
			if _, err := value.Int64(); err != nil {
				return nil, bad
			}
		default:
			return nil, bad
		}
		if !reflect.DeepEqual(part, values[descriptor.Key[index]]) {
			return nil, bad
		}
	}
	for _, column := range descriptor.Columns {
		value, ok := values[column.Name]
		if !ok || !validValue(table, column.Name, value, values) {
			return nil, bad
		}
	}
	encoded, err := archive.EncodeSourceJSON(key)
	if err != nil || len(encoded) > 32768 {
		return nil, bad
	}
	return &CatalogSnapshotRecord{Table: table, Key: key, KeyJSON: string(encoded), Values: values, Line: line}, nil
}

func validCatalogValue(table, column string, value any, values map[string]any) bool {
	if strings.HasSuffix(column, "_json") || column == "payload_patch" {
		raw, ok := value.(string)
		if !ok || len(raw) > 4<<20 {
			return false
		}
		_, err := archive.DecodeJSONObject([]byte(`{"value":`+raw+`}`), (4<<20)+10)
		return err == nil
	}
	if column == "raw_content" && (table == "sidecars" || table == "sidecar_documents") {
		blob, ok := value.(map[string]any)
		encoded, stringOK := blob["sqlite_blob_base64"].(string)
		if !ok || !stringOK || len(blob) != 1 {
			return false
		}
		body, err := base64.StdEncoding.Strict().DecodeString(encoded)
		return err == nil && base64.StdEncoding.EncodeToString(body) == encoded && CatalogSnapshotSHA(body) == values["content_sha256"]
	}
	switch value := value.(type) {
	case nil, string:
		return true
	case json.Number:
		number, err := value.Float64()
		return err == nil && !math.IsInf(number, 0)
	default:
		return false
	}
}

// SQLite BINARY ordering for the supported TEXT/INTEGER primary keys. Comparing
// their serialized JSON would put integer 10 before 2 and mishandle escapes.
func CatalogRecordAfter(table string, key []any, previousTable string, previousKey []any) bool {
	if table != previousTable {
		return table > previousTable
	}
	for i, part := range key {
		if i >= len(previousKey) {
			return false
		}
		if reflect.DeepEqual(part, previousKey[i]) {
			continue
		}
		left, leftText := part.(string)
		right, rightText := previousKey[i].(string)
		if leftText != rightText {
			return leftText
		}
		if leftText {
			return left > right
		}
		l, lOK := part.(json.Number)
		r, rOK := previousKey[i].(json.Number)
		if !lOK || !rOK {
			return false
		}
		li, le := l.Int64()
		ri, re := r.Int64()
		return le == nil && re == nil && li > ri
	}
	return false
}
