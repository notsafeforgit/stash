# Provider metadata attribution

Stash-box IDs identify a remote entity by endpoint and entity kind. They do not
establish the origin of every local field. Native schema 1000096 adds immutable
`provider_metadata_imports` receipts for selected provider metadata applications.

Each receipt identifies the local UUID and revision, provider endpoint, remote
ID, application path (`review`, `identify`, or `batch`), accepted field values and
time. The repository reads those values from the library inside the same write
transaction as the edit. It rejects unknown fields and stale identities. An edit
and its receipt roll back together. A SHA-256 integrity digest and immutable
database trigger protect the recorded facts; this is not a provider signature.

Values describe the result of accepting metadata, including configured merge
behavior. For example, accepting aliases in merge mode records the resulting
local and remote aliases together. Subsequent manual edits do not alter the
receipt or acquire provider attribution. An import history is therefore distinct
from a claim about the current origin of every field.

Relationship values use native UUIDs. Artwork choices record a digest and byte
count, without duplicating image bytes or temporary URLs. The normal blob store
continues to own currently selected artwork; an import receipt alone does not
retain every superseded image. Performer dates retain their original precision.
The field allowlist covers scene, performer, studio and tag metadata, excluding
Stash-specific flags, timestamps, provider credentials and stash-ID collections.

UUID adoption updates the receipt's foreign key while retaining the original
UUID in its immutable evidence. Merging or deleting an entity retains its
historical identity and receipts. History queries address one identity; clients
must include the retired identities explicitly when displaying merged history.
Database snapshots include these tables automatically. Anonymization removes the
private receipts before rewriting identities and metadata.

The authenticated native archive API exposes:

```text
GET /api/v3/archive/entities/{uuid}/provider-metadata-history?after=0&limit=50
```

`after` is an exclusive receipt sequence; `limit` is between 1 and 100. Results
are ordered by sequence. The route is read-only and does not contact providers.
Invalid identities/cursors are rejected; unknown identities return 404.

## Integration status

Batch performer, studio and tag creation/refresh record accepted fields,
including parent/category relationships and the final performer ID after a
provider merge. Existing field exclusions and alias/URL merge settings continue
to control the edit. Required names accepted during creation are recorded even
if name changes are excluded from refresh settings. Invalid optional values
ignored during creation do not receive attribution.

Scene identification records the accepted scene fields and newly created
performers, tags, studios and parent studios in the same transaction. A failure
in any import receipt rolls back the complete scene identification, including
related creations; successful hooks run afterward. Provider sources require a
recorder. Ordinary scrapers, which have no stash-box endpoint, continue using
their existing path. Identifying a scene may link an existing parent studio;
the create-missing setting does not authorize replacing that parent's metadata.

Studio refreshes retain the explicitly selected local identity. Studio/tag
batches reject source results matched or linked to another local entity, and
reject replacement of a different current link at the same provider. Ambiguous
exact tag-name results require an explicit remote ID. Failed parent creation
does not leave a stale local ID in the source result.

The storage contract, API, batches and identification paths are implemented and
covered by SQLite/HTTP tests. Interactive scrape selection and the history UI
still need integration. Existing field
decision views must distinguish historical imports from current field provenance.
These remaining paths are part of the transition requirement; the initial
automated import integration does not complete it. Production remains on the compatible
release until the full cutover gates pass.
