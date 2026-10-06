package sqlite

import "context"

// All evidence retains its original owner. These lookups compare the indexed
// canonical membership without walking history or copying captures/manifests.
const selectionManifestHeadersQuery = `SELECT m.*,owner.canonical_uuid=selected.canonical_uuid AS in_scope
FROM source_post_identities selected
JOIN source_attachment_manifests m
JOIN source_post_identities owner ON owner.post_uuid=m.post_uuid
WHERE selected.post_uuid=? AND m.uuid IN `

const selectionCaptureQuery = `SELECT c.manifest_uuid FROM source_capture_attachment_manifests c
JOIN source_post_identities owner ON owner.post_uuid=c.post_uuid
JOIN source_post_identities selected ON selected.post_uuid=? AND selected.canonical_uuid=owner.canonical_uuid
WHERE c.capture_uuid=?`

func samePostIdentity(ctx context.Context, left, right string) (bool, error) {
	var matches bool
	err := dbWrapper.Get(ctx, &matches, `SELECT EXISTS(SELECT 1 FROM source_post_identities l
JOIN source_post_identities r ON r.post_uuid=? AND r.canonical_uuid=l.canonical_uuid
WHERE l.post_uuid=?)`, right, left)
	return matches, err
}
