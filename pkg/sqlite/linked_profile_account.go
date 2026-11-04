package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func redgifsProfileReference(target string) *models.AccountReference {
	ref := archive.ProfileReference(target)
	if ref == nil || ref.Namespace != "native:redgifs" || ref.Kind != "handle" {
		return nil
	}
	return ref
}

// These are indexed exact URL lookups, not guesses from performer names. The
// captured uploader has no immutable account ID; record that fact as a handle.
func redgifsProfileOwner(ctx context.Context, target string) (*models.ArchiveEntity, error) {
	ref := redgifsProfileReference(target)
	if ref == nil {
		return nil, nil
	}
	urls := []string{}
	for _, prefix := range []string{"https://www.", "https://", "http://www.", "http://"} {
		value := prefix + "redgifs.com/users/" + ref.Value
		urls = append(urls, value, value+"/")
	}
	args := make([]any, len(urls))
	for i := range urls {
		args[i] = urls[i]
	}
	var ids []string
	if err := dbWrapper.Select(ctx, &ids, `SELECT DISTINCT e.uuid FROM performer_urls u
 JOIN archive_entities e ON e.performer_id=u.performer_id AND e.state='active'
 WHERE u.url IN `+getInBinding(len(args))+` ORDER BY e.uuid LIMIT 2`, args...); err != nil {
		return nil, err
	}
	if len(ids) != 1 {
		return nil, nil
	}
	return (&ArchiveEntityStore{}).Find(ctx, ids[0])
}

func redgifsProfileAccount(ctx context.Context, collection, target string, observed time.Time) (*models.SourceAccount, error) {
	ref := redgifsProfileReference(target)
	if ref == nil {
		return nil, models.ErrSourceDefinitionInvalid
	}
	accounts := &SourceAccountStore{}
	matches, err := accounts.Lookup(ctx, *ref, "", 2)
	if err != nil || len(matches) > 1 {
		return nil, err // Ambiguous/reused handles remain for review.
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	account, err := accounts.Create(ctx, ref.Namespace, ref.Value)
	if err != nil {
		return nil, err
	}
	details, err := json.Marshal(map[string]string{"collection_uuid": collection, "profile_url": target})
	if err != nil {
		return nil, err
	}
	_, err = accounts.ObserveIdentifier(ctx, account.UUID, *ref, models.AccountIdentifierEvidence{
		Key: "redgifs-profile:" + collection, Basis: "profile-url", Origin: "ingest", Details: details,
		FirstObserved: observed, LastObserved: observed,
	})
	if err != nil {
		return nil, err
	}
	return accounts.Find(ctx, account.UUID)
}

func linkRedgifsProfileOwner(ctx context.Context, account *models.SourceAccount, target string, performer *models.ArchiveEntity) (string, error) {
	accounts := &SourceAccountStore{}
	owner, err := accounts.Ownership(ctx, account.UUID)
	if err != nil {
		return "", err
	}
	if owner != nil && owner.State != models.AccountOwnershipUndecided {
		if owner.State == models.AccountOwnershipLinked && owner.PerformerUUID != nil && *owner.PerformerUUID == performer.UUID {
			return "linked", nil
		}
		return "ownership_choice", nil // Includes an explicit unlink.
	}
	_, err = accounts.DecideOwnership(ctx, models.AccountOwnershipInput{AccountUUID: account.UUID,
		ExpectedAccountRevision: account.Revision, State: models.AccountOwnershipLinked,
		PerformerUUID: performer.UUID, ExpectedPerformerRevision: performer.Revision,
		Origin: "profile-url", Reason: "Unique existing performer profile URL " + target + "; Redgifs handle, not an immutable service ID",
	})
	return "linked", err
}

// After a Reddit owner is selected, the captured related URL becomes performer
// metadata. Link only already observed Redgifs accounts and honor earlier choices.
func syncRedgifsProfileOwners(ctx context.Context, performer *models.ArchiveEntity, urls []string) error {
	for _, target := range urls {
		ref := redgifsProfileReference(target)
		if ref == nil {
			continue
		}
		owner, err := redgifsProfileOwner(ctx, target)
		if err != nil {
			return err
		}
		if owner == nil || owner.UUID != performer.UUID {
			continue
		}
		matches, err := (&SourceAccountStore{}).Lookup(ctx, *ref, "", 2)
		if err != nil {
			return err
		}
		if len(matches) == 1 {
			if _, err := linkRedgifsProfileOwner(ctx, matches[0], target, owner); err != nil {
				return err
			}
		}
	}
	return nil
}

// Repair older profile collections through a scoped application operation. New
// discoveries bind their account in revision one. Older scopes can be repaired
// only when no source or metadata job is unfinished; completed history remains.
func (s *SourceCollectionStore) LinkProfileAccount(ctx context.Context, id string, input models.ProfileAccountLink) (*models.ProfileAccountLinkResult, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if input.ExpectedRevision < 1 || !validSourceRunUUID(input.PerformerUUID) {
		return nil, models.ErrSourceDefinitionInvalid
	}
	collection, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if collection == nil || collection.State != "active" || collection.Kind != "account" || collection.Namespace != "native:redgifs" ||
		collection.CanonicalUUID != "" || redgifsProfileReference(collection.TargetURL) == nil {
		return nil, models.ErrSourceDefinitionInvalid
	}
	result := &models.ProfileAccountLinkResult{State: "review", Collection: collection}
	performer, err := redgifsProfileOwner(ctx, collection.TargetURL)
	if err != nil {
		return nil, err
	}
	if performer == nil || performer.UUID != input.PerformerUUID {
		result.Reason = "profile_url_not_unique"
		return result, nil
	}
	if collection.Revision != input.ExpectedRevision && (collection.Revision != input.ExpectedRevision+1 || collection.AccountUUID == nil) {
		return nil, models.ErrSourceDefinitionConflict
	}
	var policy *models.MetadataPolicy
	if collection.AccountUUID == nil {
		var busy bool
		if err := dbWrapper.Get(ctx, &busy, `SELECT
 EXISTS(SELECT 1 FROM source_runs WHERE collection_uuid=? AND state IN ('queued','running','deferred'))
 OR EXISTS(SELECT 1 FROM archive_jobs WHERE state IN ('queued','running')
 AND (json_extract(arguments,'$.collection_uuid')=?
 OR (kind='media.verify' AND json_extract(arguments,'$.publication.collection_uuid')=?)))`,
			collection.UUID, collection.UUID, collection.UUID); err != nil {
			return nil, err
		}
		if busy {
			result.State, result.Reason = "waiting", "unfinished_collection_work"
			return result, nil
		}
		policy, err = (&MetadataPolicyStore{}).Find(ctx, collection.UUID)
		if err != nil {
			return nil, err
		}
		if policy != nil && policy.CollectionRevision != collection.Revision {
			result.Reason = "metadata_policy_scope_changed"
			return result, nil
		}
		// Translation targets may still carry the old collection revision even
		// without a running job. This narrow repair does not migrate those scopes.
		translation, err := (&TranslationPolicyStore{}).Find(ctx, collection.UUID)
		if err != nil {
			return nil, err
		}
		if translation != nil {
			result.Reason = "translation_policy_scope"
			return result, nil
		}
	}
	var account *models.SourceAccount
	if collection.AccountUUID == nil {
		account, err = redgifsProfileAccount(ctx, collection.UUID, collection.TargetURL, collection.CreatedAt)
	} else {
		var matches []*models.SourceAccount
		matches, err = (&SourceAccountStore{}).Lookup(ctx, *redgifsProfileReference(collection.TargetURL), "", 2)
		if len(matches) == 1 {
			account = matches[0]
		}
	}
	if err != nil {
		return nil, err
	}
	if account == nil {
		result.Reason = "ambiguous_profile_account"
		return result, nil
	}
	if collection.AccountUUID != nil && *collection.AccountUUID != account.UUID {
		result.Reason = "different_collection_account"
		return result, nil
	}
	state, err := linkRedgifsProfileOwner(ctx, account, collection.TargetURL, performer)
	if err != nil {
		return nil, err
	}
	if state != "linked" {
		result.Reason = state
		return result, nil
	}
	if collection.AccountUUID == nil {
		definition := collection.SourceCollectionDefinition
		definition.AccountUUID = &account.UUID
		collection, err = s.Put(ctx, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision,
			SourceCollectionDefinition: definition, Origin: "review", Reason: "Bind the observed Redgifs profile account; preserve target, directory and monitoring"})
		if err != nil {
			return nil, err
		}
		if policy != nil {
			_, err = (&MetadataPolicyStore{}).Put(ctx, models.MetadataPolicyInput{CollectionUUID: collection.UUID,
				ExpectedCollectionRevision: collection.Revision, ExpectedRevision: policy.Revision, Definition: policy.Definition,
				Origin: "review", Reason: "Preserve metadata rules after linking the same Redgifs profile account"})
			if err != nil {
				return nil, err
			}
		}
	}
	result.State, result.Collection = "linked", collection
	return result, nil
}
