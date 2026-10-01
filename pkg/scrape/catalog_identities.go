package scrape

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

var catalogIdentityColumns = map[string][]string{
	"performer_identities":           {"id", "profile_json", "redirect_to", "created_at", "updated_at"},
	"performer_identity_bindings":    {"namespace", "performer_id", "identity_id", "profile_json", "redirect_to", "updated_at"},
	"performer_account_associations": {"account_key", "identity_id", "source", "reason", "updated_at"},
	"performer_identity_events":      {"id", "kind", "payload_json", "created_at"},
	"performer_identity_migrations":  {"name", "summary_json", "completed_at"},
	"catalog_metadata_performers":    {"namespace", "performer_id", "profile_json", "redirect_to"},
	"catalog_metadata_accounts":      {"namespace", "account_key", "performer_id"},
}

type CatalogIdentity struct {
	UUID, RedirectTo, CreatedAt, UpdatedAt string
	Profile                                map[string]any
}

type CatalogIdentityBinding struct {
	Namespace, PerformerID, IdentityUUID, RedirectTo, UpdatedAt string
}

type CatalogAccountAssociation struct {
	AccountKey, IdentityUUID, Source, Reason, UpdatedAt string
}

type PreparedCatalogIdentities struct {
	Plan           models.CatalogIdentityImportPlan
	Identities     map[string]CatalogIdentity
	Bindings       []CatalogIdentityBinding
	Accounts       []CatalogAccountAssociation
	Records        []models.CatalogIdentityImportRecord
	LegacyMigrated bool
}

func catalogIdentityUUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func catalogIdentityText(value string, limit int, empty bool) bool {
	return (empty || strings.TrimSpace(value) != "") && len(value) <= limit && !strings.ContainsFunc(value, unicode.IsControl)
}

// CatalogLocalPerformerID only accepts the exact old integer binding, never a
// display name or a service identifier. Foreign namespaces need not use integers.
func CatalogLocalPerformerID(value string) (int, bool) {
	id, err := strconv.Atoi(value)
	return id, err == nil && id > 0 && strconv.Itoa(id) == value
}

func (p *PreparedCatalogIdentities) Resolve(id string) (string, error) {
	seen := map[string]bool{}
	for range 128 {
		row, ok := p.Identities[id]
		if !ok || seen[id] {
			return "", models.ErrCatalogIdentityImportInvalid
		}
		seen[id] = true
		if row.RedirectTo == "" {
			return id, nil
		}
		id = row.RedirectTo
	}
	return "", models.ErrCatalogIdentityImportInvalid
}

// PrepareCatalogIdentities retains every original row, including embedded JSON
// text. Older plugin rows are evidence, not a second source of current choices.
func PrepareCatalogIdentities(input models.CatalogIdentityImportInput) (*PreparedCatalogIdentities, error) {
	bad := func() (*PreparedCatalogIdentities, error) { return nil, models.ErrCatalogIdentityImportInvalid }
	if !catalogIdentityUUID(input.UUID) || !catalogIdentityUUID(input.SourceUUID) || !catalogIdentityText(input.Namespace, 128, false) || input.AccountBindings == nil {
		return bad()
	}
	object, err := archive.DecodeJSONObject(input.Document, 8<<20)
	if err != nil || len(object) != 3 {
		return bad()
	}
	var document struct {
		CapturedAt string                       `json:"captured_at"`
		Tables     map[string][]json.RawMessage `json:"tables"`
		External   map[string]int               `json:"external_tables"`
	}
	if json.Unmarshal(input.Document, &document) != nil || !journalTime(document.CapturedAt) || len(document.Tables) == 0 || document.External == nil {
		return bad()
	}
	modern := []string{"performer_identities", "performer_identity_bindings", "performer_account_associations", "performer_identity_events", "performer_identity_migrations"}
	modernCount := 0
	for _, name := range modern {
		if _, ok := document.Tables[name]; ok {
			modernCount++
		}
	}
	if modernCount != 0 && modernCount != len(modern) {
		return bad()
	}
	for table, count := range document.External {
		switch table {
		case "catalogs", "routes", "links", "account_identifiers", "account_identifier_checkpoints", "account_profile_urls":
			if count < 0 {
				return bad()
			}
		default:
			return bad()
		}
	}
	// The digest binds the exact frozen document bytes as well as the namespace
	// and explicit account map. Reformatting the snapshot requires a new preview.
	documentSum := sha256.Sum256(input.Document)
	hashInput, err := archive.EncodeSourceJSON(map[string]any{"uuid": input.UUID, "source_uuid": input.SourceUUID,
		"namespace": input.Namespace, "account_bindings": input.AccountBindings, "document_sha256": hex.EncodeToString(documentSum[:])})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(hashInput)
	p := &PreparedCatalogIdentities{Plan: models.CatalogIdentityImportPlan{UUID: input.UUID, SourceUUID: input.SourceUUID,
		Namespace: input.Namespace, CapturedAt: document.CapturedAt, InputSHA256: hex.EncodeToString(sum[:]),
		Identities: []models.CatalogIdentityAction{}, Ownership: []models.CatalogOwnershipAction{}},
		Identities: map[string]CatalogIdentity{}, Bindings: []CatalogIdentityBinding{}, Accounts: []CatalogAccountAssociation{},
		Records: []models.CatalogIdentityImportRecord{}}
	names := make([]string, 0, len(document.Tables))
	for table := range document.Tables {
		names = append(names, table)
	}
	sort.Strings(names)
	counts := map[string]int{}
	seen := map[string]bool{}
	for _, table := range names {
		columns := catalogIdentityColumns[table]
		if columns == nil || document.Tables[table] == nil {
			return bad()
		}
		counts[table] = len(document.Tables[table])
		for _, body := range document.Tables[table] {
			row, err := archive.DecodeJSONObject(body, 1<<20)
			if err != nil || len(row) != len(columns) || len(p.Records) >= 10000 {
				return bad()
			}
			for _, column := range columns {
				if _, ok := row[column]; !ok {
					return bad()
				}
			}
			text := func(key string) string { value, _ := row[key].(string); return value }
			nullText := func(key string) bool { value, ok := row[key].(string); return row[key] == nil || (ok && value != "") }
			var keys []string
			switch table {
			case "performer_identities":
				if !catalogIdentityUUID(text("id")) || !nullText("redirect_to") || (row["redirect_to"] != nil && !catalogIdentityUUID(text("redirect_to"))) || !journalTime(text("created_at")) || !journalTime(text("updated_at")) {
					return bad()
				}
				created, _ := time.Parse(time.RFC3339Nano, text("created_at"))
				updated, _ := time.Parse(time.RFC3339Nano, text("updated_at"))
				if updated.Before(created) {
					return bad()
				}
				profile, err := archive.DecodeJSONObject([]byte(text("profile_json")), 1<<20)
				if err != nil {
					return bad()
				}
				p.Identities[text("id")] = CatalogIdentity{text("id"), text("redirect_to"), text("created_at"), text("updated_at"), profile}
				keys = []string{text("id")}
			case "performer_identity_bindings":
				if !catalogIdentityText(text("namespace"), 128, false) || !catalogIdentityText(text("performer_id"), 128, false) || !catalogIdentityUUID(text("identity_id")) || !nullText("redirect_to") || !journalTime(text("updated_at")) {
					return bad()
				}
				if _, err := archive.DecodeJSONObject([]byte(text("profile_json")), 1<<20); err != nil {
					return bad()
				}
				p.Bindings = append(p.Bindings, CatalogIdentityBinding{text("namespace"), text("performer_id"), text("identity_id"), text("redirect_to"), text("updated_at")})
				keys = []string{text("namespace"), text("performer_id")}
			case "performer_account_associations":
				if !catalogIdentityText(text("account_key"), 2048, false) || !nullText("identity_id") || (row["identity_id"] != nil && !catalogIdentityUUID(text("identity_id"))) || !catalogIdentityText(text("source"), 128, false) || !journalTime(text("updated_at")) {
					return bad()
				}
				if _, ok := row["reason"].(string); !ok || len(text("reason")) > 4096 {
					return bad()
				}
				p.Accounts = append(p.Accounts, CatalogAccountAssociation{text("account_key"), text("identity_id"), text("source"), text("reason"), text("updated_at")})
				keys = []string{text("account_key")}
			case "performer_identity_events":
				if !catalogIdentityUUID(text("id")) || !catalogIdentityText(text("kind"), 128, false) || !journalTime(text("created_at")) {
					return bad()
				}
				if _, err := archive.DecodeJSONObject([]byte(text("payload_json")), 1<<20); err != nil {
					return bad()
				}
				keys = []string{text("id")}
			case "performer_identity_migrations":
				if !catalogIdentityText(text("name"), 128, false) || !journalTime(text("completed_at")) {
					return bad()
				}
				if _, err := archive.DecodeJSONObject([]byte(text("summary_json")), 1<<20); err != nil {
					return bad()
				}
				p.LegacyMigrated = p.LegacyMigrated || text("name") == "catalog-metadata-links-v1"
				keys = []string{text("name")}
			case "catalog_metadata_performers", "catalog_metadata_accounts":
				if !catalogIdentityText(text("namespace"), 128, false) || !catalogIdentityText(text("performer_id"), 128, false) {
					return bad()
				}
				keys = []string{text("namespace"), text("performer_id")}
				if table == "catalog_metadata_accounts" {
					if !catalogIdentityText(text("account_key"), 2048, false) {
						return bad()
					}
					keys[1] = text("account_key")
				} else {
					if !nullText("redirect_to") {
						return bad()
					}
					if _, err := archive.DecodeJSONObject([]byte(text("profile_json")), 1<<20); err != nil {
						return bad()
					}
				}
			}
			key, _ := archive.EncodeSourceJSON(keys)
			compound := table + "\x00" + string(key)
			if seen[compound] {
				return bad()
			}
			seen[compound] = true
			p.Records = append(p.Records, models.CatalogIdentityImportRecord{Table: table, SourceKey: string(key), Evidence: body, Outcome: "copied"})
		}
	}
	for id := range p.Identities {
		if _, err := p.Resolve(id); err != nil {
			return bad()
		}
	}
	bindings := map[string]CatalogIdentityBinding{}
	for _, b := range p.Bindings {
		if _, err := p.Resolve(b.IdentityUUID); err != nil {
			return bad()
		}
		bindings[b.Namespace+"\x00"+b.PerformerID] = b
	}
	for _, b := range p.Bindings {
		at := b
		visited := map[string]bool{}
		for at.RedirectTo != "" {
			key := at.Namespace + "\x00" + at.PerformerID
			if visited[key] || len(visited) >= 128 {
				return bad()
			}
			visited[key] = true
			var ok bool
			at, ok = bindings[at.Namespace+"\x00"+at.RedirectTo]
			if !ok {
				return bad()
			}
		}
		from, _ := p.Resolve(b.IdentityUUID)
		to, _ := p.Resolve(at.IdentityUUID)
		if from != to {
			return bad()
		}
	}
	accounts := map[string]bool{}
	for _, a := range p.Accounts {
		accounts[a.AccountKey] = true
		if a.IdentityUUID != "" {
			if _, err := p.Resolve(a.IdentityUUID); err != nil {
				return bad()
			}
		}
	}
	for key, id := range input.AccountBindings {
		if !accounts[key] || !catalogIdentityUUID(id) {
			return bad()
		}
	}
	sort.Slice(p.Records, func(i, j int) bool {
		a, b := p.Records[i], p.Records[j]
		if a.Table != b.Table {
			return a.Table < b.Table
		}
		return a.SourceKey < b.SourceKey
	})
	sort.Slice(p.Accounts, func(i, j int) bool { return p.Accounts[i].AccountKey < p.Accounts[j].AccountKey })
	p.Plan.RecordCount = len(p.Records)
	p.Plan.Inventory, err = archive.EncodeSourceJSON(map[string]any{"retained_tables": counts, "external_tables": document.External})
	return p, err
}
