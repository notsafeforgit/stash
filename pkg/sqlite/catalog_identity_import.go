package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/txn"
)

type CatalogIdentityImportStore struct{}

func (s *CatalogIdentityImportStore) Find(ctx context.Context, id string) (*models.CatalogIdentityImport, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrCatalogIdentityImportInvalid
	}
	var row struct {
		Plan    string `db:"plan"`
		Created string `db:"created_at"`
	}
	if err := dbWrapper.Get(ctx, &row, "SELECT plan,created_at FROM catalog_identity_imports WHERE uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	ret := &models.CatalogIdentityImport{}
	if err := json.Unmarshal([]byte(row.Plan), &ret.CatalogIdentityImportPlan); err != nil {
		return nil, err
	}
	var err error
	ret.CreatedAt, err = time.Parse(time.RFC3339Nano, row.Created)
	return ret, err
}

func (s *CatalogIdentityImportStore) Preview(ctx context.Context, input models.CatalogIdentityImportInput, now time.Time) (*models.CatalogIdentityImportPlan, error) {
	p, err := scrape.PrepareCatalogIdentities(input)
	if err != nil {
		return nil, err
	}
	return s.preview(ctx, input, p, now)
}

func (s *CatalogIdentityImportStore) preview(ctx context.Context, input models.CatalogIdentityImportInput, p *scrape.PreparedCatalogIdentities, now time.Time) (*models.CatalogIdentityImportPlan, error) {
	captured, _ := time.Parse(time.RFC3339Nano, p.Plan.CapturedAt)
	if !validJobTime(now) || captured.After(now.Add(time.Minute)) {
		return nil, models.ErrCatalogIdentityImportInvalid
	}
	if prior, err := s.Find(ctx, input.UUID); err != nil {
		return nil, err
	} else if prior != nil {
		if prior.InputSHA256 != p.Plan.InputSHA256 {
			return nil, models.ErrCatalogIdentityImportConflict
		}
		return &prior.CatalogIdentityImportPlan, nil
	}
	// A registry is cut over once per explicitly selected Stash namespace. A
	// newer frozen snapshot must not silently replay obsolete ownership choices.
	var used bool
	if err := dbWrapper.Get(ctx, &used, `SELECT EXISTS(SELECT 1 FROM catalog_identity_imports WHERE source_uuid=? AND namespace=?)`, input.SourceUUID, input.Namespace); err != nil {
		return nil, err
	}
	if used {
		return nil, models.ErrCatalogIdentityImportConflict
	}
	ids := make([]string, 0, len(p.Identities))
	for id := range p.Identities {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	actions := map[string]models.CatalogIdentityAction{}
	for _, id := range ids {
		row := p.Identities[id]
		if row.RedirectTo != "" {
			continue
		}
		a, err := catalogIdentityAction(ctx, p, input.Namespace, id)
		if err != nil {
			return nil, err
		}
		actions[id] = a
	}
	// The same local performer cannot adopt two distinct, still-active catalog
	// UUIDs. Both alternatives remain visible instead of choosing by row order.
	claims := map[string][]string{}
	for id, a := range actions {
		if a.Action != "review" {
			claims[a.CurrentUUID] = append(claims[a.CurrentUUID], id)
		}
	}
	for _, group := range claims {
		if len(group) > 1 {
			for _, id := range group {
				a := actions[id]
				a.Action, a.Reason = "review", "multiple_catalog_identities_for_local_performer"
				actions[id] = a
			}
		}
	}
	for _, id := range ids {
		row := p.Identities[id]
		if row.RedirectTo == "" {
			continue
		}
		target, _ := p.Resolve(id)
		a := models.CatalogIdentityAction{IdentityUUID: id, RedirectTo: target, Action: "review", Reason: "unresolved_merge_survivor"}
		if actions[target].Action != "review" {
			existing, err := (&ArchiveEntityStore{}).Find(ctx, id)
			if err != nil {
				return nil, err
			}
			a.Action, a.Reason = "redirect", "historical_catalog_merge"
			if existing != nil {
				a.CurrentUUID, a.CurrentRevision = existing.UUID, existing.Revision
				resolved, err := (&ArchiveEntityStore{}).Resolve(ctx, id)
				if err != nil {
					return nil, err
				}
				if existing.State == models.ArchiveEntityRedirected && resolved != nil && resolved.UUID == actions[target].CurrentUUID {
					a.Action = "mapped"
				} else {
					a.Action, a.Reason = "review", "historical_uuid_already_in_use"
				}
			}
		}
		actions[id] = a
	}
	for _, id := range ids {
		p.Plan.Identities = append(p.Plan.Identities, actions[id])
	}
	for _, account := range p.Accounts {
		a, err := catalogOwnershipAction(ctx, input, account, p, actions)
		if err != nil {
			return nil, err
		}
		p.Plan.Ownership = append(p.Plan.Ownership, a)
	}
	// Different old keys may intentionally bind the same native account. Equal
	// choices coalesce during apply; contradictory choices never win by ordering.
	choices := map[string]string{}
	conflicting := map[string]bool{}
	for _, a := range p.Plan.Ownership {
		if a.AccountRevision == 0 {
			continue
		}
		choice := a.IdentityUUID
		if previous, ok := choices[a.AccountUUID]; ok && previous != choice {
			conflicting[a.AccountUUID] = true
		}
		choices[a.AccountUUID] = choice
	}
	for i := range p.Plan.Ownership {
		a := &p.Plan.Ownership[i]
		if conflicting[a.AccountUUID] {
			a.Action, a.Reason = "review", "conflicting_saved_account_choices"
		}
	}
	p.Plan.Records = make([]models.CatalogIdentityImportRecord, 0, len(p.Records))
	for _, record := range p.Records {
		if err := catalogIdentityRecordOutcome(ctx, &record, p, &p.Plan); err != nil {
			return nil, err
		}
		record.Evidence = nil
		p.Plan.Records = append(p.Plan.Records, record)
	}
	hash, err := sourceRunHash(p.Plan)
	p.Plan.PlanSHA256 = hash
	return &p.Plan, err
}

func catalogIdentityAction(ctx context.Context, p *scrape.PreparedCatalogIdentities, namespace, id string) (models.CatalogIdentityAction, error) {
	a := models.CatalogIdentityAction{IdentityUUID: id, Action: "review", Reason: "unbound_catalog_identity"}
	var bindings []scrape.CatalogIdentityBinding
	for _, b := range p.Bindings {
		resolved, _ := p.Resolve(b.IdentityUUID)
		if b.Namespace == namespace && b.RedirectTo == "" && resolved == id {
			bindings = append(bindings, b)
		}
	}
	store := &ArchiveEntityStore{}
	existing, err := store.Find(ctx, id)
	if err != nil {
		return a, err
	}
	if len(bindings) > 1 {
		a.Reason = "multiple_local_performer_bindings"
		return a, nil
	}
	if len(bindings) == 0 {
		// An exact UUID already present natively is sufficient; name/alias lookup
		// is deliberately absent. Unbound identities are retained for review.
		if existing != nil && existing.Kind == models.ArchivePerformer && existing.State == models.ArchiveEntityActive {
			a.Action, a.Reason, a.CurrentUUID, a.CurrentRevision, a.LocalID = "mapped", "existing_catalog_uuid", existing.UUID, existing.Revision, existing.LocalID
		}
		return a, nil
	}
	b := bindings[0]
	local, ok := scrape.CatalogLocalPerformerID(b.PerformerID)
	if !ok {
		a.Reason = "invalid_local_performer_binding"
		return a, nil
	}
	a.LocalID = &local
	current, err := store.FindByLocalID(ctx, models.ArchivePerformer, local)
	if err != nil {
		return a, err
	}
	if current == nil {
		a.Reason = "local_performer_no_longer_exists"
		return a, nil
	}
	a.CurrentUUID, a.CurrentRevision = current.UUID, current.Revision
	updated, _ := time.Parse(time.RFC3339Nano, b.UpdatedAt)
	if current.CreatedAt.After(updated) {
		a.Reason = "local_performer_id_may_have_been_reused"
		return a, nil
	}
	if existing != nil {
		resolved, err := store.Resolve(ctx, id)
		if err != nil {
			return a, err
		}
		if resolved == nil || resolved.UUID != current.UUID || resolved.State != models.ArchiveEntityActive || resolved.Kind != models.ArchivePerformer {
			a.Reason = "catalog_uuid_already_in_use"
			return a, nil
		}
		a.Action, a.Reason = "mapped", "existing_catalog_uuid"
	} else {
		a.Action, a.Reason = "adopt", "explicit_local_performer_binding"
	}
	return a, nil
}

func catalogOwnershipAction(ctx context.Context, input models.CatalogIdentityImportInput, row scrape.CatalogAccountAssociation, p *scrape.PreparedCatalogIdentities, identities map[string]models.CatalogIdentityAction) (models.CatalogOwnershipAction, error) {
	a := models.CatalogOwnershipAction{AccountKey: row.AccountKey, AccountUUID: input.AccountBindings[row.AccountKey], Action: "review", Reason: "native_account_binding_required"}
	if row.IdentityUUID != "" {
		a.IdentityUUID, _ = p.Resolve(row.IdentityUUID)
	}
	if a.AccountUUID == "" {
		return a, nil
	}
	store := &SourceAccountStore{}
	account, err := store.Find(ctx, a.AccountUUID)
	if err != nil {
		return a, err
	}
	if account == nil || account.RedirectTo != nil || account.UUID != account.CanonicalUUID {
		a.Reason = "native_account_missing_or_merged"
		return a, nil
	}
	a.AccountRevision = account.Revision
	if a.IdentityUUID != "" && identities[a.IdentityUUID].Action == "review" {
		a.Reason = "unresolved_catalog_performer"
		return a, nil
	}
	// A migration-conflict placeholder is not an explicit user unlink.
	if row.IdentityUUID == "" && row.Source == "migration_conflict" {
		a.Reason = "legacy_ownership_conflict"
		return a, nil
	}
	current, err := store.Ownership(ctx, account.UUID)
	if err != nil {
		return a, err
	}
	if current != nil {
		a.DecisionUUID = current.UUID
		matches := current.State == models.AccountOwnershipUnlinked && a.IdentityUUID == ""
		if current.State == models.AccountOwnershipLinked && a.IdentityUUID != "" && current.PerformerUUID != nil {
			owner, err := (&ArchiveEntityStore{}).Resolve(ctx, *current.PerformerUUID)
			if err != nil {
				return a, err
			}
			matches = owner != nil && owner.UUID == identities[a.IdentityUUID].CurrentUUID
		}
		if matches {
			a.Action, a.Reason = "mapped", "same_native_ownership_choice"
			return a, nil
		}
		// Even a deliberate return to undecided is a later native choice.
		a.Reason = "native_ownership_already_decided"
		return a, nil
	}
	a.Action, a.Reason = "linked", "explicit_saved_account_association"
	if a.IdentityUUID == "" {
		a.Action, a.Reason = "unlinked", "explicit_saved_account_unlink"
	}
	return a, nil
}

func (s *CatalogIdentityImportStore) Apply(ctx context.Context, input models.CatalogIdentityImportInput, expected string, now time.Time) (*models.CatalogIdentityImport, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !archive.ValidSHA256(expected) || !validJobTime(now) {
		return nil, models.ErrCatalogIdentityImportInvalid
	}
	p, err := scrape.PrepareCatalogIdentities(input)
	if err != nil {
		return nil, err
	}
	if prior, err := s.Find(ctx, input.UUID); err != nil {
		return nil, err
	} else if prior != nil {
		if prior.InputSHA256 != p.Plan.InputSHA256 || prior.PlanSHA256 != expected {
			return nil, models.ErrCatalogIdentityImportConflict
		}
		return prior, nil
	}
	plan, err := s.preview(ctx, input, p, now)
	if err != nil {
		return nil, err
	}
	if plan.PlanSHA256 != expected {
		return nil, models.ErrCatalogIdentityImportConflict
	}
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return errors.New("catalog identity import did not finish atomically")
		}
		return nil
	})
	entities := &ArchiveEntityStore{}
	for _, a := range plan.Identities {
		if a.Action == "adopt" {
			if _, err := entities.AdoptUUID(ctx, a.CurrentUUID, a.IdentityUUID, a.CurrentRevision); err != nil {
				return nil, err
			}
		}
	}
	for _, a := range plan.Identities {
		if a.Action != "redirect" {
			continue
		}
		target, err := entities.Resolve(ctx, a.RedirectTo)
		if err != nil {
			return nil, err
		}
		if target == nil || target.Kind != models.ArchivePerformer || target.State != models.ArchiveEntityActive {
			return nil, models.ErrCatalogIdentityImportConflict
		}
		row := p.Identities[a.IdentityUUID]
		created, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
		retired, _ := time.Parse(time.RFC3339Nano, row.UpdatedAt)
		// This records a merge that already happened. It never deletes or
		// recreates a local performer and has no guessed historical integer ID.
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO archive_entities(uuid,kind,state,redirect_to,created_at,retired_at)
VALUES(?,'performer','redirected',?,?,?)`, a.IdentityUUID, target.UUID, Timestamp{Timestamp: created}, Timestamp{Timestamp: retired}); err != nil {
			return nil, err
		}
	}
	accounts := &SourceAccountStore{}
	applied := map[string]bool{}
	for _, a := range plan.Ownership {
		if (a.Action != "linked" && a.Action != "unlinked") || applied[a.AccountUUID] {
			continue
		}
		choice := models.AccountOwnershipInput{AccountUUID: a.AccountUUID, ExpectedAccountRevision: a.AccountRevision, State: models.AccountOwnershipState(a.Action), Origin: "migration", Reason: "Imported explicit catalog account choice"}
		if a.IdentityUUID != "" {
			owner, err := entities.Resolve(ctx, a.IdentityUUID)
			if err != nil {
				return nil, err
			}
			if owner == nil || owner.State != models.ArchiveEntityActive {
				return nil, models.ErrCatalogIdentityImportConflict
			}
			choice.PerformerUUID, choice.ExpectedPerformerRevision = owner.UUID, owner.Revision
		}
		if _, err := accounts.DecideOwnership(ctx, choice); err != nil {
			return nil, err
		}
		applied[a.AccountUUID] = true
	}
	body, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_identity_imports(uuid,source_uuid,namespace,input_sha256,plan_sha256,record_count,plan,created_at)
VALUES(?,?,?,?,?,?,?,?)`, plan.UUID, plan.SourceUUID, plan.Namespace, plan.InputSHA256, plan.PlanSHA256, plan.RecordCount, string(body), now.UTC().Format(time.RFC3339Nano)); err != nil {
		return nil, err
	}
	for i, record := range plan.Records {
		record.Evidence = p.Records[i].Evidence
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_identity_import_records(import_uuid,source_table,source_key,outcome,reason,archive_uuid,account_uuid,evidence)
VALUES(?,?,?,?,?,?,?,?)`, plan.UUID, record.Table, record.SourceKey, record.Outcome, record.Reason, record.ArchiveUUID, record.AccountUUID, string(record.Evidence)); err != nil {
			return nil, err
		}
	}
	complete = true
	return &models.CatalogIdentityImport{CatalogIdentityImportPlan: *plan, CreatedAt: now.UTC()}, nil
}

func catalogIdentityRecordOutcome(ctx context.Context, r *models.CatalogIdentityImportRecord, p *scrape.PreparedCatalogIdentities, plan *models.CatalogIdentityImportPlan) error {
	var row map[string]any
	if err := json.Unmarshal(r.Evidence, &row); err != nil {
		return err
	}
	var id string
	switch r.Table {
	case "performer_identities":
		id, _ = row["id"].(string)
	case "performer_identity_bindings":
		if row["namespace"] != plan.Namespace {
			r.Outcome, r.Reason = "copied", "external_namespace_binding"
			return nil
		}
		if row["redirect_to"] != nil {
			r.Reason = "historical_local_redirect"
			local, ok := scrape.CatalogLocalPerformerID(row["performer_id"].(string))
			if !ok {
				r.Outcome, r.Reason = "review", "invalid_historical_local_id"
				return nil
			}
			current, err := (&ArchiveEntityStore{}).FindByLocalID(ctx, models.ArchivePerformer, local)
			if err != nil {
				return err
			}
			if current != nil {
				r.Outcome, r.Reason = "review", "historical_local_id_is_live"
				return nil
			}
		}
		id, _ = row["identity_id"].(string)
		id, _ = p.Resolve(id)
	case "performer_account_associations":
		for _, a := range plan.Ownership {
			if a.AccountKey == row["account_key"] {
				r.Outcome, r.Reason = a.Action, a.Reason
				if a.Action == "review" {
					return nil
				}
				r.Outcome = "mapped"
				r.AccountUUID = &a.AccountUUID
				id = a.IdentityUUID
				break
			}
		}
	case "catalog_metadata_accounts", "catalog_metadata_performers":
		r.Outcome, r.Reason = "review", "legacy_plugin_binding_requires_reconciliation"
		if p.LegacyMigrated {
			r.Outcome, r.Reason = "superseded", "catalog_metadata_links_v1_already_migrated"
		}
	}
	if id != "" {
		for _, a := range plan.Identities {
			if a.IdentityUUID == id {
				if a.Action == "review" {
					r.Outcome, r.Reason = "review", a.Reason
				} else {
					r.ArchiveUUID = &a.IdentityUUID
					r.Outcome = "mapped"
				}
				return nil
			}
		}
	}
	return nil
}

func (s *CatalogIdentityImportStore) Records(ctx context.Context, id string, after int64, limit int) ([]models.CatalogIdentityImportRecord, error) {
	if !validSourceRunUUID(id) || after < 0 || limit < 1 || limit > 100 {
		return nil, models.ErrCatalogIdentityImportInvalid
	}
	var rows []struct {
		models.CatalogIdentityImportRecord
		Body string `db:"evidence"`
	}
	if err := dbWrapper.Select(ctx, &rows, `SELECT id,source_table,source_key,outcome,reason,archive_uuid,account_uuid,evidence FROM catalog_identity_import_records WHERE import_uuid=? AND id>? ORDER BY id LIMIT ?`, id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.CatalogIdentityImportRecord, 0, len(rows))
	for _, row := range rows {
		row.Evidence = json.RawMessage(row.Body)
		ret = append(ret, row.CatalogIdentityImportRecord)
	}
	return ret, nil
}
