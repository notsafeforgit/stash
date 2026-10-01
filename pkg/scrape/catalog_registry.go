package scrape

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

var catalogRegistryColumns = map[string][]string{
	"catalogs":                       {"id", "kind", "label", "owner_key", "created_at", "redirect_to"},
	"routes":                         {"route", "catalog_id"},
	"links":                          {"source_id", "target_id", "reason", "linked_at"},
	"account_identifiers":            {"catalog_id", "account_key", "platform", "namespace", "kind", "value", "handle", "alias_key", "basis", "origin", "first_observed", "last_observed"},
	"account_identifier_checkpoints": {"catalog_id", "captured_at", "version"},
	"account_profile_urls":           {"account_key", "url", "basis", "first_observed"},
}

type RegistryCatalog struct{ ID, Kind, Label, OwnerKey, CreatedAt, RedirectTo string }
type RegistryIdentifier struct {
	RecordKey, CatalogID, AccountKey, Platform, Handle, AliasKey, Basis, Origin string
	Reference                                                                   models.AccountReference
	FirstObserved, LastObserved                                                 time.Time
}
type RegistryAccountGroup struct {
	Key, UUID, Namespace, Label string
	Strong                      *models.AccountReference
	Locator                     *models.AccountReference
	Locators                    []string
	Evidence                    []RegistryIdentifier
}
type RegistryAccountKeys struct {
	Key    string
	Groups []string
	Reason string
}
type PreparedCatalogRegistry struct {
	Plan        models.CatalogRegistryImportPlan
	Catalogs    map[string]RegistryCatalog
	Identifiers []RegistryIdentifier
	Records     []models.CatalogRegistryImportRecord
	Keys        map[string]bool
	Groups      map[string]*RegistryAccountGroup
	AccountKeys []RegistryAccountKeys
}

func RegistryImportUUID(source, kind, key string) string {
	return uuid.NewSHA1(uuid.MustParse(source), []byte(kind+"\x00"+key)).String()
}

func registryReferenceKey(ref models.AccountReference) string {
	body, _ := archive.EncodeSourceJSON([]string{ref.Namespace, ref.Kind, ref.Value})
	return string(body)
}

func (p *PreparedCatalogRegistry) ResolveCatalog(id string) (string, bool) {
	seen := map[string]bool{}
	for range 128 {
		row, ok := p.Catalogs[id]
		if !ok || seen[id] {
			return "", false
		}
		seen[id] = true
		if row.RedirectTo == "" {
			return id, true
		}
		id = row.RedirectTo
	}
	return "", false
}

func PrepareCatalogRegistry(input models.CatalogRegistryImportInput) (*PreparedCatalogRegistry, error) {
	location := "envelope"
	bad := func() (*PreparedCatalogRegistry, error) {
		return nil, fmt.Errorf("%w: %s", models.ErrCatalogRegistryImportInvalid, location)
	}
	if !catalogIdentityUUID(input.UUID) || !catalogIdentityUUID(input.SourceUUID) || !catalogIdentityUUID(input.IdentityImportUUID) {
		return bad()
	}
	object, err := archive.DecodeJSONObject(input.Document, 16<<20)
	if err != nil || len(object) != 3 {
		return bad()
	}
	var document struct {
		CapturedAt string                       `json:"captured_at"`
		Tables     map[string][]json.RawMessage `json:"tables"`
		External   map[string]int               `json:"external_tables"`
	}
	if json.Unmarshal(input.Document, &document) != nil || !journalTime(document.CapturedAt) || document.External == nil {
		return bad()
	}
	for _, required := range []string{"catalogs", "routes", "links"} {
		if document.Tables[required] == nil {
			return bad()
		}
	}
	if (document.Tables["account_identifiers"] == nil) != (document.Tables["account_identifier_checkpoints"] == nil) {
		return bad()
	}
	for table, count := range document.External {
		if catalogIdentityColumns[table] == nil || count < 0 {
			return bad()
		}
	}
	documentSum := sha256.Sum256(input.Document)
	value, err := archive.EncodeSourceJSON(map[string]any{"uuid": input.UUID, "source_uuid": input.SourceUUID, "identity_import_uuid": input.IdentityImportUUID, "document_sha256": hex.EncodeToString(documentSum[:])})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(value)
	p := &PreparedCatalogRegistry{Plan: models.CatalogRegistryImportPlan{UUID: input.UUID, SourceUUID: input.SourceUUID, IdentityImportUUID: input.IdentityImportUUID, CapturedAt: document.CapturedAt, InputSHA256: hex.EncodeToString(sum[:])},
		Catalogs: map[string]RegistryCatalog{}, Keys: map[string]bool{}, Groups: map[string]*RegistryAccountGroup{}, Records: []models.CatalogRegistryImportRecord{}}
	names := make([]string, 0, len(document.Tables))
	for name := range document.Tables {
		names = append(names, name)
	}
	sort.Strings(names)
	counts := map[string]int{}
	seen := map[string]bool{}
	for _, table := range names {
		location = table
		columns := catalogRegistryColumns[table]
		if columns == nil || document.Tables[table] == nil {
			return bad()
		}
		counts[table] = len(document.Tables[table])
		for index, body := range document.Tables[table] {
			location = fmt.Sprintf("%s row %d", table, index+1)
			row, err := archive.DecodeJSONObject(body, 1<<20)
			if err != nil || len(row) != len(columns) || len(p.Records) >= 25000 {
				return bad()
			}
			for _, column := range columns {
				if _, ok := row[column]; !ok {
					return bad()
				}
			}
			text := func(key string) string { v, _ := row[key].(string); return v }
			for key, value := range row {
				if key == "version" || key == "redirect_to" {
					continue
				}
				v, ok := value.(string)
				if !ok || len(v) > 8192 {
					return bad()
				}
			}
			var keys []string
			var identifier *RegistryIdentifier
			switch table {
			case "catalogs":
				if !catalogIdentityText(text("id"), 128, false) || !catalogIdentityText(text("kind"), 128, false) || !catalogIdentityText(text("owner_key"), 4096, false) || !journalTime(text("created_at")) {
					return bad()
				}
				if row["redirect_to"] != nil && !catalogIdentityText(text("redirect_to"), 128, false) {
					return bad()
				}
				p.Catalogs[text("id")] = RegistryCatalog{text("id"), text("kind"), text("label"), text("owner_key"), text("created_at"), text("redirect_to")}
				if strings.HasPrefix(text("owner_key"), "creator:") {
					p.Keys[strings.TrimPrefix(text("owner_key"), "creator:")] = true
				}
				keys = []string{text("id")}
			case "routes":
				if !catalogIdentityText(text("route"), 8192, false) || !catalogIdentityText(text("catalog_id"), 128, false) {
					return bad()
				}
				if strings.HasPrefix(text("route"), "owner:creator:") {
					p.Keys[strings.TrimPrefix(text("route"), "owner:creator:")] = true
				}
				keys = []string{text("route")}
			case "links":
				if !catalogIdentityText(text("source_id"), 128, false) || !catalogIdentityText(text("target_id"), 128, false) || !journalTime(text("linked_at")) {
					return bad()
				}
				keys = []string{text("source_id"), text("target_id")}
			case "account_identifier_checkpoints":
				// The old registry used an empty source timestamp for catalogs
				// without account captures. Keep that checkpoint as history only.
				if !catalogIdentityText(text("catalog_id"), 128, false) || (text("captured_at") != "" && !journalTime(text("captured_at"))) || !journalNumber(row["version"], 1, 2147483647, true) {
					return bad()
				}
				keys = []string{text("catalog_id")}
			case "account_profile_urls":
				if !catalogIdentityText(text("account_key"), 2048, false) || !catalogIdentityText(text("basis"), 128, false) || !journalTime(text("first_observed")) {
					return bad()
				}
				p.Keys[text("account_key")] = true
				keys = []string{text("account_key"), text("url")}
			case "account_identifiers":
				ref, err := archive.NormalizeAccountReference(models.AccountReference{Namespace: text("namespace"), Kind: text("kind"), Value: text("value")})
				if err != nil || ref.Namespace == "url" {
					return bad()
				}
				first, err := time.Parse(time.RFC3339Nano, text("first_observed"))
				if err != nil {
					return bad()
				}
				last, err := time.Parse(time.RFC3339Nano, text("last_observed"))
				if err != nil || last.Before(first) {
					return bad()
				}
				if !catalogIdentityText(text("catalog_id"), 128, false) || !catalogIdentityText(text("account_key"), 2048, false) || !catalogIdentityText(text("platform"), 128, false) || !catalogIdentityText(text("basis"), 128, false) || !catalogIdentityText(text("origin"), 128, false) || !catalogIdentityText(text("alias_key"), 2048, true) || !catalogIdentityText(text("handle"), 1024, true) {
					return bad()
				}
				identifier = &RegistryIdentifier{CatalogID: text("catalog_id"), AccountKey: text("account_key"), Platform: text("platform"), Handle: text("handle"), AliasKey: text("alias_key"), Basis: text("basis"), Origin: text("origin"), Reference: ref, FirstObserved: first, LastObserved: last}
				p.Keys[text("account_key")] = true
				keys = []string{text("catalog_id"), text("account_key"), text("namespace"), text("kind"), text("value"), text("alias_key"), text("basis"), text("origin")}
			}
			key, _ := archive.EncodeSourceJSON(keys)
			compound := table + "\x00" + string(key)
			if seen[compound] {
				return bad()
			}
			seen[compound] = true
			if identifier != nil {
				identifier.RecordKey = string(key)
				p.Identifiers = append(p.Identifiers, *identifier)
			}
			p.Records = append(p.Records, models.CatalogRegistryImportRecord{CatalogIdentityImportRecord: models.CatalogIdentityImportRecord{Table: table, SourceKey: string(key), Outcome: "copied", Evidence: body}})
		}
	}
	for id := range p.Catalogs {
		location = "catalog redirect graph"
		if _, ok := p.ResolveCatalog(id); !ok {
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
	sort.Slice(p.Identifiers, func(i, j int) bool { return p.Identifiers[i].RecordKey < p.Identifiers[j].RecordKey })
	p.Plan.RecordCount = len(p.Records)
	p.Plan.Inventory, err = archive.EncodeSourceJSON(map[string]any{"retained_tables": counts, "external_tables": document.External})
	return p, err
}

// Qualified captured identities create separate groups. Alias reuse never
// merges those groups; ambiguous inventory keys retain all candidate groups.
func (p *PreparedCatalogRegistry) PrepareAccounts(additionalKeys []string) {
	for _, key := range additionalKeys {
		p.Keys[key] = true
	}
	primary := map[string]map[string]bool{}
	aliases := map[string]map[string]bool{}
	add := func(index map[string]map[string]bool, key, group string) {
		if index[key] == nil {
			index[key] = map[string]bool{}
		}
		index[key][group] = true
	}
	for _, row := range p.Identifiers {
		if _, ok := p.Catalogs[row.CatalogID]; !ok || !row.Supported() {
			continue
		}
		key := registryReferenceKey(row.Reference)
		group := p.Groups[key]
		if group == nil {
			ref := row.Reference
			group = &RegistryAccountGroup{Key: key, UUID: RegistryImportUUID(p.Plan.SourceUUID, "account", key), Namespace: ref.Namespace, Label: row.Handle, Strong: &ref}
			p.Groups[key] = group
		}
		group.Evidence = append(group.Evidence, row)
		add(primary, row.AccountKey, key)
		if row.AliasKey != "" && strings.HasPrefix(row.Reference.Namespace, "native:") && (row.Basis == "captured-author" || row.Basis == "retained-source-pair") {
			add(aliases, row.AliasKey, key)
			if ref, ok := row.AliasReference(); ok {
				add(aliases, strings.TrimPrefix(ref.Namespace, "native:")+":handle:"+ref.Value, key)
			}
		}
	}
	keys := make([]string, 0, len(p.Keys))
	for key := range p.Keys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		candidates := map[string]bool{}
		for group := range primary[key] {
			candidates[group] = true
		}
		for group := range aliases[key] {
			candidates[group] = true
		}
		if ref, ok := registryLegacyLocator(key); ok && ref.Kind == "handle" {
			for group := range aliases[strings.TrimPrefix(ref.Namespace, "native:")+":handle:"+ref.Value] {
				candidates[group] = true
			}
		}
		// Mirror public identifiers can associate an inventory handle only in
		// its evidenced catalog. Display names never become handle identifiers.
		for _, row := range p.Identifiers {
			if row.AliasKey != key || row.Basis != "captured-mirror-public-id" || !row.Supported() {
				continue
			}
			catalog, ok := p.Catalogs[row.CatalogID]
			if ok && catalog.OwnerKey == "creator:"+key {
				candidates[registryReferenceKey(row.Reference)] = true
			}
		}
		entry := RegistryAccountKeys{Key: key, Groups: []string{}}
		for group := range candidates {
			entry.Groups = append(entry.Groups, group)
		}
		sort.Strings(entry.Groups)
		if len(entry.Groups) > 1 {
			entry.Reason = "conflicting_captured_account_identities"
		}
		if len(entry.Groups) == 0 {
			ref, ok := registryLegacyLocator(key)
			if !ok {
				entry.Reason = "unqualified_legacy_account_locator"
			} else {
				groupKey := "locator:" + registryReferenceKey(ref)
				if p.Groups[groupKey] == nil {
					p.Groups[groupKey] = &RegistryAccountGroup{Key: groupKey, UUID: RegistryImportUUID(p.Plan.SourceUUID, "account", groupKey), Namespace: ref.Namespace, Label: ref.Value, Locator: &ref}
				}
				p.Groups[groupKey].Locators = append(p.Groups[groupKey].Locators, key)
				entry.Groups = append(entry.Groups, groupKey)
			}
		}
		p.AccountKeys = append(p.AccountKeys, entry)
	}
}

func (r RegistryIdentifier) Supported() bool {
	if strings.HasPrefix(r.Reference.Namespace, "native:") && (r.Basis == "captured-author" || r.Basis == "retained-source-pair") {
		return r.Reference.Kind == "id" || r.Reference.Kind == "secUid" || r.Reference.Kind == "did" || r.Reference.Kind == "uuid"
	}
	return strings.HasPrefix(r.Reference.Namespace, "mirror:") && r.Reference.Kind == "user" && (r.Basis == "captured-mirror-name" || r.Basis == "captured-mirror-public-id")
}

func (r RegistryIdentifier) GroupKey() string { return registryReferenceKey(r.Reference) }

func (r RegistryIdentifier) AliasReference() (models.AccountReference, bool) {
	if r.Handle == "" {
		return models.AccountReference{}, false
	}
	kind := "handle"
	if strings.HasPrefix(r.Reference.Namespace, "mirror:") {
		if r.Basis != "captured-mirror-public-id" {
			return models.AccountReference{}, false
		}
		kind = "public_id"
	}
	ref, err := archive.NormalizeAccountReference(models.AccountReference{Namespace: r.Reference.Namespace, Kind: kind, Value: r.Handle})
	return ref, err == nil
}

func registryLegacyLocator(key string) (models.AccountReference, bool) {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 {
		return models.AccountReference{}, false
	}
	switch parts[0] {
	case "reddit", "twitter", "instagram", "bluesky", "tiktok", "tumblr":
	default:
		return models.AccountReference{}, false
	}
	kind, value := "legacy_key", key
	if parts[1] == "handle" {
		kind, value = "handle", parts[2]
	} else if parts[1] != "id" {
		return models.AccountReference{}, false
	}
	ref, err := archive.NormalizeAccountReference(models.AccountReference{Namespace: "native:" + parts[0], Kind: kind, Value: value})
	return ref, err == nil
}
