package sqlite

import (
	"context"
	"slices"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sourceadapter"
)

// Called only after a capture has an established publisher. Profile links are
// metadata for that account's owner, never evidence to assign depicted people
// or to establish ownership of another account mentioned in the bio.
func recordAccountProfileURLs(ctx context.Context, account, captureID string) error {
	capture, err := (&SourceEvidenceStore{}).FindCapture(ctx, captureID)
	if err != nil {
		return err
	}
	if capture == nil {
		return models.ErrCapturePublisherConflict
	}
	observation, err := publisherObservationCapture(ctx, capture)
	if err != nil {
		return err
	}
	if observation.Origin != "gallery-dl" && observation.Origin != "gallery-dl-enrichment" {
		return nil
	}
	raw, err := archive.RestoreCapture(capture.Payload)
	if err != nil {
		return err
	}
	urls, err := archive.ExtractCapturedProfileURLs(raw)
	if err != nil {
		// Invalid author evidence is already handled by publisher review. An
		// explicit link must not turn malformed profile fields into metadata.
		return nil
	}
	linked, err := recordLinkedRedgifsProfile(ctx, captureID, raw)
	if err != nil {
		return err
	}
	if linked != "" {
		urls = append(urls, linked)
	}
	for _, value := range urls {
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO account_profile_urls(account_uuid,url_key,url,capture_uuid) VALUES(?,?,?,?)
ON CONFLICT(account_uuid,url_key) DO UPDATE SET url=excluded.url,capture_uuid=excluded.capture_uuid`,
			account, archive.ProfileURLKey(value), value, captureID); err != nil {
			return err
		}
	}
	if len(urls) == 0 {
		return nil
	}
	return syncAccountProfileURLs(ctx, account)
}

type accountProfileURLRow struct {
	AccountUUID string `db:"account_uuid"`
	Key         string `db:"url_key"`
	URL         string `db:"url"`
}

// Also runs when an owner is linked after capture or accounts are consolidated.
// All queries are scoped to that account/performer; no library scan is needed.
func syncAccountProfileURLs(ctx context.Context, accountID string) error {
	accounts := &SourceAccountStore{}
	account, err := accounts.Resolve(ctx, accountID)
	if err != nil || account == nil {
		return err
	}
	owner, err := accounts.Ownership(ctx, account.UUID)
	if err != nil || owner == nil || owner.State != models.AccountOwnershipLinked {
		return err
	}
	performer, err := (&ArchiveEntityStore{}).Resolve(ctx, *owner.PerformerUUID)
	if err != nil || performer == nil || performer.State != models.ArchiveEntityActive || performer.LocalID == nil {
		return err
	}
	existing, err := performersURLsTableMgr.get(ctx, *performer.LocalID)
	if err != nil {
		return err
	}
	seen := profileURLKeys(existing)
	var suppressed []string
	if err := dbWrapper.Select(ctx, &suppressed, "SELECT url FROM performer_profile_url_suppressions WHERE performer_uuid=?", performer.UUID); err != nil {
		return err
	}
	for _, value := range suppressed {
		seen[performerProfileURLKey(value)] = true
	}
	var added []string
	afterAccount, afterKey := "", ""
	for {
		var rows []accountProfileURLRow
		if err := dbWrapper.Select(ctx, &rows, `SELECT u.account_uuid,u.url_key,u.url FROM source_accounts a
CROSS JOIN account_profile_urls u ON u.account_uuid=a.uuid
CROSS JOIN capture_publisher_heads h ON h.capture_uuid=u.capture_uuid
CROSS JOIN capture_publisher_decisions d ON d.uuid=h.decision_uuid AND d.state='linked'
CROSS JOIN source_accounts p ON p.uuid=d.account_uuid
CROSS JOIN source_captures c ON c.uuid=u.capture_uuid
CROSS JOIN source_post_identities i ON i.post_uuid=c.post_uuid
CROSS JOIN source_posts post ON post.uuid=i.canonical_uuid AND post.state='active'
WHERE a.canonical_uuid=? AND p.canonical_uuid=a.canonical_uuid AND (u.account_uuid,u.url_key)>(?,?)
ORDER BY u.account_uuid,u.url_key LIMIT 128`, account.UUID, afterAccount, afterKey); err != nil {
			return err
		}
		for _, row := range rows {
			key := performerProfileURLKey(row.URL)
			if !seen[key] {
				added = append(added, sourceadapter.PerformerProfileURL(row.URL))
				seen[key] = true
			}
			afterAccount, afterKey = row.AccountUUID, row.Key
		}
		if len(rows) < 128 {
			break
		}
	}
	afterAccount, afterKey = "", ""
	for {
		var rows []accountProfileURLRow
		if err := dbWrapper.Select(ctx, &rows, `SELECT u.account_uuid,u.url_key,u.url FROM source_accounts a
CROSS JOIN account_observed_profile_urls u ON u.account_uuid=a.uuid
CROSS JOIN source_account_profiles p ON p.uuid=u.profile_uuid AND p.account_uuid=u.account_uuid
WHERE a.canonical_uuid=? AND (u.account_uuid,u.url_key)>(?,?)
ORDER BY u.account_uuid,u.url_key LIMIT 128`, account.UUID, afterAccount, afterKey); err != nil {
			return err
		}
		for _, row := range rows {
			key := performerProfileURLKey(row.URL)
			if !seen[key] {
				added = append(added, sourceadapter.PerformerProfileURL(row.URL))
				seen[key] = true
			}
			afterAccount, afterKey = row.AccountUUID, row.Key
		}
		if len(rows) < 128 {
			break
		}
	}
	if len(added) == 0 && slices.Equal(existing, normalizePerformerURLs(existing)) {
		return nil
	}
	update := models.NewPerformerPartial()
	update.URLs = &models.UpdateStrings{Mode: models.RelationshipUpdateModeAdd, Values: added}
	_, err = NewPerformerStore(nil).UpdatePartial(ctx, *performer.LocalID, update)
	if err != nil {
		return err
	}
	return models.NotifyEntityUpdate(ctx, models.ArchivePerformer, *performer.LocalID, []string{"urls"})
}

func profileURLKeys(urls []string) map[string]bool {
	keys := make(map[string]bool, len(urls))
	for _, value := range urls {
		if key := performerProfileURLKey(value); key != "" {
			keys[key] = true
		}
	}
	return keys
}

func performerProfileURLKey(value string) string {
	return archive.ProfileURLKey(sourceadapter.PerformerProfileURL(value))
}

func normalizePerformerURLs(values []string) []string {
	ret := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = sourceadapter.PerformerProfileURL(value)
		if !seen[value] {
			ret = append(ret, value)
			seen[value] = true
		}
	}
	return ret
}

func updatePerformerURLs(ctx context.Context, id int, update models.UpdateStrings) error {
	before, err := performersURLsTableMgr.get(ctx, id)
	if err != nil {
		return err
	}
	update.Values = normalizePerformerURLs(update.Values)
	after := update.Apply(normalizePerformerURLs(before))
	if slices.Equal(before, after) {
		return nil
	}
	if err := performersURLsTableMgr.replaceJoins(ctx, id, after); err != nil {
		return err
	}
	return rememberRemovedPerformerURLs(ctx, id, before)
}

// Both full and partial performer updates pass here, including bulk edits.
// Compare final sets, so reordering/replacing joins does not count as removal.
func rememberRemovedPerformerURLs(ctx context.Context, performerID int, before []string) error {
	after, err := performersURLsTableMgr.get(ctx, performerID)
	if err != nil {
		return err
	}
	remaining := profileURLKeys(after)
	for _, value := range before {
		value = sourceadapter.PerformerProfileURL(value)
		key := archive.ProfileURLKey(value)
		if key == "" || remaining[key] {
			continue
		}
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO performer_profile_url_suppressions(performer_uuid,url_key,url)
SELECT uuid,?,? FROM archive_entities WHERE performer_id=?
ON CONFLICT(performer_uuid,url_key) DO NOTHING`, key, value, performerID); err != nil {
			return err
		}
	}
	return nil
}

func mergePerformerProfileURLSuppressions(ctx context.Context, source, destination string) error {
	_, err := dbWrapper.Exec(ctx, `INSERT INTO performer_profile_url_suppressions(performer_uuid,url_key,url,created_at)
SELECT ?,url_key,url,created_at FROM performer_profile_url_suppressions WHERE performer_uuid=?
ON CONFLICT(performer_uuid,url_key) DO NOTHING`, destination, source)
	return err
}
