package sqlite

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

// Discovery is limited to the author's own profile collection. A subreddit,
// saved feed, crosspost or unrelated child extractor cannot subscribe a library
// to somebody else's profile. These URLs are related sources, not ownership.
func recordLinkedRedgifsProfile(ctx context.Context, capture string, raw []byte) (string, error) {
	link, err := archive.ExtractCapturedRedditRedgifsProfile(raw)
	if err != nil || link == nil {
		// Malformed optional discovery evidence does not invalidate the post.
		return "", nil
	}
	type scope struct {
		Collection string  `db:"collection_uuid"`
		Revision   int     `db:"revision"`
		Target     string  `db:"target_url"`
		Root       *string `db:"root_uuid"`
		Path       string  `db:"path_prefix"`
		Monitor    bool    `db:"monitor"`
	}
	url := ""
	after, revision := "", 0
	for {
		var scopes []scope
		if err := dbWrapper.Select(ctx, &scopes, `SELECT r.collection_uuid,r.revision,r.target_url,r.root_uuid,r.path_prefix,
(COALESCE(s.enabled,1)=1 AND r.state='active' AND COALESCE(mr.state,'')='active') AS monitor
FROM source_collection_captures c
JOIN source_collections b ON b.uuid=c.collection_uuid AND b.revision=c.collection_revision
JOIN source_collection_revisions r ON r.collection_uuid=b.uuid AND r.revision=b.revision
LEFT JOIN source_subscriptions s ON s.collection_uuid=b.uuid
LEFT JOIN media_roots m ON m.uuid=r.root_uuid
LEFT JOIN media_root_revisions mr ON mr.root_uuid=m.uuid AND mr.revision=m.revision
WHERE c.capture_uuid=? AND (r.collection_uuid,r.revision)>(?,?) AND r.namespace IN ('','native:reddit')
AND NOT EXISTS(SELECT 1 FROM source_collection_aliases a WHERE a.alias_uuid=b.uuid)
ORDER BY r.collection_uuid,r.revision LIMIT 128`, capture, after, revision); err != nil {
			return "", err
		}
		for _, scope := range scopes {
			after, revision = scope.Collection, scope.Revision
			if scrape.RedditProfileURL(scope.Target) != link.SourceProfileURL {
				continue
			}
			if scope.Root != nil {
				retired, err := linkedRedgifsProfileRetired(ctx, link.URL, *scope.Root)
				if err != nil {
					return "", err
				}
				if retired {
					continue // Old captures cannot relink a retired account.
				}
				full, err := linkedRedgifsFolderFull(ctx, link.URL, *scope.Root, link.Directory)
				if err != nil {
					return "", err
				}
				if full {
					continue // A repost profile: the uploader is not this owner's account.
				}
			}
			url = link.URL
			// Keep the directory actually chosen by the Reddit job, including
			// custom names. Missing/unsafe hints may add a URL but cannot widen
			// the original root or choose a download destination.
			validDirectory := archive.ValidRootRelativePath(link.Directory, true) &&
				(scope.Path == "." || link.Directory == scope.Path || strings.HasPrefix(link.Directory, scope.Path+"/"))
			if scope.Monitor && scope.Root != nil && validDirectory {
				if err := subscribeLinkedRedgifsProfile(ctx, link.URL, *scope.Root, link.Directory, capture); err != nil {
					return "", err
				}
			}
		}
		if len(scopes) < 128 {
			return url, nil
		}
	}
}

func linkedRedgifsProfileRetired(ctx context.Context, target, root string) (bool, error) {
	bare := strings.Replace(target, "https://www.", "https://", 1)
	var retired bool
	err := dbWrapper.Get(ctx, &retired, `SELECT EXISTS(SELECT 1 FROM source_collections b
 JOIN source_collection_revisions r ON r.collection_uuid=b.uuid AND r.revision=b.revision
 WHERE r.root_uuid=? AND r.namespace='native:redgifs' AND r.kind='account' AND r.state='retired'
 AND lower(rtrim(r.target_url,'/')) IN (?,?,?,?))`, root, target, bare,
		strings.Replace(target, "https://", "http://", 1), strings.Replace(bare, "https://", "http://", 1))
	return retired, err
}

// The discovery identity survives retargets and disables: another post must
// never recreate an automatically discovered source that was later edited.
func linkedRedgifsCollectionID(root, target string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("stash-linked-redgifs-v1\x00"+root+"\x00"+target)).String()
}

const linkedRedgifsReason = "Linked Redgifs uploader observed in Reddit profile capture "

// linkedRedgifsFolderLimit bounds discovery per Reddit download folder. A
// profile that links many different uploaders reposts other people's clips;
// their profiles are neither its owner's accounts nor sources to download.
const linkedRedgifsFolderLimit = 9

// linkedRedgifsFolderFull reports whether a new uploader would exceed the
// limit in the folder chosen by the Reddit job. Uploaders already discovered
// there keep their sources, profile URLs and replays; a repost profile cannot
// add more. The count uses each discovered source's creating revision, so a
// later disable, retarget or retirement still counts.
func linkedRedgifsFolderFull(ctx context.Context, target, root, directory string) (bool, error) {
	var full bool
	err := dbWrapper.Get(ctx, &full, `SELECT NOT EXISTS(SELECT 1 FROM source_collections WHERE uuid=?)
 AND (SELECT count(*) FROM source_collection_revisions INDEXED BY source_collection_directory
  WHERE root_uuid=? AND path_prefix=? AND revision=1 AND namespace='native:redgifs' AND kind='account'
  AND origin='ingest' AND substr(reason,1,?)=?) >= ?`,
		linkedRedgifsCollectionID(root, target), root, directory, len(linkedRedgifsReason), linkedRedgifsReason, linkedRedgifsFolderLimit)
	return full, err
}

func subscribeLinkedRedgifsProfile(ctx context.Context, target, root, prefix, capture string) error {
	collections := &SourceCollectionStore{}
	id := linkedRedgifsCollectionID(root, target)
	existing, err := collections.Find(ctx, id)
	if err != nil || existing != nil {
		return err
	}
	bare := strings.Replace(target, "https://www.", "https://", 1)
	candidates, err := collections.LookupCurrentTargets(ctx, []string{target, target + "/", bare, bare + "/"}, nil, &root, true)
	if err != nil || len(candidates) != 0 {
		// Preserve existing subscriptions, including paused/retired/manual ones.
		return err
	}
	account, err := redgifsProfileAccount(ctx, id, target, time.Now().UTC())
	if err != nil {
		return err
	}
	var accountID *string
	if account != nil {
		accountID = &account.UUID
	}
	_, err = collections.Put(ctx, models.SourceCollectionInput{UUID: id,
		SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Redgifs / " + strings.TrimPrefix(target, "https://www.redgifs.com/users/"),
			Kind:  "account", Namespace: "native:redgifs", State: "active", TargetURL: target, AccountUUID: accountID, RootUUID: &root, PathPrefix: prefix,
		}, Origin: "ingest", Reason: linkedRedgifsReason + capture})
	if err != nil {
		return err
	}
	_, err = (&SourceSubscriptionStore{}).Put(ctx, id, models.SourceSubscriptionInput{Enabled: true})
	return err
}
