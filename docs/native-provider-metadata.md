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

## Interactive selection and history

Scene and performer scrape review carries each accepted field's provider and
remote ID into the edit form. Apply changes the pending form; Save records the
edit and its import receipts together. Changing a field afterward removes its
pending attribution, even if the user types the original value back. Discard
and a successful save clear pending choices. A failed save retains them for
retry. Explicit list merges can retain choices from multiple providers;
replacing a scalar keeps the most recently selected provider.

The existing create-related-item actions still create those items when Apply is
pressed. New performers, studios and tags use their own remote IDs, never the
parent scene's ID. A user-entered replacement name keeps the explicit remote
association without attributing that name to the provider. Results missing a
required remote ID cannot be imported as provider metadata.

GraphQL scene, performer, studio and tag create/update inputs accept
`provider_metadata: [{ endpoint, remote_id, fields }]`. These are explicit user
choices, not trusted claims signed by a provider. Every selected field must be
on that entity's metadata allowlist and included in the same mutation input.
The server records the values actually saved, including normalization and merge
results. Invalid fields, identities or receipts roll back the edit. Provider
choices are excluded from entity-field hook lists and are rejected inside merge
values; save such choices separately from merging entities.

Provider import history is available in scene metadata review and the performer,
studio and tag history tabs. It loads on expansion, with bounded pages and
explicit refresh. A performer's Source accounts identity history also exposes
receipts on retained identities from before a merge. Historical accepted values
remain separate from current field decisions: matching a current value to an old
receipt does not establish its current origin.

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

The storage contract, API, batches, identification paths, interactive selection
and history UI are implemented. SQLite/HTTP tests verify transactional receipts;
form tests and Chromium/WebKit checks cover selection, manual edits, discard,
related-item creation and history pagination. See
[the interactive verification report](native-provider-review-verification.json).
Populated schema-96 migration, final-image startup/performance and owner
acceptance remain cutover requirements. Production remains on the compatible
release until the full cutover gates pass.
