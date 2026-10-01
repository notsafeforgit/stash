package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/txn"
)

type CatalogRegistryImportStore struct{}

func (s *CatalogRegistryImportStore) Find(ctx context.Context, id string) (*models.CatalogRegistryImport, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrCatalogRegistryImportInvalid
	}
	var row struct {
		Plan    string `db:"plan"`
		Created string `db:"created_at"`
	}
	if err := dbWrapper.Get(ctx, &row, "SELECT plan,created_at FROM catalog_registry_imports WHERE uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	ret := &models.CatalogRegistryImport{}
	if err := json.Unmarshal([]byte(row.Plan), &ret.CatalogRegistryImportPlan); err != nil {
		return nil, err
	}
	var err error
	ret.CreatedAt, err = time.Parse(time.RFC3339Nano, row.Created)
	return ret, err
}

func (s *CatalogRegistryImportStore) Preview(ctx context.Context, input models.CatalogRegistryImportInput, now time.Time) (*models.CatalogRegistryImportPlan, error) {
	p, err := scrape.PrepareCatalogRegistry(input)
	if err != nil {
		return nil, err
	}
	return s.preview(ctx, input, p, now)
}

func (s *CatalogRegistryImportStore) preview(ctx context.Context, input models.CatalogRegistryImportInput, p *scrape.PreparedCatalogRegistry, now time.Time) (*models.CatalogRegistryImportPlan, error) {
	captured, _ := time.Parse(time.RFC3339Nano, p.Plan.CapturedAt)
	if !validJobTime(now) || captured.After(now.Add(time.Minute)) {
		return nil, models.ErrCatalogRegistryImportInvalid
	}
	if prior, err := s.Find(ctx, input.UUID); err != nil {
		return nil, err
	} else if prior != nil {
		if prior.InputSHA256 != p.Plan.InputSHA256 {
			return nil, models.ErrCatalogRegistryImportConflict
		}
		return &prior.CatalogRegistryImportPlan, nil
	}
	var used bool
	if err := dbWrapper.Get(ctx, &used, "SELECT EXISTS(SELECT 1 FROM catalog_registry_imports WHERE source_uuid=?)", input.SourceUUID); err != nil {
		return nil, err
	}
	if used {
		return nil, models.ErrCatalogRegistryImportConflict
	}
	parent, err := (&CatalogIdentityImportStore{}).Find(ctx, input.IdentityImportUUID)
	if err != nil {
		return nil, err
	}
	if parent == nil || parent.SourceUUID != input.SourceUUID || parent.CapturedAt != p.Plan.CapturedAt {
		return nil, models.ErrCatalogRegistryImportInvalid
	}
	var before, after struct {
		Retained map[string]int `json:"retained_tables"`
		External map[string]int `json:"external_tables"`
	}
	if json.Unmarshal(parent.Inventory, &before) != nil || json.Unmarshal(p.Plan.Inventory, &after) != nil || !reflect.DeepEqual(before.Retained, after.External) || !reflect.DeepEqual(before.External, after.Retained) {
		return nil, models.ErrCatalogRegistryImportInvalid
	}
	keys := make([]string, 0, len(parent.Ownership))
	for _, a := range parent.Ownership {
		keys = append(keys, a.AccountKey)
	}
	p.PrepareAccounts(keys)
	explicit := map[string][]string{}
	for _, row := range p.AccountKeys {
		if len(row.Groups) != 1 {
			continue
		}
		for _, old := range parent.Ownership {
			if old.AccountKey == row.Key && old.AccountUUID != "" {
				explicit[row.Groups[0]] = append(explicit[row.Groups[0]], old.AccountUUID)
			}
		}
	}
	p.Plan.Accounts = []models.CatalogRegistryAccountAction{}
	p.Plan.AccountKeys = []models.CatalogRegistryKeyAction{}
	p.Plan.Collections = []models.CatalogRegistryCollectionAction{}
	p.Plan.Ownership = []models.CatalogOwnershipAction{}
	p.Plan.Records = []models.CatalogRegistryImportRecord{}
	groups := make([]string, 0, len(p.Groups))
	for key := range p.Groups {
		groups = append(groups, key)
	}
	sort.Strings(groups)
	accountActions := map[string]models.CatalogRegistryAccountAction{}
	for _, key := range groups {
		a, err := registryAccountPlan(ctx, p.Groups[key], explicit[key])
		if err != nil {
			return nil, err
		}
		accountActions[key] = a
		p.Plan.Accounts = append(p.Plan.Accounts, a)
	}
	keyActions := map[string]models.CatalogRegistryKeyAction{}
	for _, row := range p.AccountKeys {
		a := models.CatalogRegistryKeyAction{AccountKey: row.Key, Action: "review", Reason: row.Reason, Candidates: []string{}}
		resolved := len(row.Groups) != 0
		for _, key := range row.Groups {
			account := accountActions[key]
			if account.Action == "review" {
				resolved = false
				a.Candidates = append(a.Candidates, account.Candidates...)
				if len(row.Groups) == 1 {
					a.Reason = account.Reason
				}
			} else {
				a.Candidates = append(a.Candidates, account.AccountUUID)
			}
		}
		sort.Strings(a.Candidates)
		a.Candidates = slices.Compact(a.Candidates)
		// Distinct captured references may already resolve to one native account
		// (for example, its numeric TikTok ID and secUid). A shared handle alone
		// never establishes that equivalence.
		if resolved && len(a.Candidates) == 1 {
			a.Action, a.Reason, a.AccountUUID = "mapped", "", a.Candidates[0]
		}
		keyActions[row.Key] = a
		p.Plan.AccountKeys = append(p.Plan.AccountKeys, a)
	}
	ids := make([]string, 0, len(p.Catalogs))
	for id := range p.Catalogs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	collections := map[string]models.CatalogRegistryCollectionAction{}
	for _, id := range ids {
		row := p.Catalogs[id]
		if row.RedirectTo != "" {
			continue
		}
		a := models.CatalogRegistryCollectionAction{CatalogID: id, CollectionUUID: scrape.RegistryImportUUID(input.SourceUUID, "collection", id), Label: row.Label, Action: "create"}
		if strings.HasPrefix(row.OwnerKey, "creator:") {
			key := keyActions[strings.TrimPrefix(row.OwnerKey, "creator:")]
			if key.Action == "mapped" {
				a.AccountUUID = &key.AccountUUID
				for _, account := range accountActions {
					if account.AccountUUID == key.AccountUUID {
						a.Namespace = account.Namespace
						break
					}
				}
			}
		}
		if !sourceDefinitionText(a.Label, "disabled", "") {
			a.Action, a.Reason = "review", "invalid_collection_label"
		}
		prior, err := (&SourceCollectionStore{}).Find(ctx, a.CollectionUUID)
		if err != nil {
			return nil, err
		}
		if prior != nil {
			a.Action, a.Reason = "review", "reserved_collection_uuid_in_use"
		}
		collections[id] = a
	}
	for _, id := range ids {
		row := p.Catalogs[id]
		if row.RedirectTo != "" {
			target, _ := p.ResolveCatalog(id)
			a := collections[target]
			a.CatalogID, a.RedirectTo = id, target
			if a.Action != "review" {
				a.Action, a.Reason = "mapped", "historical_catalog_redirect"
			}
			collections[id] = a
		}
		p.Plan.Collections = append(p.Plan.Collections, collections[id])
	}
	var old []struct {
		Evidence string `db:"evidence"`
	}
	if err := dbWrapper.Select(ctx, &old, "SELECT evidence FROM catalog_identity_import_records WHERE import_uuid=? AND source_table='performer_account_associations' ORDER BY source_key", parent.UUID); err != nil {
		return nil, err
	}
	for _, row := range old {
		var association struct {
			AccountKey string  `json:"account_key"`
			Identity   *string `json:"identity_id"`
			Source     string  `json:"source"`
		}
		if err := json.Unmarshal([]byte(row.Evidence), &association); err != nil {
			return nil, err
		}
		key := keyActions[association.AccountKey]
		a := models.CatalogOwnershipAction{AccountKey: association.AccountKey, AccountUUID: key.AccountUUID, Action: "review", Reason: "unresolved_registry_account"}
		if key.Action != "mapped" {
			p.Plan.Ownership = append(p.Plan.Ownership, a)
			continue
		}
		if association.Identity == nil && association.Source == "migration_conflict" {
			a.Reason = "legacy_ownership_conflict"
			p.Plan.Ownership = append(p.Plan.Ownership, a)
			continue
		}
		if association.Identity != nil {
			owner, err := (&ArchiveEntityStore{}).Resolve(ctx, *association.Identity)
			if err != nil {
				return nil, err
			}
			if owner == nil || owner.Kind != models.ArchivePerformer || owner.State != models.ArchiveEntityActive {
				a.Reason = "unresolved_registry_performer"
				p.Plan.Ownership = append(p.Plan.Ownership, a)
				continue
			}
			a.IdentityUUID = owner.UUID
			// Pin the performer's current revision separately from the account.
			a.PerformerRevision = owner.Revision
		}
		account, err := (&SourceAccountStore{}).Find(ctx, key.AccountUUID)
		if err != nil {
			return nil, err
		}
		if account != nil {
			a.AccountRevision = account.Revision
		}
		current, err := (&SourceAccountStore{}).Ownership(ctx, key.AccountUUID)
		if err != nil {
			return nil, err
		}
		if current != nil {
			a.DecisionUUID = current.UUID
			matches := current.State == models.AccountOwnershipUnlinked && association.Identity == nil
			if current.State == models.AccountOwnershipLinked && current.PerformerUUID != nil && a.IdentityUUID != "" {
				owner, err := (&ArchiveEntityStore{}).Resolve(ctx, *current.PerformerUUID)
				if err != nil {
					return nil, err
				}
				matches = owner != nil && owner.UUID == a.IdentityUUID
			}
			if matches {
				a.Action, a.Reason = "mapped", "same_native_ownership_choice"
			} else {
				a.Reason = "native_ownership_already_decided"
			}
		} else {
			a.Action, a.Reason = "linked", "explicit_saved_account_association"
			if association.Identity == nil {
				a.Action, a.Reason = "unlinked", "explicit_saved_account_unlink"
			}
		}
		p.Plan.Ownership = append(p.Plan.Ownership, a)
	}
	choices := map[string]string{}
	conflicts := map[string]bool{}
	for _, a := range p.Plan.Ownership {
		if a.AccountUUID == "" {
			continue
		}
		choice := a.IdentityUUID
		if a.Action == "review" {
			choice = "review:" + a.AccountKey
		}
		if prior, ok := choices[a.AccountUUID]; ok && prior != choice {
			conflicts[a.AccountUUID] = true
		}
		choices[a.AccountUUID] = choice
	}
	for i := range p.Plan.Ownership {
		a := &p.Plan.Ownership[i]
		if conflicts[a.AccountUUID] {
			a.Action, a.Reason = "review", "conflicting_saved_account_choices"
		}
	}
	for _, record := range p.Records {
		registryRecordOutcome(&record, p, accountActions, keyActions, collections)
		record.Evidence = nil
		p.Plan.Records = append(p.Plan.Records, record)
	}
	hash, err := sourceRunHash(p.Plan)
	p.Plan.PlanSHA256 = hash
	return &p.Plan, err
}

func registryAccountPlan(ctx context.Context, group *scrape.RegistryAccountGroup, explicit []string) (models.CatalogRegistryAccountAction, error) {
	a := models.CatalogRegistryAccountAction{Key: group.Key, AccountUUID: group.UUID, Namespace: group.Namespace, Label: group.Label, Action: "create", Candidates: []string{}}
	if !validAccountText(a.Label, 1024, true) {
		a.Action, a.Reason = "review", "invalid_account_label"
		return a, nil
	}
	ref := group.Strong
	if ref == nil {
		ref = group.Locator
	}
	candidates, err := (&SourceAccountStore{}).Lookup(ctx, *ref, "", 101)
	if err != nil {
		return a, err
	}
	for _, candidate := range candidates {
		a.Candidates = append(a.Candidates, candidate.UUID)
	}
	var selected *models.SourceAccount
	for _, id := range explicit {
		account, err := (&SourceAccountStore{}).Resolve(ctx, id)
		if err != nil {
			return a, err
		}
		if account != nil && !slices.Contains(a.Candidates, account.UUID) {
			a.Candidates = append(a.Candidates, account.UUID)
			sort.Strings(a.Candidates)
		}
		if account == nil || account.Namespace != group.Namespace || (selected != nil && selected.UUID != account.UUID) {
			a.Action, a.Reason = "review", "explicit_account_bindings_disagree"
			return a, nil
		}
		selected = account
	}
	if selected != nil && group.Strong != nil {
		for _, candidate := range candidates {
			if candidate.UUID != selected.UUID {
				a.Action, a.Reason = "review", "captured_id_conflicts_with_explicit_binding"
				return a, nil
			}
		}
	}
	if selected == nil && (len(candidates) > 1 || (group.Strong == nil && len(candidates) != 0)) {
		a.Action, a.Reason = "review", "existing_account_candidates_require_review"
		return a, nil
	}
	if selected == nil && len(candidates) == 1 {
		selected = candidates[0]
	}
	if selected != nil {
		account := selected
		a.AccountUUID, a.Revision = account.UUID, account.Revision
		identifiers, err := (&SourceAccountStore{}).Identifiers(ctx, account.UUID, "", 1000)
		if err != nil {
			return a, err
		}
		if len(identifiers) == 1000 {
			a.Action, a.Reason = "review", "account_identifier_review_limit"
			return a, nil
		}
		for _, known := range identifiers {
			r := known.Reference
			if group.Strong != nil && r.Namespace == ref.Namespace && r.Kind == ref.Kind && r.Value != ref.Value {
				a.Action, a.Reason = "review", "native_account_has_conflicting_ids"
				return a, nil
			}
		}
		a.Action, a.Reason = "mapped", "same_captured_account_identifier"
		if len(explicit) != 0 {
			a.Reason = "explicit_native_account_binding"
		}
		return a, nil
	}
	prior, err := (&SourceAccountStore{}).Find(ctx, a.AccountUUID)
	if err != nil {
		return a, err
	}
	if prior != nil {
		a.Action, a.Reason = "review", "reserved_account_uuid_in_use"
	}
	return a, nil
}

func registryRecordOutcome(r *models.CatalogRegistryImportRecord, p *scrape.PreparedCatalogRegistry, accounts map[string]models.CatalogRegistryAccountAction, keys map[string]models.CatalogRegistryKeyAction, collections map[string]models.CatalogRegistryCollectionAction) {
	var row map[string]any
	_ = json.Unmarshal(r.Evidence, &row)
	text := func(key string) string { v, _ := row[key].(string); return v }
	catalog := text("catalog_id")
	if r.Table == "catalogs" {
		catalog = text("id")
	}
	if r.Table == "links" {
		catalog = text("source_id")
	}
	unresolvedCatalog := false
	if catalog != "" {
		a, ok := collections[catalog]
		if !ok || a.Action == "review" {
			unresolvedCatalog = true
		} else {
			r.CollectionUUID = &a.CollectionUUID
		}
	}
	switch r.Table {
	case "catalogs":
		r.Outcome = "mapped"
	case "routes":
		route := text("route")
		r.Reason = "historical_catalog_route"
		if strings.HasPrefix(route, "directory:") {
			prefix := strings.TrimPrefix(route, "directory:")
			suffix := ":" + text("catalog_id")
			if !strings.HasSuffix(prefix, suffix) || !archive.ValidRootRelativePath(strings.TrimSuffix(prefix, suffix), true) {
				r.Outcome, r.Reason = "review", "invalid_or_unknown_directory_route"
			}
		} else if !strings.HasPrefix(route, "owner:") && !strings.HasPrefix(route, "media:") {
			r.Outcome, r.Reason = "review", "unknown_route_kind"
		}
	case "links":
		if target, ok := collections[text("target_id")]; !ok || target.Action == "review" {
			r.Outcome, r.Reason = "review", "unknown_catalog_link_target"
		}
	case "account_identifier_checkpoints":
		r.Reason = "historical_identifier_checkpoint"
	case "account_identifiers":
		for _, identifier := range p.Identifiers {
			if identifier.RecordKey != r.SourceKey {
				continue
			}
			a, ok := accounts[identifier.GroupKey()]
			if !ok || a.Action == "review" {
				r.Outcome, r.Reason = "review", "unresolved_captured_account"
			} else {
				r.Outcome = "mapped"
				r.AccountUUID = &a.AccountUUID
			}
			break
		}
	case "account_profile_urls":
		key := keys[text("account_key")]
		_, valid := archive.CanonicalProfileURL(text("url"))
		if key.Action != "mapped" || !valid {
			r.Outcome, r.Reason = "review", "unresolved_account_profile_url"
		} else {
			r.Outcome = "mapped"
			r.AccountUUID = &key.AccountUUID
		}
	}
	if unresolvedCatalog {
		r.Outcome, r.Reason = "review", "unresolved_source_catalog"
	}
}

func (s *CatalogRegistryImportStore) Apply(ctx context.Context, input models.CatalogRegistryImportInput, expected string, now time.Time) (*models.CatalogRegistryImport, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !archive.ValidSHA256(expected) || !validJobTime(now) {
		return nil, models.ErrCatalogRegistryImportInvalid
	}
	p, err := scrape.PrepareCatalogRegistry(input)
	if err != nil {
		return nil, err
	}
	if prior, err := s.Find(ctx, input.UUID); err != nil {
		return nil, err
	} else if prior != nil {
		if prior.InputSHA256 != p.Plan.InputSHA256 || prior.PlanSHA256 != expected {
			return nil, models.ErrCatalogRegistryImportConflict
		}
		return prior, nil
	}
	plan, err := s.preview(ctx, input, p, now)
	if err != nil {
		return nil, err
	}
	if plan.PlanSHA256 != expected {
		return nil, models.ErrCatalogRegistryImportConflict
	}
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return errors.New("catalog registry import did not finish atomically")
		}
		return nil
	})
	accounts := &SourceAccountStore{}
	for _, a := range plan.Accounts {
		if a.Action == "review" {
			continue
		}
		if a.Action == "create" {
			if _, err := accounts.create(ctx, a.AccountUUID, a.Namespace, a.Label); err != nil {
				return nil, err
			}
		}
		group := p.Groups[a.Key]
		if group.Locator != nil {
			captured, _ := time.Parse(time.RFC3339Nano, plan.CapturedAt)
			for _, key := range group.Locators {
				evidence, err := archive.EncodeSourceJSON(map[string]any{"registry_import_uuid": plan.UUID, "legacy_account_key": key})
				if err != nil {
					return nil, err
				}
				if _, err := accounts.ObserveIdentifier(ctx, a.AccountUUID, *group.Locator, models.AccountIdentifierEvidence{Key: scrape.RegistryImportUUID(plan.SourceUUID, "locator", key), Basis: "legacy-locator", Origin: "migration", Details: evidence, FirstObserved: captured, LastObserved: captured}); err != nil {
					return nil, err
				}
			}
		}
		for _, row := range group.Evidence {
			details, err := archive.EncodeSourceJSON(map[string]any{"registry_import_uuid": plan.UUID, "source_table": "account_identifiers", "source_key": row.RecordKey, "origin": row.Origin})
			if err != nil {
				return nil, err
			}
			refs := []models.AccountReference{row.Reference}
			if alias, ok := row.AliasReference(); ok {
				refs = append(refs, alias)
			}
			for _, ref := range refs {
				if _, err := accounts.ObserveIdentifier(ctx, a.AccountUUID, ref, models.AccountIdentifierEvidence{Key: scrape.RegistryImportUUID(plan.SourceUUID, "identifier", row.RecordKey), Basis: row.Basis, Origin: "migration", Details: details, FirstObserved: row.FirstObserved, LastObserved: row.LastObserved}); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, a := range plan.Collections {
		if a.Action != "create" {
			continue
		}
		_, err := (&SourceCollectionStore{}).Put(ctx, models.SourceCollectionInput{UUID: a.CollectionUUID, Origin: "migration", Reason: "Imported catalog grouping; source activation requires a reviewed definition", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: a.Label, Kind: "legacy_catalog", Namespace: a.Namespace, State: "disabled", AccountUUID: a.AccountUUID}})
		if err != nil {
			return nil, err
		}
	}
	for i, record := range p.Records {
		mapping := plan.Records[i]
		if record.Table != "account_profile_urls" || mapping.Outcome != "mapped" {
			continue
		}
		var row map[string]string
		if err := json.Unmarshal(record.Evidence, &row); err != nil {
			return nil, err
		}
		first, _ := time.Parse(time.RFC3339Nano, row["first_observed"])
		details, err := archive.EncodeSourceJSON(map[string]any{"registry_import_uuid": plan.UUID, "source_table": record.Table, "source_key": record.SourceKey})
		if err != nil {
			return nil, err
		}
		_, err = accounts.ObserveIdentifier(ctx, *mapping.AccountUUID, models.AccountReference{Namespace: "url", Kind: "profile", Value: row["url"]}, models.AccountIdentifierEvidence{Key: scrape.RegistryImportUUID(plan.SourceUUID, "profile-url", record.SourceKey), Basis: row["basis"], Origin: "migration", Details: details, FirstObserved: first, LastObserved: first})
		if err != nil {
			return nil, err
		}
	}
	chosen := map[string]bool{}
	for _, a := range plan.Ownership {
		if (a.Action != "linked" && a.Action != "unlinked") || chosen[a.AccountUUID] {
			continue
		}
		account, err := accounts.Find(ctx, a.AccountUUID)
		if err != nil {
			return nil, err
		}
		_, err = accounts.DecideOwnership(ctx, models.AccountOwnershipInput{AccountUUID: a.AccountUUID, ExpectedAccountRevision: account.Revision, State: models.AccountOwnershipState(a.Action), PerformerUUID: a.IdentityUUID, ExpectedPerformerRevision: a.PerformerRevision, Origin: "migration", Reason: "Imported explicit registry account choice"})
		if err != nil {
			return nil, err
		}
		chosen[a.AccountUUID] = true
	}
	body, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_registry_imports(uuid,source_uuid,identity_import_uuid,input_sha256,plan_sha256,record_count,plan,created_at) VALUES(?,?,?,?,?,?,?,?)`, plan.UUID, plan.SourceUUID, plan.IdentityImportUUID, plan.InputSHA256, plan.PlanSHA256, plan.RecordCount, string(body), now.UTC().Format(time.RFC3339Nano)); err != nil {
		return nil, err
	}
	for i, record := range plan.Records {
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_registry_import_records(import_uuid,source_table,source_key,outcome,reason,account_uuid,collection_uuid,evidence) VALUES(?,?,?,?,?,?,?,?)`, plan.UUID, record.Table, record.SourceKey, record.Outcome, record.Reason, record.AccountUUID, record.CollectionUUID, string(p.Records[i].Evidence)); err != nil {
			return nil, err
		}
	}
	for _, a := range plan.AccountKeys {
		if a.Action == "mapped" {
			if _, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_account_mappings(source_uuid,account_key,account_uuid,import_uuid) VALUES(?,?,?,?)`, plan.SourceUUID, a.AccountKey, a.AccountUUID, plan.UUID); err != nil {
				return nil, err
			}
		}
	}
	for _, a := range plan.Collections {
		if a.Action != "review" {
			if _, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_collection_mappings(source_uuid,catalog_id,collection_uuid,import_uuid) VALUES(?,?,?,?)`, plan.SourceUUID, a.CatalogID, a.CollectionUUID, plan.UUID); err != nil {
				return nil, err
			}
		}
	}
	complete = true
	return &models.CatalogRegistryImport{CatalogRegistryImportPlan: *plan, CreatedAt: now.UTC()}, nil
}

func (s *CatalogRegistryImportStore) Records(ctx context.Context, id string, after int64, limit int) ([]models.CatalogRegistryImportRecord, error) {
	if !validSourceRunUUID(id) || after < 0 || limit < 1 || limit > 100 {
		return nil, models.ErrCatalogRegistryImportInvalid
	}
	var rows []struct {
		models.CatalogRegistryImportRecord
		Body string `db:"evidence"`
	}
	if err := dbWrapper.Select(ctx, &rows, `SELECT id,source_table,source_key,outcome,reason,account_uuid,collection_uuid,evidence FROM catalog_registry_import_records WHERE import_uuid=? AND id>? ORDER BY id LIMIT ?`, id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.CatalogRegistryImportRecord, 0, len(rows))
	for _, row := range rows {
		row.Evidence = json.RawMessage(row.Body)
		ret = append(ret, row.CatalogRegistryImportRecord)
	}
	return ret, nil
}
