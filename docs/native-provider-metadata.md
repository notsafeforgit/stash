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

Batch performer creation and refresh record accepted fields, including the final
remote ID after a provider merge. Existing field exclusions and alias/URL merge
settings continue to control the edit. Required names accepted during creation
are recorded even if name changes are excluded from refresh settings.

The storage contract and API are implemented and covered by SQLite/HTTP tests.
Studio/tag batches, scene identification and related entity creation, interactive
scrape selection and the history UI still need integration. Existing field
decision views must distinguish historical imports from current field provenance.
These remaining paths are part of the transition requirement; the initial
performer integration does not complete it. Production remains on the compatible
release until the full cutover gates pass.
