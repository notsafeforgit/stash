package sqlite

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

const catalogAssociationPolicy = "catalog-association-v1"
const catalogAssociationLimit = 100

func (s *CapturePublisherStore) CatalogAssociationPosts(ctx context.Context, after string, limit int) ([]string, error) {
	after, limit, err := sourceDefinitionPage(after, limit)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	err = dbWrapper.Select(ctx, &ids, `SELECT post_uuid FROM (
 SELECT post_uuid FROM (SELECT post_uuid FROM source_post_account_claims
 WHERE post_uuid>? AND origin='migration' AND basis='catalog-posts' GROUP BY post_uuid ORDER BY post_uuid LIMIT ?)
 UNION SELECT post_uuid FROM (SELECT post_uuid FROM source_collection_post_evidence
 WHERE post_uuid>? AND origin='migration' AND basis='catalog-membership' GROUP BY post_uuid ORDER BY post_uuid LIMIT ?)
) ORDER BY post_uuid LIMIT ?`, after, limit, after, limit, limit)
	return ids, err
}

// These bounded queries use the post membership/claim indexes. They do not
// inspect the catalog snapshots or walk the media library for each post.
func catalogAssociationEvidence(ctx context.Context, post string, ret *models.CatalogAssociationPreview) error {
	var claims []struct {
		UUID      string `db:"uuid"`
		Account   string `db:"account"`
		Namespace string `db:"namespace"`
		Label     string `db:"label"`
	}
	if err := dbWrapper.Select(ctx, &claims, `SELECT p.uuid,a.canonical_uuid AS account,a.namespace,a.label
 FROM source_post_identities i CROSS JOIN source_post_account_claims p ON p.post_uuid=i.post_uuid
 JOIN source_accounts a ON a.uuid=p.account_uuid
 WHERE i.canonical_uuid=? AND p.origin='migration' AND p.basis='catalog-posts'
 ORDER BY p.uuid LIMIT ?`, post, catalogAssociationLimit+1); err != nil {
		return err
	}
	if len(claims) > catalogAssociationLimit {
		ret.Reason = "too_many_imported_claims"
		return nil
	}
	for _, claim := range claims {
		// The old catalog writer also called the fixed Reddit saved-feed
		// directory a creator. A valid retained claim is not enough to turn
		// that known non-author folder into a publisher.
		if claim.Namespace == "native:reddit" && strings.HasSuffix(strings.ToLower(claim.Label), " saved") {
			ret.Reason = "non_author_directory"
			return nil
		}
		ret.Candidates = append(ret.Candidates, claim.Account)
		ret.Evidence = append(ret.Evidence, "catalog-post:"+claim.UUID)
	}
	var folders []struct {
		UUID  string `db:"uuid"`
		Label string `db:"label"`
	}
	if err := dbWrapper.Select(ctx, &folders, `SELECT e.uuid,d.label
 FROM source_post_identities i CROSS JOIN source_collection_post_evidence e ON e.post_uuid=i.post_uuid
 JOIN source_collection_revisions d ON d.collection_uuid=e.collection_uuid AND d.revision=e.collection_revision
 WHERE i.canonical_uuid=? AND e.origin='migration' AND e.basis='catalog-membership' AND d.kind='directory'
 ORDER BY e.uuid LIMIT ?`, post, catalogAssociationLimit+1); err != nil {
		return err
	}
	if len(folders) > catalogAssociationLimit {
		ret.Reason = "too_many_imported_folders"
		return nil
	}
	for _, folder := range folders {
		refs := archive.CatalogDirectoryAccounts(folder.Label)
		if len(refs) == 0 {
			var namespaces []string
			if err := dbWrapper.Select(ctx, &namespaces, `SELECT DISTINCT p.namespace FROM source_post_identities i
 CROSS JOIN source_post_identifiers p ON p.post_uuid=i.post_uuid WHERE i.canonical_uuid=? LIMIT 101`, post); err != nil {
				return err
			}
			if len(namespaces) > catalogAssociationLimit {
				ret.Reason = "too_many_post_identifiers"
				return nil
			}
			for _, namespace := range namespaces {
				if ref := archive.CatalogMirrorDirectoryAccount(folder.Label, namespace); ref != nil {
					refs = append(refs, *ref)
				}
			}
			if len(refs) == 0 {
				continue
			}
		}
		// A folder from another service may describe a download/conversion's
		// location, not this post's publisher.
		allowed, err := publisherNamespaceAllowed(ctx, post, refs[0].Namespace)
		if err != nil {
			return err
		}
		if !allowed {
			continue
		}
		var matched []string
		for _, ref := range refs {
			if ref.Kind == "catalog_label" {
				var ids []string
				if err := dbWrapper.Select(ctx, &ids, `SELECT DISTINCT canonical_uuid FROM source_accounts
 WHERE namespace=? AND label=? COLLATE NOCASE ORDER BY canonical_uuid LIMIT 101`, ref.Namespace, ref.Value); err != nil {
					return err
				}
				if len(ids) > catalogAssociationLimit {
					ret.Reason = "too_many_account_matches"
					return nil
				}
				matched = append(matched, ids...)
				continue
			}
			accounts, err := (&SourceAccountStore{}).Lookup(ctx, ref, "", catalogAssociationLimit+1)
			if err != nil {
				return err
			}
			if len(accounts) > catalogAssociationLimit {
				ret.Reason = "too_many_account_matches"
				return nil
			}
			for _, account := range accounts {
				matched = append(matched, account.UUID)
			}
		}
		slices.Sort(matched)
		matched = slices.Compact(matched)
		if len(matched) == 0 {
			ret.Reason = "folder_account_unresolved"
			continue
		}
		// Even a matching handle must not override a different numeric ID.
		identity := &models.CapturedAccount{Namespace: refs[0].Namespace}
		for _, ref := range refs {
			identity.Identifiers = append(identity.Identifiers, models.CapturedAccountIdentifier{Reference: ref})
		}
		for _, account := range matched {
			conflict, err := publisherIdentifierConflict(ctx, account, identity)
			if err != nil {
				return err
			}
			if conflict {
				ret.Reason = "folder_account_conflict"
				return nil
			}
		}
		ret.Candidates = append(ret.Candidates, matched...)
		ret.Evidence = append(ret.Evidence, "directory:"+folder.UUID+":"+folder.Label)
	}
	slices.Sort(ret.Candidates)
	ret.Candidates = slices.Compact(ret.Candidates)
	return nil
}

func (s *CapturePublisherStore) PreviewCatalogAssociation(ctx context.Context, value string) (*models.CatalogAssociationPreview, error) {
	post, err := (&SourceEvidenceStore{}).PostIdentity(ctx, value)
	if err != nil || post == nil {
		if err == nil {
			err = models.ErrSourcePostConflict
		}
		return nil, err
	}
	ret := &models.CatalogAssociationPreview{PostUUID: post.CanonicalUUID, Action: "review", Evidence: []string{}, Candidates: []string{}}
	if post.CanonicalUUID != post.UUID {
		post, err = (&SourceEvidenceStore{}).PostIdentity(ctx, post.CanonicalUUID)
		if err != nil || post == nil {
			return nil, models.ErrSourcePostConflict
		}
	}
	if post.State != "active" {
		ret.Action, ret.Reason = "preserved", "post_forgotten"
		return ret, nil
	}
	if err := catalogAssociationEvidence(ctx, post.CanonicalUUID, ret); err != nil {
		return nil, err
	}
	if ret.Reason != "" {
		return ret, nil
	}
	if len(ret.Candidates) != 1 {
		ret.Reason = "conflicting_imported_accounts"
		if len(ret.Candidates) == 0 {
			ret.Action, ret.Reason = "unavailable", "no_imported_account"
		}
		return ret, nil
	}
	ret.AccountUUID = ret.Candidates[0]
	var captures []string
	if err := dbWrapper.Select(ctx, &captures, `SELECT c.uuid FROM source_post_identities i
 CROSS JOIN source_captures c INDEXED BY source_captures_scope ON c.post_uuid=i.post_uuid
 WHERE i.canonical_uuid=? ORDER BY c.uuid LIMIT ?`, post.CanonicalUUID, catalogAssociationLimit+1); err != nil {
		return nil, err
	}
	if len(captures) == 0 || len(captures) > catalogAssociationLimit {
		ret.Reason = "capture_scope_requires_review"
		return ret, nil
	}
	// An explicit decision anywhere on a merged post takes precedence. A
	// second imported capture must not resurrect an explicitly unlinked author.
	selected := false
	for _, id := range captures {
		current, err := s.Current(ctx, id)
		if err != nil {
			return nil, err
		}
		if current == nil {
			continue
		}
		if current.State != "linked" || current.CanonicalAccountUUID == nil || *current.CanonicalAccountUUID != ret.AccountUUID {
			ret.Action, ret.Reason = "preserved", "existing_publisher_choice"
			return ret, nil
		}
		selected = true
	}
	if selected {
		ret.Action, ret.Reason = "preserved", "already_linked"
		return ret, nil
	}
	for _, id := range captures {
		preview, err := s.Preview(ctx, id, ret.AccountUUID)
		if err != nil {
			return nil, err
		}
		if preview.IdentityError != "" || preview.CandidatesTruncated || slices.Contains(preview.Conflicts, "target_namespace_mismatch") ||
			slices.Contains(preview.Conflicts, "target_stable_id_conflict") || slices.Contains(preview.Conflicts, "captured_namespace_mismatch") {
			ret.Reason = "captured_author_conflict"
			return ret, nil
		}
		if preview.Identity != nil {
			if preview.Identity.Namespace != preview.Target.Namespace || (preview.AccountUUID != nil && *preview.AccountUUID != ret.AccountUUID) {
				ret.Reason = "captured_author_conflict"
				return ret, nil
			}
			matched := false
			for _, candidate := range preview.Candidates {
				if candidate.Account.UUID != ret.AccountUUID {
					ret.Reason = "captured_author_conflict"
					return ret, nil
				}
				matched = true
			}
			if !matched {
				ret.Reason = "captured_author_conflict"
				return ret, nil
			}
		} else {
			// The normal automatic policy requires an author ID. A handle-only
			// author can still contradict a folder, so do not treat it as absent.
			capture, err := (&SourceEvidenceStore{}).FindCapture(ctx, id)
			if err != nil {
				return nil, err
			}
			raw, err := archive.RestoreCapture(capture.Payload)
			if err != nil {
				return nil, err
			}
			identity, err := archive.CapturedAccountReferences(raw)
			if err != nil {
				ret.Reason = "captured_author_conflict"
				return ret, nil
			}
			if identity != nil {
				matched := false
				for _, claim := range identity.Identifiers {
					accounts, err := (&SourceAccountStore{}).Lookup(ctx, claim.Reference, "", 2)
					if err != nil {
						return nil, err
					}
					if len(accounts) != 1 || accounts[0].UUID != ret.AccountUUID {
						ret.Reason = "captured_author_conflict"
						return ret, nil
					}
					matched = true
				}
				if !matched {
					ret.Reason = "captured_author_conflict"
					return ret, nil
				}
			}
		}
		if ret.CaptureUUID == "" {
			ret.CaptureUUID, ret.Signature = id, preview.Signature
		}
	}
	ret.Action = "link"
	return ret, nil
}

// BackfillCatalogAssociation is idempotent by current choices and uses ordinary
// publisher/ownership history. No auxiliary database or rewritten import receipt
// is required. Calls run in the application transaction, with all guards reread.
func (s *CapturePublisherStore) BackfillCatalogAssociation(ctx context.Context, post string, linkOwner bool) (*models.CatalogAssociationResult, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	preview, err := s.PreviewCatalogAssociation(ctx, post)
	if err != nil {
		return nil, err
	}
	ret := &models.CatalogAssociationResult{CatalogAssociationPreview: *preview}
	complete := snapshotAtomicWrite(ctx)
	if preview.Action == "link" {
		reason := catalogAssociationPolicy + ": " + strings.Join(preview.Evidence, "; ")
		if len(reason) > 4096 {
			ret.Action, ret.Reason = "review", "too_much_imported_evidence"
			*complete = true
			return ret, nil
		}
		decision, err := s.Apply(ctx, models.CapturePublisherInput{
			UUID:        scrape.RegistryImportUUID(preview.PostUUID, catalogAssociationPolicy, preview.CaptureUUID+":"+preview.AccountUUID),
			CaptureUUID: preview.CaptureUUID, AccountUUID: preview.AccountUUID, ExpectedSignature: preview.Signature,
			Action: "link", Origin: "migration", Reason: reason,
		})
		if err != nil {
			return nil, err
		}
		ret.Action, ret.DecisionUUID = "linked", decision.UUID
	}
	if linkOwner && (ret.Action == "linked" || ret.Reason == "already_linked") {
		if err := catalogAssociationOwner(ctx, ret); err != nil {
			return nil, err
		}
	}
	*complete = true
	return ret, nil
}

func catalogAssociationOwner(ctx context.Context, ret *models.CatalogAssociationResult) error {
	accounts := &SourceAccountStore{}
	current, err := accounts.Ownership(ctx, ret.AccountUUID)
	if err != nil {
		return err
	}
	if current != nil {
		ret.Ownership = "preserved"
		return nil
	}
	account, err := accounts.Resolve(ctx, ret.AccountUUID)
	if err != nil {
		return err
	}
	identifiers, err := accounts.Identifiers(ctx, account.UUID, "", catalogAssociationLimit+1)
	if err != nil {
		return err
	}
	if len(identifiers) > catalogAssociationLimit {
		ret.Ownership = "too_many_identifiers"
		return nil
	}
	names := []string{account.Label}
	for _, identifier := range identifiers {
		if identifier.Reference.Kind == "handle" && identifier.Reference.Namespace == account.Namespace {
			names = append(names, identifier.Reference.Value)
		}
	}
	slices.Sort(names)
	names = slices.Compact(names)
	var candidates []string
	for _, name := range names {
		if name == "" {
			continue
		}
		var matched []string
		if err := dbWrapper.Select(ctx, &matched, `SELECT DISTINCT e.uuid FROM performer_names n
 JOIN archive_entities e ON e.performer_id=n.performer_id AND e.kind='performer' AND e.state='active'
 WHERE n.name=? COLLATE NOCASE ORDER BY e.uuid LIMIT 2`, name); err != nil {
			return err
		}
		candidates = append(candidates, matched...)
	}
	slices.Sort(candidates)
	candidates = slices.Compact(candidates)
	ret.PerformerCandidates = candidates
	if len(candidates) != 1 {
		ret.Ownership = "no_matching_performer"
		if len(candidates) > 1 {
			ret.Ownership = "ambiguous_performers"
		}
		return nil
	}
	performer, err := (&ArchiveEntityStore{}).Find(ctx, candidates[0])
	if err != nil {
		return err
	}
	_, err = accounts.DecideOwnership(ctx, models.AccountOwnershipInput{
		AccountUUID: account.UUID, ExpectedAccountRevision: account.Revision, State: models.AccountOwnershipLinked,
		PerformerUUID: performer.UUID, ExpectedPerformerRevision: performer.Revision, Origin: "migration",
		Reason: fmt.Sprintf("%s: unique exact performer name or alias matching imported account %s", catalogAssociationPolicy, account.Label),
	})
	if err == nil {
		ret.Ownership, ret.PerformerUUID = "linked", performer.UUID
	}
	return err
}
