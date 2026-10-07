# Native source-account identity evidence

The native archive separates captured account identifiers, an account's owner,
and performers depicted in media. `pkg/archive.ExtractCapturedAccount` implements
the versioned `captured-account-v1` parser for producer and catalog-import work.
It returns evidence; it does not allocate accounts, merge them, change ownership,
or assign scene/image performers. Integration with ingestion and catalog import
remains pending under the [transition plan](native-archive-transition-plan.md).

The input is a reconstructed capture object, bounded by the source JSON limit.
The output contains a qualified namespace, optional display label, policy version,
and identifier claims with their evidence basis and JSON pointer into the input.
An explicitly captured ID is required before pairing any handles with it. A
missing ID returns no account claim; a malformed claimed identifier is an error
that the caller must surface for review. Retained source data is not discarded
or replaced by this derived result. Existing handle-only inventory records,
explicit associations, and legacy keys still require their own lossless import;
a nil parser result does not authorize discarding them.

| Extractor | Account ID evidence | Additional captured identity |
| --- | --- | --- |
| Reddit | `author_fullname` | `author`, or the author object's `name`; never a feed owner's `user` object |
| Twitter | `author.id` | `author.name` |
| Instagram | `owner_id` | `username` |
| Bluesky | `author.did` | `author.handle` |
| TikTok | `author.id` and/or `author.secUid`, with distinct identifier kinds | `author.uniqueId` or its captured `name` |
| Tumblr | `blog.uuid` | `blog_name` or `blog.name` |
| Coomer/Kemono | `user` in `mirror:<extractor>:<service>` | A matching profile's `public_id` remains a separately typed mirror claim |
| yt-dlp | `channel_id` or `uploader_id`, qualified by extractor/site | Uploader/channel text remains a display label |
| Other gallery-dl extractors | The author's own `id`, `did`, or `uuid`; otherwise a captured owner/uploader/user/creator object or top-level `user_id`/`uploader_id` | Explicit `username`, `account`, or `handle`; a generic `name` remains a display label |

A Reddit parent capture takes precedence over a child media-host account, and
its pointers retain the `/_reddit/` prefix. For unfamiliar extractors, a separately
named author cannot borrow the ID of another `user` object. Directory names,
filenames, post IDs, captions, and scraper target URLs do not identify the author.
Generic yt-dlp URLs establish only a site namespace, never an account ID.

Opaque IDs retain spelling and case. JSON integer IDs remain exact even above
JavaScript's safe integer range. Fractional/exponent numeric IDs, nested objects,
booleans, invalid text, duplicate JSON keys, and oversized payloads are rejected.
Known case-insensitive native handles use the shared reference normalizer;
unfamiliar handles retain case. No requests or redirects are followed.

Mirror display names are labels, not native handles. A profile's public identifier
is accepted only if both its user ID and underlying service match the captured
mirror account. Neither a mirror user ID nor its public identifier is silently
promoted into a native-service ID. This keeps a Coomer/Fansly account distinct
from a native Fansly account even when their numeric values happen to match.

The account repository's indexed lookup returns all candidates for a qualified
identifier. A returned claim is not permission to consolidate those candidates:
reused handles and conflicting IDs still need the checked review operation in
[native schema 1000014](native-schema.md). Different services remain separate
accounts, even when explicitly linked to the same performer.

Tests cover the named services, unfamiliar extractors, native/mirror separation,
TikTok secondary IDs, Reddit parent context, misleading feed profiles, exact large
IDs, malformed input, replay, unchanged extractor input bytes, and unchanged
identity after source-retention reduction or irrelevant counters change.

## Publisher decisions

Native schema 1000016 adds `CapturePublisher.Preview` and `Apply` repository
operations. They load and verify one retained capture, derive its account claims,
and query only the corresponding identifiers and selected account. They never
assign scene/image performers or change account ownership. A post can expose
several selected publisher accounts for review; the service does not guess a
single owner from contradictory captures.

| Preview action | Meaning |
| --- | --- |
| `create` | A captured ID has no matching account or locator candidate. Automatic processing may create a source account. |
| `link` | Captured ID claims resolve to one canonical account, without a contradictory claimed ID kind on that account. Other handle matches remain visible candidates. |
| `review` | IDs are ambiguous/contradictory, only locator candidates match, or the capture's service contradicts known post identifiers. |
| `unavailable` | The capture has no usable ID, has a malformed claim, or its post is forgotten. Existing evidence remains retained. |
| `preserve` | A linked or explicitly unlinked decision already exists. Automatic processing leaves it intact. |

Handles and mirror public identifiers alone cannot establish equivalence. A
reviewer can select a candidate, choose a new account for a reused handle when
no captured ID already matches, unlink a capture's publisher, or return the
capture to automatic selection. Manual choices can resolve an ambiguous match
for that capture without merging accounts or removing other candidate evidence.
A selected account with contradictory IDs of the same claimed kind requires
account reconciliation; this operation cannot overwrite its identifiers.
Qualified native/mirror namespaces remain separate. A manual choice may also
supply a publisher when machine-readable identity is missing, with explicit
review provenance rather than fabricated source claims.

Preview signatures cover the capture, current choice, relevant candidates,
canonical identities, display labels, and contradictions. New competing IDs or
account consolidation invalidate stale review. Unrelated observations of the
same account do not: its general observation revision and creation time are
excluded from the signature. Candidate display is bounded at 100 and reports
truncation. Full candidate paging uses the existing qualified identifier lookup;
explicit review can select a target directly by UUID. Post-account queries begin
with the selected post's indexed capture range rather than scanning all decisions.

Apply requires a stable request UUID and the preview signature. Replaying the
same successful request returns its original decision even after a later unlink
or account consolidation; it does not reapply the old choice. Changed input
with that UUID is rejected. Automatic apply is valid only for a `create` or
`link` preview. For `preserve`, callers use the existing decision without making
another write or history entry. A later explicit `inherit` decision permits a
fresh automatic selection.

Decisions and evidence references are immutable. Identifier evidence records the
capture UUID, parser policy, source JSON pointer, and observed timestamp. Joined
claim records identify exactly which evidence a decision used; the source post
and profile bodies stay shared. Original account associations remain history
while current queries follow canonical accounts after consolidation. Durable
write context prevents a late failure from committing a new account or only
some claims when a caller ignores an error. Startup also checks publication
integrity. Anonymised exports remove publisher decisions and evidence references.

This service does not expose a public endpoint yet. Producer authorization,
ingestion receipts, native review UI, account-profile presentation, and catalog
import remain required integration work. The migration does not invent publisher
choices from old paths, names, or existing account ownership.

## Importing the performer registry

Schema 1000028 adds a reviewed import of a frozen registry's five performer
identity tables and its two older `catalog_metadata_*` tables. Existing catalog
performer UUIDs become the UUIDs of their explicitly bound native performers;
the previous native UUIDs remain resolvable redirects. Local performer IDs,
selected names, aliases, URLs and other library metadata are preserved. Saved
catalog profiles, events, bindings and migration receipts remain inspectable
as original evidence rather than overwriting newer selected metadata.

The import requires the original registry database UUID, a stable snapshot UUID,
capture time, and the explicit namespace identifying this Stash library. Only
saved local bindings or an existing exact catalog UUID identify a performer.
Unbound identities, missing/reused local IDs, multiple active bindings and UUID
collisions become review records. Bindings for other libraries remain external
evidence; their integer IDs are never interpreted as local Stash IDs. Historical
catalog UUID redirects preserve an already-completed merge without deleting or
recreating local performers. An old local merge ID that exists again is reported
for review and is left intact.

An optional, reviewed account map connects each exact old account key to a native
source-account UUID. This is a one-time migration input, not a plugin setting or
a second live ownership registry. Unmapped keys remain review records, including
directory-derived labels whose service identity cannot be asserted. Captured
account-identifier import follows in the registry step below. This performer
operation does not infer native IDs, handles or service namespaces from a key.

Saved associations and explicit unlinks become native ownership decisions only
when both sides are resolved. A `migration_conflict` placeholder remains review,
not a deliberate unlink. Existing native ownership choices are preserved: an
equal choice maps to the existing decision, while a differing choice needs review.
Several old keys mapped to one account coalesce equal choices and report
contradictory ones. The `catalog-metadata-links-v1` receipt makes older plugin
bindings superseded evidence, so they cannot resurrect later unlinks. A registry
containing only the older plugin tables is retained for reconciliation.

The application API exposes these maintenance operations:

| Method and path under `/api/v3/archive` | Result |
| --- | --- |
| `POST /catalog-identity-imports/preview` | Current identity/ownership actions and every original row's proposed outcome, plus the reviewed plan digest |
| `POST /catalog-identity-imports` | Apply `{binding, expected_plan_sha256}` atomically and return the immutable receipt |
| `GET /catalog-identity-imports/{uuid}` | Original receipt, including its frozen plan |
| `GET /catalog-identity-imports/{uuid}/records?after=0` | Up to 100 original rows with retained evidence, outcomes and native references |

These routes require application authorization; producer ingest tokens grant
no access. Changed identities, ownership, bindings or snapshot bytes invalidate
an uncommitted preview. A successful retry returns the original receipt even
after a subsequent native edit. A different snapshot cannot replay the same
registry/namespace cutover. Resolving retained review items is subsequent native
review work, not permission to reapply old choices. The transaction cannot commit
part of an import even if a caller accidentally swallows a write error.

`stash-import-catalog-identities` inventories every table/column in one read-only
SQLite transaction. It rejects unknown shapes and inventories the remaining
catalog/routing/identifier tables for their later import. Original embedded JSON
text and large IDs survive unchanged. Limits are 10,000 retained rows and an
8 MiB document; inputs exceeding these limits need an explicit migration plan.

Prepare from a frozen copy with `--registry`, `--source`, `--snapshot`,
`--captured-at`, `--namespace`, and optionally `--account-bindings`. Save the
output privately. Then use `--binding saved.json --endpoint URL` to obtain the
server preview. Apply that saved preview using the same binding and endpoint,
`--apply --expected-sha256 PLAN_SHA256`; retry the exact same input after response
loss. The Stash application key is read from `STASH_API_KEY` (or `--api-key-env`),
never from the registry payload. Website credentials remain with the scraper.

## Importing catalog accounts and groupings

Schema 1000029 imports the remaining six registry families: `catalogs`, `routes`,
`links`, `account_identifiers`, `account_identifier_checkpoints`, and
`account_profile_urls`. It requires the completed performer import from the same
frozen registry, with the same source UUID and capture time. Each import's retained
table counts must match the other's external inventory. Keep that frozen database
through both steps; matching counts alone do not establish a common snapshot.

Captured, qualified account IDs establish native source accounts. Handles observed
alongside those IDs become identifiers on the same account. Reused handles with
different captured IDs remain ambiguous; matching labels never merge accounts.
Different reference kinds, such as a TikTok numeric ID and `secUid`, resolve to
one account only when existing native evidence already establishes that connection.
Mirror account IDs stay qualified by mirror and service. Captured mirror public
IDs can associate a saved locator in its evidenced catalog; mirror display names
never become native handles. Existing exact ID matches and explicit account
bindings from the performer import preserve the chosen native account and label.
Conflicting native candidates remain visible for review.

An old locator for a known native service can create a provisional account when
no captured ID is available. Its evidence is marked `legacy-locator`; an old
numeric-looking key is retained as `legacy_key`, not promoted to a verified ID.
Unknown services and directory-derived labels remain review items. No website
requests or name-based performer matches run during this import.

Each surviving catalog becomes a disabled `legacy_catalog` source collection.
Old catalog redirects map to that survivor, and all grouping/link history is
retained. A collection's resolved account identifies its publisher; it does not
assign depicted performers to its media. Aggregator and directory collections can
have no account. Imported collections have no root, destination prefix or scrape
URL: directory routes, media keys and identifier checkpoints are retained evidence
for subsequent validated routing and catalog-content import. Importing a grouping
does not activate a scrape or restore old process state.

Resolved saved performer/account links and unlinks become native ownership
decisions, with the same conflict and later-edit protections as the performer
import. Source-qualified `catalog_account_mappings` and `catalog_collection_mappings`
preserve the original migration associations for the remaining catalog import.
They are immutable migration records, not plugin settings or another ownership
authority. Subsequent native account consolidation resolves their original UUIDs.

The application-authorized API follows the same guarded preview/apply contract:

| Method and path under `/api/v3/archive` | Result |
| --- | --- |
| `POST /catalog-registry-imports/preview` | Account, legacy-key, collection and ownership actions; original row outcomes and plan digest |
| `POST /catalog-registry-imports` | Atomically apply `{binding, expected_plan_sha256}` and return the immutable receipt |
| `GET /catalog-registry-imports/{uuid}` | Original receipt and frozen plan |
| `GET /catalog-registry-imports/{uuid}/records?after=0` | Up to 100 original rows, with native references and retained evidence |

One source registry has one account/grouping cutover. Exact retries return the
original receipt after restart or later native edits. A stale plan or changed
snapshot conflicts; a failed write rolls back both domain records and receipts.
Original evidence remains available even when an account, route or catalog needs
review. Anonymised exports remove these receipts, mappings and source rows.

`stash-import-catalog-registry` reads the six families in one read-only SQLite
transaction and inventories the seven performer families. Limits are 25,000 rows
and a 16 MiB document. Unknown table/column shapes, incomplete identifier families,
duplicate row keys and broken catalog redirect graphs are rejected.

Prepare with `--registry FROZEN_SQLITE --identity-import SAVED_IDENTITY_JSON
--snapshot UUID`. The saved identity binding, preview or receipt supplies the
source UUID, performer-import UUID and capture time. Save the output privately,
then preview with `--binding SAVED_REGISTRY_JSON --endpoint URL`. Apply the
reviewed binding using `--apply --expected-sha256 PLAN_SHA256`; retry the same
binding and digest after response loss. As above, authorization uses the Stash
application key, not a producer token or website credential. Review resolution,
source activation and the individual catalog bodies remain subsequent work.

## Preparing individual catalog bodies

The supported producer package now includes `stash-prepare-catalog`, the read-only
input stage for catalog-body migration. Its versioned reader covers recognized
SQLite catalog layouts for versions 1–3, including flat observations, shared
observation/capture rows, separately referenced account profiles, older physical
sidecars and normalized sidecar documents/sources. It checks the database lineage,
column/key inventory, SQLite integrity and logical references independently of
whether older schemas declared every foreign key. Unknown shapes stop preparation.

A prepared snapshot retains every physical row, original JSON text and binary
sidecar bytes in bounded, ordered chunks. Its manifest binds the original registry
UUID, catalog ID, snapshot UUID, capture time, complete schema, table/chunk hashes,
record counts, relationship counts and reconstructed capture hashes. Shared
observations with detail rows produce only their original captures; a flat
observation produces one capture. The shared observation's summary timestamp is
not invented as an additional capture. Profile hashes and reference paths are
validated before hydration, and `_reddit` parent patches preserve their original
merge semantics. Reconstruction runs in memory and adds no stored payload copies.

The CLI publishes a new private directory after flushing its contents and supports
verification of the exact saved manifest after interruption or response loss.
It performs no native database writes or identity/media decisions. The resulting
chunks are the input to the native catalog importer, whose domain
mappings, review outcomes and completion receipts remain separate work. See the
[snapshot command and format](../integrations/gallery-dl/README.md#individual-catalog-snapshots).

## Receiving catalog snapshots

Native schema 1000030 adds bounded, resumable receipt of the prepared catalog
bodies. The application API requires the manifest's original registry source and
catalog ID to have an imported `catalog_collection_mappings` association. It binds
one immutable snapshot to that source/catalog pair, registry receipt and native
collection. A new UUID cannot silently replace an existing frozen input.

| Method and path under `/api/v3/archive` | Result |
| --- | --- |
| `POST /catalog-snapshots` | Receive the original manifest bytes, or return current progress for an exact retry |
| `PUT /catalog-snapshots/{uuid}/chunks/{index}` | Atomically receive the next ordered JSONL chunk and its receipt |
| `GET /catalog-snapshots/{uuid}` | Bounded progress summary, original binding and explicitly pending families |

Both writes require `X-Stash-Manifest-SHA256` with the reviewed manifest digest.
The manifest uses `application/json` and is limited to 8 MiB; chunks use
`application/x-ndjson` and at most 1,000 rows/16 MiB. The server independently
checks recognized table/column/key shapes, unique SQLite key ordering, exact
chunk and table hashes/counts, embedded JSON, binary sidecar checksums and catalog
identity rows. Retained SQLite schema text is evidence and is never executed.
Known sidecar views and their triggers are retained without copying view rows.

`catalog_snapshots` holds the frozen manifest/binding and progress;
`catalog_snapshot_chunks` holds immutable acknowledgements;
`catalog_snapshot_tables` holds resumable table-hash checkpoints; and
`catalog_snapshot_records` temporarily holds original JSONL rows, indexed by
snapshot, table/key and ordinal. Original strings and binary encodings survive
unchanged. Hash checkpoints allow bounded transactions even for a large catalog.
A failed chunk rolls back its records, hashes and receipt together. Replaying an
already received chunk returns current progress without duplicating rows.

The `received` state proves byte receipt only. It always reports `imported:false`
and lists every table in `pending_families`. Referential/capture reconstruction
proofs in the manifest are still claims to reconcile through native domain
importers before completion. Upload does not create posts, change selected fields,
assign performers, scan files, or activate jobs. Native mappings and eventual
retirement of temporary staged bodies remain required subsequent work; these
tables do not become a parallel catalog authority. Anonymisation removes their
private source evidence before deleting registry mappings.

## Mapping catalog source evidence

Native schema 1000031 maps received `posts`, `observations`,
`observation_details` and `account_snapshots` into the native source evidence
services. `catalog_evidence_imports` holds bounded progress;
`catalog_evidence_posts` binds each original post key to its native identity or
review reason; `catalog_evidence_records` retains one immutable disposition per
original row, including its checksum and capture/profile mapping. Staged source
bytes remain available while other catalog families still need import.

| Method and path under `/api/v3/archive/catalog-snapshots/{uuid}` | Result |
| --- | --- |
| `GET /evidence-import` | Current evidence mapping checkpoint; 404 before the first committed batch |
| `POST /evidence-import` | Advance with `expected_manifest_sha256` and the exact `after` ordinal |
| `GET /evidence-import/records?after=0&limit=100` | Bounded original-row dispositions without payload bodies |

These routes require application access. Each write processes at most 50 source
records, stopping between records once decoded input reaches 16 MiB. Individual
source rows and reconstructed payloads also have size bounds. Indexed dependency
lookups load the relevant post, URLs, observation and profiles. Native writes and
their checkpoint commit together. Following a lost response, read the checkpoint
before submitting the next batch; a stale ordinal conflicts. Completed passes
replay their original receipt.

Qualified service IDs share native posts across catalogs. Mirror URLs preserve
`mirror:coomer:onlyfans`, `mirror:kemono:patreon` and other mirror/service
namespaces even when old catalog rows used a native service label. Unqualified
local keys remain scoped to their physical catalog. Captured IDs from the
[supported post adapters](native-ingestion.md) can qualify a local identity, but
differing captured/catalog IDs, existing conflicting associations and forgotten
posts produce review outcomes. No names,
directory labels or content equality establish performer ownership.
Completed import receipts keep their original outcomes; adding an adapter does
not silently rerun earlier identity decisions.

The reader reproduces the original Python checksum encoding before native
conversion. Shared observations with detail rows receive a `shared` disposition;
only their detail rows become captures. Flat observations become one capture.
The native store deduplicates post revisions, payloads and profile bodies. An
exact copied event can share a capture across catalogs only when its old capture
ID and complete native event signature agree; differing payloads, provenance or
capture times remain distinct. Historical collection provenance is pinned to the
original imported collection revision. Unreferenced profile bodies are retained.

A pass ends in `mapped` or `review`, always with `imported:false`. These states
cover source evidence only; they do not complete the catalog migration. They do
not select metadata, attach media, assign performers, construct galleries or
activate source jobs. Subsequent importers and final reconciliation must cover
the remaining families and resolve or retain review outcomes before staging can
be retired. The snapshot receipt's `pending_families` remains the conservative
whole-catalog inventory until that final coordination is implemented.

## Post links and their evidence

Native schema 1000032 adds `SourcePostLinks` for post URLs, identifier evidence
and unselected publisher claims. The schema-1000033 catalog relationship import
uses these services to retain original catalog associations.

`source_post_urls` stores each exact URL once per post. Repeated observations
share that row and retain their own UUID, origin, basis, observation time and
bounded JSON evidence in `source_post_url_evidence`. Observation time describes
when the association was recorded, not when the post was published. URL strings
are retained without fetching or canonicalising them; HTTP(S) URLs containing
userinfo are rejected. A shared URL never merges posts.

`source_post_identifier_evidence` records why a qualified identifier belongs to
the post. Adding it requires the reviewed post revision and cannot move an
identifier already owned by another post. Catalog aliases must keep their
physical-catalog scope until stronger evidence establishes a service identity.

`source_post_account_claims` retains an association with an original account
UUID. Reads also resolve its current canonical UUID after consolidation. A
claim does not choose a capture's publisher, change an explicit unlink, establish
account ownership or assign depicted performers. Those operations retain their
separate core review/selection contracts. Known post and account namespaces must
agree when adding a claim; legacy-only post identities remain unqualified.

All three evidence types use immutable request UUIDs. Exact retry returns the
stored evidence, while changed content conflicts. New evidence is refused for
forgotten posts; retrying an already committed request remains valid. Native
writes require a managed transaction, including rollback when a caller ignores
a late write error. Reads use bounded indexed UUID cursors. Startup checks the
schema and relationship integrity; anonymised exports remove the evidence.

## Comparing possible duplicate posts

`GET /api/v3/archive/posts/{post}/comparison?other={post_uuid}` inspects two
explicit native post UUIDs in one read transaction. It returns their identifiers,
retained URLs, compact latest-capture summaries, current source lists, gallery
choices, attachment decisions and explicit post-to-media choices. Media choices
retain their original UUIDs alongside the current resolved identity, including
deleted records without a live scene/image ID.

The comparison reports qualified identifier disagreements, differing service
namespaces, incompatible source order/counts, disabled versus enabled source
lists, conflicting gallery choices and contradictory media links. It compares
attachment references and resolved media identities, so independently allocated
attachment UUIDs and already-merged media do not create false differences.
Compatible partial source lists retain their gaps. Explicit unlinks and disabled
choices remain visible.

Shared URLs are supporting evidence only. For example, distinct Instagram
stories can share a highlights URL. Matching content, titles or a shared URL
does not establish that two records represent the same post. An empty conflict
list likewise does not establish identity. This endpoint performs no merge,
returns no Apply authorization, and does not rewrite captures or job receipts.
It inspects current choices; original history, evidence and pending work remain
under their existing post scopes. Reviewed consolidation is a separate release
requirement.

Each post is limited to 512 identifiers, 512 URLs, 8,192 retained attachment
identities and 8,192 explicit media choices. Larger scopes return HTTP 422 with
`post_comparison_limit`; the response never silently truncates choices. Source
list loading also retains its existing manifest/entry limits. Queries start from
the selected post indexes. Full post/profile payloads, plugin settings and
library-wide searches are not included.

## Canonical post identities and original evidence

Schema 88 introduces `source_post_identities`. Each original post begins as its
own canonical identity. A reviewed consolidation can group duplicate records
under one surviving UUID while retaining every original post, identifier,
capture, source list and receipt under its original owner. This native relation
does not combine distinct posts that happen to share a caption, URL or media.

Immutable consolidation receipts retain the direct source/destination UUIDs,
reviewed revisions, signatures and reason. Canonical lookup is flattened after
later merges, while the original redirect history remains available. An exact
request retry recovers its original receipt even after another merge, restart
or retained deletion. Forgetting a member tombstones its entire identity group.

Identity reads use indexed lookups and paginated members/history. The storage
primitive limits a reviewed group to 256 original posts and 8,192 qualified
identifiers. Different non-legacy qualified upstream identifiers reject a merge;
shared URLs cannot override them. Updates require a managed transaction and
matching publication context, including rollback when a caller catches a late
write failure. Startup checks reject inconsistent identity/history relationships.

The internal choice review inspects all members of both groups, including
choices retained on earlier aliases. It reports incompatible source order,
gallery choices, explicit media unlinks and differing attachment decisions.
Its signature includes selected library revisions, so a scene/image edit also
invalidates the review. Indexed preflight limits reject oversized evidence before
loading it; an empty conflict list still does not establish post identity.

Schema 89 lets an attachment selection reference original captures and manifests
from any member of its post identity. Foreign keys retain the original evidence
UUIDs; scope guards require matching canonical identities. Neither post payloads
nor manifest entries are copied. The migration preserves existing decision rows,
their row IDs, history and incoming review/gallery references. Startup validation
rejects source evidence belonging to an unrelated post.

Compatible partial lists can contribute to one selection across original post
owners. Attachment representatives are deterministic and use only contributing
manifests; an unused candidate cannot alter the existing selection in a preview.
Pinned and disabled selections remain protected. A gallery created from such a
selection retains the original capture as its metadata provenance. Original
selection-history scopes and exact review receipts remain unchanged.

The internal source-list consolidation operation validates the latest post merge
and the complete reviewed set of current choices. It can combine compatible
lists, pin one original capture or explicitly disable source selection. Additional
lists must come from the reviewed current choices; unrelated, duplicate and
unreviewed lists are rejected. It retires only current pointers, preserving all
original captures, manifests, decisions and saved review receipts. Late failures
roll back the transaction even if a caller catches the error. The encompassing
merge still owns its final receipt and subsequent gallery synchronization.

Schema 90 permits an explicit consolidated post-media choice to supersede the
current choices of every original member. Each cross-owner replacement records
the consolidation that established their shared identity. The database checks
that original owner ancestry existed at that event, rather than comparing
unrelated posts' revision counters. A deferred foreign key prevents committing
replacement proof without the replacement itself. Original decisions and
capture ownership remain immutable, and exact media-decision retries survive
later merges and restart. Existing associations to deleted media can be carried
forward without restoring library records or making them eligible for imports.

The internal gallery choice operation requires the complete reviewed set of
current associations, retires those associations and records one new choice.
It retains both selected and unselected galleries and all original decisions.
Later source synchronization recognizes automatic members contributed by earlier
post identities while preserving manual additions, exclusions, covers and edited
metadata. Gallery claims outside the consolidated group still block adoption;
a caught late failure cannot commit partially retired associations.

Equivalent attachments share a current media choice by canonical post identity
and exact attachment namespace/value. The original attachments, manifests and
immutable decisions keep their UUIDs; no separate attachment-identity table or
copied decisions are needed. The internal consolidation operation reviews every
original head, publishes one choice and retires the others atomically. Multiple
unresolved heads fail explicitly. Later ingestion honors a shared unlink and
can fill an undecided choice only with unique file evidence across the group.
Album inspection resolves shared choices in a bounded batch while retaining the
original attachment on every source slot.

Current post-media association reads resolve the canonical post and include all
its original heads. New ordinary decisions require the canonical UUID and cannot
retire another owner's head without consolidation proof; committed request retry
still returns the original result before inspecting current state. Metadata source
pickers follow shared attachment and whole-post choices across original captures.
They retain original capture/attachment provenance and suppress competing heads
or a canonical post unlink. Attachment review contexts distinguish the requested
UUID from the current choice owner, and the UI recovers saved original requests
before following that owner for new choices.

Current source-list and gallery reads resolve the canonical post, including
ordered album pages and gallery-to-post links. They reject unsettled choices on
earlier owners instead of silently selecting one. Source-list discovery pages
the indexed manifests of all original members without reconstructing payloads.
Ordinary new choices require the current canonical UUID and revision; ingestion
uses that identity while retaining its original capture as provenance.

The identity context returns both requested and canonical identities. Album
responses likewise separate the requested UUID from the current post. Editors
recover an original saved request before any pending canonical request, then use
the canonical identity for new edits. Original history, full merge comparison,
capture and ingestion lookup methods keep their original scopes so evidence and
saved requests remain replayable.

Current post browsing, media reviews, selected publishers and expanded evidence
pages follow the canonical identity. Exact UUID/qualified-ID/URL lookups resolve
the original owner before applying the canonical cursor. A shared URL alone does
not combine unrelated posts. Responses distinguish the requested identity from
the current one; capture and URL references retain their original owners.
Current URL pages select one stable witness per exact URL before pagination.
Capture summaries share revision metadata and page the indexed ranges of the
original members without reconstructing their payloads. Metadata policies use
the same deduplicated current URLs while retaining the selected original capture
and post as provenance. Original-owner evidence getters remain available to
history, import recovery and complete merge comparison.

The identity writer is internal and has no application mutation route. It does
not settle conflicting source-list, gallery or media choices on its own. The
encompassing reviewed transaction, pending-work publication and the complete
post-merge UI must be connected before users can apply a post merge.

## Mapping catalog relationships

Native schema 1000033 maps `accounts`, `handles`, `posts`, `post_urls` and
`post_aliases` after the snapshot's source-evidence pass has finished. Each
`catalog_relation_records` receipt retains the original key, row checksum and
complete source values alongside its native references or review reason. This
keeps legacy basis, timestamps and unsupported values available beyond temporary
staging. `catalog_relations_imports` checkpoints bounded progress atomically
with the native writes and immutable receipts.

| Source family | Native result |
| --- | --- |
| `accounts` | Observe the original legacy key on the account selected by the snapshot's frozen registry mapping. Retain old source IDs and identity basis as evidence; do not reinterpret them as captured service IDs. |
| `handles` | Observe native handles at their original `first_observed` time. Mirror values become `legacy_label` references because these fields can contain display names. |
| `post_urls` | Deduplicate the exact URL per mapped post, preserving separate evidence for each original catalog row. |
| `post_aliases` | Add a physical-catalog-scoped legacy identifier with provenance; an alias already bound elsewhere requires review. |
| `posts` | Retain the old account association as an unselected claim. An absent account yields `unassigned`; missing mappings or incompatible namespaces require review. |

The association observation time is the frozen snapshot time. An old post's
creation time cannot establish when its current account link was made; the
original value remains in the receipt. Original account UUIDs survive account
consolidation, while core lookups resolve their canonical account. Forgotten
posts, invalid legacy values and unmapped dependencies retain review outcomes
instead of creating new associations. No publisher selection, ownership choice,
performer attribution, media intake or job activation occurs in this pass.

| Method and path under `/api/v3/archive/catalog-snapshots/{uuid}` | Result |
| --- | --- |
| `GET /relations-import` | Current relationship checkpoint; 404 before the first committed batch |
| `POST /relations-import` | Advance with `expected_manifest_sha256` and the exact `after` ordinal |
| `GET /relations-import/records?after=0&limit=100` | Bounded summaries; keys exceeding 8 KiB are omitted with `key_omitted:true` |
| `GET /relations-import/records/{ordinal}` | One full original key and source-values object, with its disposition and native references |

These routes require application access. Writes process at most 50 rows, stopping
between records after 16 MiB of decoded input. Each original row already meets
the snapshot's 16 MiB limit. A late failure rolls back native writes, receipts and
progress together. Resume a lost response by reading the committed checkpoint;
completed passes replay unchanged. Startup verifies source-row correspondence,
counts, checkpoint continuity and native evidence scope. Anonymisation removes
these records before their source and account parents.

The CLI is `stash-import-catalog-relations`; it revalidates the frozen snapshot
and received-upload receipt before advancing. Exit 0 means mapped, exit 2 means
completed with review outcomes, and exit 1 means failure or an unavailable
response. An unassigned legacy post is counted separately from a conflict.
Every receipt remains `imported:false`: captured publisher decisions, remaining
catalog families and final semantic reconciliation are still required.

## Selecting catalog capture publishers

Native schema 1000034 applies the existing `captured-account-v1` policy after a
snapshot's evidence and relationship passes have completed. It processes the
original flat observations and detail captures; shared observation parents do
not become extra captures. Decisions use each capture's reconstructed source
payload and the same core service used by native ingestion.

An actual captured author ID can link to a uniquely matching account. With no
matching account or conflicting handle candidates, the core policy can create
an account from that ID. A handle alone, the scraped feed's owner profile, the
folder name or an old catalog association cannot establish the publisher. Mirror
identities retain their service-qualified namespace. Source publishers remain
separate from depicted performers and account ownership choices.

| Receipt outcome | Meaning |
| --- | --- |
| `linked` | The core policy selected a publisher and retained its captured identifier evidence. `created_account` records whether it also created the account. |
| `preserved` | A publisher link or explicit unlink already existed and remains unchanged. |
| `review` | The capture could not be mapped, its identity is invalid, or identity candidates conflict. The receipt retains the decision context and candidate references. |
| `unavailable` | The source has no qualifying captured identity, or the post was forgotten. No publisher is inferred. |

Receipts reference the existing capture and historical decision, retaining a
small context object rather than another copy of source payloads or profiles.
They preserve the original account UUID and resolve its current canonical UUID
when read. Later decisions do not rewrite these historical receipts.

| Method and path under `/api/v3/archive/catalog-snapshots/{uuid}` | Result |
| --- | --- |
| `GET /publisher-import` | Current publisher checkpoint; 404 before the first committed batch |
| `POST /publisher-import` | Advance with `expected_manifest_sha256` and the exact `after` ordinal |
| `GET /publisher-import/records?after=0&limit=100` | Bounded summaries; source keys exceeding 8 KiB are omitted with `key_omitted:true` |
| `GET /publisher-import/records/{ordinal}` | One full source key, disposition, native references and decision context |

Application access is required. Each transaction handles at most 50 rows and
checks a 16 MiB retained-payload threshold between rows. Decisions, account
evidence, receipts and progress commit together; a late failure rolls back the
whole batch. Lost responses resume from the committed checkpoint. Copied captures
reuse their existing choices, and completed imports replay unchanged. Startup
validates capture correspondence, counts, checkpoint continuity and decision
scope. Anonymisation removes the receipts before their source parents.

`stash-import-catalog-publishers` validates the frozen snapshot and completed
upload before advancing. Exit 0 means the pass completed without review outcomes;
this can include unavailable or preserved captures. Exit 2 means completed with
review outcomes, and exit 1 means failure or an unavailable response. Every
result remains `imported:false`: media, memberships, remaining histories and
final semantic reconciliation still require their own migration work.

## Mapping catalog attachment lists

Native schema 1000035 maps explicit attachment evidence from actual flat/detail
captures after a snapshot's evidence pass completes. It applies the shared
`captured-attachments-v2` extractor to reconstructed payloads and verifies that
the captured post identifier resolves to the already-mapped native post. Reddit
gallery lists, direct Reddit media and retained Twitter extended-entities lists
use their source positions and qualified media IDs. A shared observation parent
is not an additional capture.

Identical manifests share native storage, with each capture retaining its
association. Compatible partial lists combine through the core automatic
selection service, preserving missing slots and known counts. Migration choices
carry `origin:migration`; pinned or disabled selections are protected. Conflicting
lists remain available for review without replacing the current selection.
Manifests and selections reference shared evidence rather than copying payloads.

| Receipt outcome | Meaning |
| --- | --- |
| `mapped` | Source attachment evidence was retained and the automatic selection was updated or was already equivalent. `selection_changed` distinguishes those cases. |
| `preserved` | The source manifest was retained while an existing pinned or disabled selection remained unchanged. |
| `review` | Source evidence is unmapped, invalid, identifies a different post, disagrees with an existing capture manifest, or conflicts with the current attachment list. |
| `unavailable` | No supported attachment-list evidence exists, or the post was forgotten. |

Download counters, filenames, folder membership and captions cannot establish a
source list. In the frozen migration corpus, older Twitter captures retain
individual-file metadata without original extended-entities lists; this pass
reports those as unavailable. The native scraper adapter retains the original
lists for new captures. Legacy `appearances` and file associations are separate
migration inputs and must still be handled, including unresolved evidence.

| Method and path under `/api/v3/archive/catalog-snapshots/{uuid}` | Result |
| --- | --- |
| `GET /attachment-import` | Current checkpoint; 404 before the first committed batch |
| `POST /attachment-import` | Advance with `expected_manifest_sha256` and the exact `after` ordinal |
| `GET /attachment-import/records?after=0&limit=100` | Bounded summaries; keys exceeding 8 KiB use `key_omitted:true` |
| `GET /attachment-import/records/{ordinal}` | Full source key, manifest/selection references and retained decision context |

Application access is required. Transactions process at most 50 rows and check
the 16 MiB retained-payload threshold between records. Source lists, selection
decisions, receipts and progress commit atomically. Late errors roll back the
whole batch. Lost responses resume from the committed ordinal; completed passes
replay unchanged. Context retains at most 128 conflict samples and marks further
conflicts with `conflicts_truncated:true`; complete source manifests remain
available. Startup checks original-row correspondence, counts, checkpoint
continuity and native reference scope. Anonymisation removes these receipts.

`stash-import-catalog-attachments` validates the frozen local snapshot and upload
receipt before advancing. Exit 0 means completed without review outcomes, exit 2
means completed with review outcomes, and exit 1 means failure or an unavailable
response. Every result remains `imported:false`. Source-list completeness is
separate from file availability: this pass creates neither playable media nor
galleries. Existing library metadata, attribution and gallery memberships remain
unchanged while subsequent media/appearance mapping is completed.

## Mapping catalog assets, files and appearances

Schema 1000038 imports these three families after a snapshot's evidence pass.
Application access is required. A reviewed binding identifies the logical media
root and its revision, the snapshot collection's revision, and the historical
library mount prefix. This mapping translates catalog-relative paths into the
paths stored in Stash. It does not bind a live filesystem mount or activate a
worker; the root and collection can remain disabled during rehearsal.

| Operation | Contract |
| --- | --- |
| `POST /api/v3/archive/catalog-snapshots/{snapshot}/media-import` | Begin with `expected_manifest_sha256`, `root_uuid`, `root_revision`, `collection_revision` and `library_root_path`. Repeating the same binding returns its checkpoint; a different binding conflicts. |
| `GET .../media-import` | Read the binding, current phase and outcome counters. |
| `POST .../media-import/advance` | Send `expected_manifest_sha256` and `after`, the previously returned `processed_records` count. |
| `GET .../media-import/records?after=ORDINAL&limit=100` | Read bounded summaries of original rows and their native references. |
| `GET .../media-import/records/{ordinal}` | Read one complete source key and retained match/review context. |

The phases are `assets`, `files`, `appearances`, then `complete`. Each transaction
handles at most 50 source rows and checks a 16 MiB input threshold between rows.
The resume checkpoint is the processed-record count across phases, since original
catalog row ordinals are ordered differently. Original rows, native evidence and
receipts commit together. A lost response resumes by reading the checkpoint;
completed passes replay unchanged.

Assets become shared source claims. Files become observations retaining their
original state, role, size, timestamp and survivor path. A path-derived asset ID
is not a content hash. Automatic matching uses literal paths under the reviewed
mount, including explicit survivor paths, then existing server-verified SHA-256
content. Multiple candidates or conflicting metadata require review. Original
nanoseconds remain stored, while modification times are compared at the
whole-second precision retained by Stash's file records. ZIP matches include
archive and member identities and both generations.

Appearances retain post-file evidence even when no playable media is available.
A currently valid match with exactly one scene/image owner also adds post-media
evidence. The original attachment key, source media ID, download position and
source path remain attached to that evidence; download numbering does not become
album order. Missing source paths can use a unique location sharing the same
asset claim; ambiguous locations remain for review. Existing library values,
performer attribution, attachment selections and galleries are preserved.
Unindexed present files remain unavailable here and require normal media intake.

`stash-import-catalog-media` validates the local frozen snapshot and its upload
receipt before binding or advancing. Exit 0 means the pass completed without
review outcomes, exit 2 means completed with review outcomes, and exit 1 means
failure or an unavailable response. `unavailable` retains missing/pending media
as evidence. Every result remains `imported:false` until the remaining catalog
families and final reconciliation are complete. Database backups include the
native evidence and receipts; anonymised exports remove them.

## Historical post collection membership

Schema 1000039 adds `source_collection_post_evidence`. Each immutable membership
names a post and a historical collection definition, with its own evidence UUID,
origin, basis, observation time and provenance. It needs no scrape capture,
media file or performer. One post can belong to several groups, and several
catalog snapshots can independently support the same post/group relationship.
New evidence advances the post's review revision; exact replay does not. A
forgotten post rejects new evidence while retaining historical replay.
This supplements existing capture-to-collection membership and manual
media-intake provenance. The endpoints below return direct post-membership
evidence; capture-based membership remains available through collection captures.

Native membership evidence is paged by its UUID using `after` and `limit`
(default 50, maximum 100):

| Method | Endpoint | Result |
| --- | --- | --- |
| GET | `/api/v3/archive/collections/{collection}/post-memberships` | Post membership evidence for one collection |
| GET | `/api/v3/archive/posts/{post}/collection-memberships` | Collection membership evidence for one post |

These lists contain provenance records. Multiple records can support the same
post/group relationship; a collection's distinct-post view should group those
references without discarding their evidence.

The catalog membership importer consumes only the original `memberships` rows
after their snapshot's evidence pass has completed. A registry-qualified
collection key maps to one native group across its downloaded catalogs. The
mapping records the original kind and label and the first native definition.
Later native renames and retirement leave that historical definition intact.
Equal labels alone do not combine different source keys or separate registries.

Legacy `creator` meant that a directory name contained commas, so it maps to a
directory group without assigning an owner. Other directory memberships map to
directory groups; subreddit memberships retain their kind and Reddit namespace.
Groups start disabled with no account, target URL or media root. Unsupported
identities and conflicting definitions remain review records with their exact
original values. Membership does not create an album or imply depicted
performers, publishers, or ownership.

The application-authenticated migration routes are:

| Method | Endpoint | Result |
| --- | --- | --- |
| GET/POST | `/api/v3/archive/catalog-snapshots/{snapshot}/membership-import` | Read progress or advance a bounded transaction |
| GET | `/api/v3/archive/catalog-snapshots/{snapshot}/membership-import/records` | Receipt summaries by original ordinal |
| GET | `/api/v3/archive/catalog-snapshots/{snapshot}/membership-import/records/{ordinal}` | Original values and native references |

POST requires `expected_manifest_sha256` and the last committed source ordinal
as `after` (zero starts). Each transaction handles at most 50 rows and checks
the 16 MiB work limit between rows; group creation, membership and receipts
commit together. `stash-import-catalog-memberships` reads committed progress
before resuming the same frozen manifest after a lost response. Exit codes are
0 for mapped, 2 for review, and 1 for a failed/unavailable request. Original
membership rows have no timestamp: the snapshot capture time describes when
the evidence was retained, without inventing when the post joined the group.
The result remains `imported:false` pending other catalog families and final
reconciliation. Backups include the new records; anonymisation removes them.

## Matching imported media to source albums

The core `SourceGallery.PreviewBackfill` service operates on one post's selected
attachment list. It proposes media choices and shows the resulting gallery
membership without writing. The post UUID identifies the album; the selected
source manifest supplies attachment positions, including repeated attachments
and missing slots. Folder labels, filenames and download counters cannot create
an album or determine its order.

Callers must choose a matching policy explicitly:

| Policy | Accepted historical evidence |
|---|---|
| `source-identifiers-v1` | A retained qualified Reddit or Twitter media ID equal to the typed attachment ID, plus an imported file match for the same post |
| `legacy-reddit-filename-v1` | The identifier policy, plus the original Reddit `post-id_media-id_...` or `post-id_media-id.ext` filename convention when no explicit media ID was retained |

The filename policy checks both IDs against existing native post/attachment
records. It uses the original file observation, which can differ from the
surviving file after deduplication or conversion. A conflicting or malformed
explicit ID suppresses filename fallback. Unsupported namespaces and delegated
media services cannot borrow another service's identifier convention.

Proofs reference the original post-media evidence, file observation and file
match. Current library file generations, ZIP archive generations and unique
scene/image ownership are revalidated. A historical path match remains legacy
evidence; it does not add a verified content digest or assert fresh filesystem
verification. Multiple proofs for the same canonical media UUID form one
candidate. Different candidates, changed files/ownership and media-kind conflicts
remain review items. Existing attachment evidence also participates in ambiguity
detection. Bounded queries fail explicitly if they cannot inspect the complete
candidate set.

Every existing attachment decision is preserved, including explicit unlinks
and deliberately undecided reviews. For unresolved attachments, a unique current
file-backed candidate can be proposed. Applying the exact preview signature
records attachment-specific legacy evidence and a migration decision together
with gallery sync. It does not manufacture a capture association. Ambiguous and
unavailable attachments remain gaps, and the shared gallery service preserves
manual membership, exclusions, cover choices, metadata and deletion suppression.

`Backfill` requires a managed write transaction. It rechecks the preview before
writing and revalidates the selected file proofs and resulting gallery before
commit. A partial failure cannot commit even if its caller swallows the error.
A fresh preview after successful application is a no-op for those choices and
reuses the same gallery. `gallery.AlbumBackfill` now admits the reviewed signature
as durable work, with publication and hook delivery tracked separately. Its
[application API](native-ingestion.md#historical-source-album-backfill) supports
status, cancellation and explicit retry. Schema 1000040 retains existing file
jobs while admitting the new album job kind. The native UI, migration command
and production activation remain subsequent work.
