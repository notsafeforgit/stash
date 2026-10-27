package sqlite

import (
	"context"
	"strings"

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

func subscribeLinkedRedgifsProfile(ctx context.Context, target, root, prefix, capture string) error {
	collections := &SourceCollectionStore{}
	// The discovery identity survives retargets and disables: another post must
	// never recreate an automatically discovered source that was later edited.
	id := uuid.NewSHA1(uuid.NameSpaceURL, []byte("stash-linked-redgifs-v1\x00"+root+"\x00"+target)).String()
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
	_, err = collections.Put(ctx, models.SourceCollectionInput{UUID: id,
		SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Redgifs / " + strings.TrimPrefix(target, "https://www.redgifs.com/users/"),
			Kind:  "account", Namespace: "native:redgifs", State: "active", TargetURL: target, RootUUID: &root, PathPrefix: prefix,
		}, Origin: "ingest", Reason: "Linked Redgifs uploader observed in Reddit profile capture " + capture})
	if err != nil {
		return err
	}
	_, err = (&SourceSubscriptionStore{}).Put(ctx, id, models.SourceSubscriptionInput{Enabled: true})
	return err
}
