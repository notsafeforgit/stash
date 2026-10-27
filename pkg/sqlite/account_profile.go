package sqlite

import (
	"context"
	"encoding/json"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sourceadapter"
)

func (s *SourceAccountStore) RecordProfile(ctx context.Context, input models.AccountProfileInput) (*models.AccountProfileResult, error) {
	profile, err := sourceadapter.ExtractAccountProfile(input.Source)
	if err != nil {
		return nil, err
	}
	result := &models.AccountProfileResult{Status: "recorded", ProfileUUID: input.UUID}
	matches, err := s.Lookup(ctx, profile.ID, "", 2)
	if err != nil {
		return nil, err
	}
	var account *models.SourceAccount
	switch len(matches) {
	case 0:
		locators, err := s.Lookup(ctx, profile.Handle, "", 1)
		if err != nil {
			return nil, err
		}
		if len(locators) > 0 {
			result.Review = "locator_only_candidates"
		} else {
			account, err = s.Create(ctx, profile.ID.Namespace, profile.Handle.Value)
			if err != nil {
				return nil, err
			}
		}
	case 1:
		account = matches[0]
		conflict, err := publisherIdentifierConflict(ctx, account.UUID, &models.CapturedAccount{
			Namespace: profile.ID.Namespace, Identifiers: []models.CapturedAccountIdentifier{{Reference: profile.ID}}})
		if err != nil {
			return nil, err
		}
		if conflict {
			account = nil
			result.Review = "stable_id_conflict"
		}
	default:
		result.Review = "ambiguous_stable_id"
	}
	var accountID *string
	if account != nil {
		accountID = &account.UUID
		result.AccountUUID = account.UUID
		details, err := json.Marshal(map[string]string{"profile_uuid": input.UUID})
		if err != nil {
			return nil, err
		}
		for _, ref := range []models.AccountReference{profile.ID, profile.Handle} {
			_, err = s.ObserveIdentifier(ctx, account.UUID, ref, models.AccountIdentifierEvidence{
				Key: "profile:" + input.UUID, Basis: "captured-profile", Origin: "gallery-dl", Details: details,
				FirstObserved: input.ObservedAt, LastObserved: input.ObservedAt})
			if err != nil {
				return nil, err
			}
		}
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO source_account_profiles(uuid,producer_uuid,event_uuid,collection_uuid,collection_revision,account_uuid,observed_at,extractor_version,source,review)
VALUES(?,?,?,?,?,?,?,?,?,?)`, input.UUID, input.ProducerUUID, input.EventUUID, input.CollectionUUID, input.CollectionRevision, accountID, input.ObservedAt, input.ExtractorVersion, string(input.Source), result.Review)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return result, nil
	}
	for _, value := range profile.URLs {
		_, err = dbWrapper.Exec(ctx, `INSERT INTO account_observed_profile_urls(account_uuid,url_key,url,profile_uuid) VALUES(?,?,?,?)
ON CONFLICT(account_uuid,url_key) DO UPDATE SET url=excluded.url,profile_uuid=excluded.profile_uuid`, account.UUID, sourceadapter.ProfileURLKey(value), value, input.UUID)
		if err != nil {
			return nil, err
		}
	}
	if err = syncAccountProfileURLs(ctx, account.UUID); err != nil {
		return nil, err
	}
	return result, nil
}
