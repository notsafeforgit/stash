package scrape

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

var scanJournalColumns = map[string][]string{
	"scan_jobs":                      {"id", "run_key", "context", "command_json", "url", "created", "attempts", "retry_after", "last_status"},
	"extractor_jobs":                 {"id", "url", "context", "pid", "process_start", "last_key", "last_index", "scope"},
	"scan_deferrals":                 {"context", "url", "reason", "last_status", "deferred_at"},
	"backfill_scan_completion":       {"backfill_key", "scan_key", "completed"},
	"collection_backfill_completion": {"platform", "collection_kind", "collection_name", "recorded_at", "result_json"},
	"backfill_policy_migrations":     {"name", "applied_at", "details_json"},
	"legacy_handoffs":                {"run_key", "unit", "boot_id", "pid", "jobs_json"},
}

func ValidScanJournalTable(table string) bool {
	return scanJournalColumns[table] != nil
}

type PreparedScanJournal struct {
	Receipt models.ScanJournal
	Records []models.ScanJournalRecord
}

func PrepareScanJournal(input models.ScanJournalInput) (*PreparedScanJournal, error) {
	for _, id := range []string{input.UUID, input.RootUUID, input.SourceUUID} {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed == uuid.Nil || parsed.String() != id {
			return nil, models.ErrScanJournalInvalid
		}
	}
	object, err := archive.DecodeJSONObject(input.Document, 8<<20)
	if err != nil || len(object) != 3 {
		return nil, models.ErrScanJournalInvalid
	}
	var document struct {
		CapturedAt string                       `json:"captured_at"`
		Tables     map[string][]json.RawMessage `json:"tables"`
		External   map[string]int               `json:"external_tables"`
	}
	if json.Unmarshal(input.Document, &document) != nil || !journalTime(document.CapturedAt) || len(document.Tables) == 0 || document.External == nil {
		return nil, models.ErrScanJournalInvalid
	}
	for table, count := range document.External {
		if (table != "backfill_completion" && table != "legacy_backfill_skip") || count < 0 {
			return nil, models.ErrScanJournalInvalid
		}
	}
	sum := sha256.Sum256(input.Document)
	ret := &PreparedScanJournal{Receipt: models.ScanJournal{UUID: input.UUID, RootUUID: input.RootUUID, SourceUUID: input.SourceUUID,
		InputSHA256: hex.EncodeToString(sum[:]), CapturedAt: document.CapturedAt}, Records: []models.ScanJournalRecord{}}
	names := make([]string, 0, len(document.Tables))
	for name := range document.Tables {
		names = append(names, name)
	}
	sort.Strings(names)
	counts := map[string]int{}
	seen := map[string]bool{}
	for _, table := range names {
		if !ValidScanJournalTable(table) || document.Tables[table] == nil {
			return nil, models.ErrScanJournalInvalid
		}
		counts[table] = len(document.Tables[table])
		for _, body := range document.Tables[table] {
			row, err := prepareJournalRecord(input.UUID, table, body)
			if err != nil || seen[row.UUID] || len(ret.Records) >= 10000 {
				return nil, models.ErrScanJournalInvalid
			}
			seen[row.UUID] = true
			ret.Records = append(ret.Records, row)
		}
	}
	// Scope links are exact old IDs within this same snapshot. Missing scopes
	// and manual work remain visible; never attach them by URL/name alone.
	jobs := map[string]string{}
	deferrals := map[string]string{}
	for _, row := range ret.Records {
		var data map[string]any
		_ = json.Unmarshal(row.Evidence, &data)
		if row.Table == "scan_jobs" {
			jobs[data["id"].(string)] = row.UUID
		}
		if row.Table == "scan_deferrals" {
			deferrals[row.Context+"\x00"+row.TargetURL] = row.UUID
		}
	}
	for i := range ret.Records {
		row := &ret.Records[i]
		summary, _ := archive.DecodeJSONObject(row.Summary, 16384)
		if row.Table == "scan_jobs" {
			if id := deferrals[row.Context+"\x00"+row.TargetURL]; id != "" {
				summary["deferral_uuid"], summary["requires_explicit_retry"] = id, true
			}
		}
		if row.Table == "extractor_jobs" {
			scope, _ := summary["scope"].(string)
			if id := jobs[scope]; id != "" {
				summary["scan_record_uuid"] = id
			} else {
				row.Disposition = "review"
				summary["review_reason"] = "unbound_extractor_scope"
			}
		}
		row.Summary, err = archive.EncodeSourceJSON(summary)
		if err != nil {
			return nil, err
		}
	}
	ret.Receipt.RecordCount = len(ret.Records)
	ret.Receipt.Inventory, err = archive.EncodeSourceJSON(map[string]any{"retained_tables": counts, "external_tables": document.External})
	return ret, err
}

func journalTime(value string) bool {
	t, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && t.Year() >= 1 && t.Year() <= 9999
}

func journalURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && len(value) <= 8192 && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil && u.Fragment == ""
}

func journalNumber(value any, minimum, maximum float64, integer bool) bool {
	number, ok := value.(json.Number)
	if !ok {
		return false
	}
	n, err := number.Float64()
	return err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && n >= minimum && n <= maximum && (!integer || math.Trunc(n) == n)
}

// Command evidence is accepted only for known, non-secret argument forms. It
// is never executable server input. Unknown options must be reviewed in the
// original private snapshot rather than copied as possible website logins.
func journalCommand(value string) (string, bool) {
	var args []string
	if len(value) > 32768 || json.Unmarshal([]byte(value), &args) != nil || len(args) == 0 || path.Base(args[0]) != "gallery-dl" {
		return "", false
	}
	lower := ""
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--config-ignore", "--no-skip":
		case "--config", "-c":
			i++
			if i == len(args) || !strings.HasPrefix(args[i], "/") || len(args[i]) > 4096 {
				return "", false
			}
		case "-i", "--input-file":
			i++
			if i == len(args) || args[i] != "-" {
				return "", false
			}
		case "-o", "--option":
			i++
			if i == len(args) {
				return "", false
			}
			if args[i] == "skip=true" || args[i] == "skip=false" {
				continue
			}
			if !strings.HasPrefix(args[i], "extractor.reddit.date-min=") || lower != "" {
				return "", false
			}
			lower = strings.TrimPrefix(args[i], "extractor.reddit.date-min=")
			if _, err := time.Parse("2006-01-02T15:04:05", lower); err != nil && !journalTime(lower) {
				return "", false
			}
		default:
			return "", false
		}
	}
	return lower, true
}

func prepareJournalRecord(journal, table string, body json.RawMessage) (models.ScanJournalRecord, error) {
	ret := models.ScanJournalRecord{JournalUUID: journal, Table: table, Evidence: body, Disposition: "pending_binding"}
	bad := func() (models.ScanJournalRecord, error) { return ret, models.ErrScanJournalInvalid }
	data, err := archive.DecodeJSONObject(body, 1<<20)
	if err != nil {
		return bad()
	}
	columns := scanJournalColumns[table]
	if table == "extractor_jobs" {
		// The old extractor table acquired these columns incrementally.
		columns = columns[:5]
	}
	for _, key := range columns {
		if _, ok := data[key]; !ok {
			return bad()
		}
	}
	allowed := map[string]bool{}
	for _, key := range scanJournalColumns[table] {
		allowed[key] = true
	}
	for key := range data {
		if !allowed[key] {
			return bad()
		}
	}
	text := func(key string) string { value, _ := data[key].(string); return value }
	nullText := func(key string) bool { _, ok := data[key].(string); return data[key] == nil || ok }
	var keys []string
	summary := map[string]any{"activation": "not_activated"}
	if table == "scan_jobs" || table == "scan_deferrals" || table == "extractor_jobs" {
		ret.Context, ret.TargetURL = text("context"), text("url")
		if (ret.Context != "host" && ret.Context != "n8n") || !journalURL(ret.TargetURL) {
			return bad()
		}
	}
	switch table {
	case "scan_jobs":
		if text("id") == "" || text("run_key") == "" || !journalNumber(data["created"], 1, 253402300799, false) ||
			!journalNumber(data["attempts"], 0, 2147483647, true) || !journalNumber(data["retry_after"], 0, 253402300799, false) ||
			(data["last_status"] != nil && !journalNumber(data["last_status"], -255, 255, true)) {
			return bad()
		}
		lower, valid := journalCommand(text("command_json"))
		if !valid {
			return bad()
		}
		keys = []string{text("id")}
		for _, key := range []string{"run_key", "created", "attempts", "retry_after", "last_status"} {
			summary[key] = data[key]
		}
		hash := sha256.Sum256([]byte(text("command_json")))
		summary["command_sha256"], summary["legacy_date_min"] = hex.EncodeToString(hash[:]), lower
		summary["window_until"] = "requires_cutover_policy"
	case "extractor_jobs":
		if text("id") == "" || !journalNumber(data["pid"], -1, 2147483647, true) || !nullText("process_start") || !nullText("last_key") || !nullText("scope") ||
			(data["last_index"] != nil && !journalNumber(data["last_index"], 0, 9007199254740991, true)) {
			return bad()
		}
		if (text("last_key") != "" && !archive.ValidSHA256(text("last_key"))) || len(text("scope")) > 128 || len(text("process_start")) > 512 {
			return bad()
		}
		keys = []string{text("id")}
		for _, key := range []string{"last_key", "last_index", "scope"} {
			summary[key] = data[key]
		}
		summary["cursor_format"], summary["runtime_ownership"] = "legacy_gallery_dl_archive_key", "not_transferred"
	case "scan_deferrals":
		if text("reason") == "" || len(text("reason")) > 128 || !journalNumber(data["last_status"], 1, 255, true) || !journalNumber(data["deferred_at"], 1, 253402300799, false) {
			return bad()
		}
		keys = []string{ret.Context, ret.TargetURL}
		for _, key := range []string{"reason", "last_status", "deferred_at"} {
			summary[key] = data[key]
		}
		summary["requires_explicit_retry"] = true
	case "backfill_scan_completion":
		if !archive.ValidSHA256(text("backfill_key")) || !archive.ValidSHA256(text("scan_key")) || !journalNumber(data["completed"], 1, 253402300799, false) {
			return bad()
		}
		keys = []string{text("backfill_key"), text("scan_key")}
		ret.Disposition, summary["scope"] = "historical", "unresolved_hashed_scope"
		summary["completed"] = data["completed"]
	case "collection_backfill_completion":
		if text("platform") == "" || text("collection_kind") == "" || text("collection_name") == "" || !journalTime(text("recorded_at")) {
			return bad()
		}
		if _, err := archive.DecodeJSONObject([]byte(text("result_json")), 1<<20); err != nil {
			return bad()
		}
		keys = []string{text("platform"), text("collection_kind"), text("collection_name")}
		ret.Disposition = "historical"
		for _, key := range []string{"platform", "collection_kind", "collection_name", "recorded_at"} {
			summary[key] = data[key]
		}
		summary["basis"] = "legacy_collection_confirmation"
	case "backfill_policy_migrations":
		if text("name") == "" || !journalTime(text("applied_at")) {
			return bad()
		}
		if _, err := archive.DecodeJSONObject([]byte(text("details_json")), 1<<20); err != nil {
			return bad()
		}
		keys = []string{text("name")}
		ret.Disposition, summary["applied_at"] = "historical", data["applied_at"]
	case "legacy_handoffs":
		if text("run_key") == "" || !nullText("unit") || !nullText("boot_id") || !nullText("jobs_json") ||
			(data["pid"] != nil && !journalNumber(data["pid"], -1, 2147483647, true)) {
			return bad()
		}
		keys = []string{text("run_key")}
		ret.Disposition = "review"
		summary["runtime_ownership"], summary["review_reason"] = "not_transferred", "historical_runtime_unresolved"
	default:
		return bad()
	}
	for _, key := range keys {
		if len(key) > 8192 {
			return bad()
		}
	}
	encoded, err := archive.EncodeSourceJSON(keys)
	if err != nil {
		return ret, err
	}
	if len(encoded) > 8192 {
		return bad()
	}
	ret.SourceKey = string(encoded)
	ret.UUID = uuid.NewSHA1(uuid.MustParse(journal), []byte(table+"/"+ret.SourceKey)).String()
	ret.Summary, err = archive.EncodeSourceJSON(summary)
	return ret, err
}
