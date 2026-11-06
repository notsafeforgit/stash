package sqlite

import (
	"context"

	"github.com/stashapp/stash/pkg/sourceadapter"
)

// Explicit identity wins over incidental links. URL-only legacy posts qualify
// when every saved X permalink identifies the same post. Ambiguity stays blank.
func twitterPostDate(ctx context.Context, post string) (*string, error) {
	var identities []string
	err := dbWrapper.Select(ctx, &identities, `SELECT DISTINCT identifier.value
 FROM source_post_identities identity JOIN source_post_identifiers identifier ON identifier.post_uuid=identity.post_uuid
 WHERE identity.canonical_uuid=(SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=?)
 AND identifier.namespace='native:twitter' LIMIT 2`, post)
	if err != nil || len(identities) > 1 {
		return nil, err
	}
	if len(identities) == 1 {
		return sourceadapter.TwitterPublicationDate(identities[0]), nil
	}
	var platforms []string
	err = dbWrapper.Select(ctx, &platforms, `SELECT DISTINCT capture.platform
 FROM source_post_identities identity JOIN source_captures capture ON capture.post_uuid=identity.post_uuid
 WHERE identity.canonical_uuid=(SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=?) LIMIT 2`, post)
	if err != nil || len(platforms) != 1 || platforms[0] != "twitter" {
		return nil, err
	}
	urls, err := (&SourcePostLinksStore{}).CurrentURLs(ctx, post, "", 100)
	if err != nil || len(urls) == 100 {
		return nil, err
	}
	identity := ""
	for _, link := range urls {
		candidate := sourceadapter.TwitterPostID(link.URL)
		if candidate == "" {
			continue
		}
		if identity != "" && identity != candidate {
			return nil, nil
		}
		identity = candidate
	}
	return sourceadapter.TwitterPublicationDate(identity), nil
}
