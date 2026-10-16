# Native source collections

Open **Source collections** from the desktop utility menu or mobile navigation
drawer. Collections describe scrape targets, feeds, searches, imported catalogs,
folders and manual batches. They have stable UUIDs and immutable definition
history in the native database. Accounts identify source publishers; assigning
an account here does not identify the performers depicted in its media.

Search current collection names, source URLs or relative folders, optionally
filtering by type and status. Searches return 25 rows at a time. Editing a
collection refreshes its card and preserves the current page. History is fetched
only when expanded and has its own pagination.

Creating or editing a collection supports its name, type, source URL, qualified
service, source account, registered media root, relative folder and state. Account
and root pickers search the server; they do not load every catalog. Selecting an
account supplies its service namespace. Direct folders and manual batches may
leave both source fields empty. A folder uses a canonical relative path under
the selected root, or `.` for that whole root. The root is a deployment binding;
the collection retains the portable root UUID and relative path.

Active permits configured ingestion; Disabled pauses it; Retired marks a
collection as no longer used. Both Disabled and Retired can be restored: select
Active (or Disabled to restore without allowing ingestion) and save. Restoration
appends a definition revision, preserving the UUID, media associations and
history. Stale revisions still conflict. This does not resurrect deleted media.

Historical membership-only groups are removed after a complete catalog import.
Useful directory/subreddit names and studio attribution remain on the posts;
the completed aggregate receipt prevents a retry from recreating the groups.
Unfinished or unresolved imports, groups edited by a user, and collections with
other consumers are preserved. Native migration 1000121 also performs this
cleanup for previously completed imports in the same database transaction.
It does not delete posts, captures, translations, media, or active subscriptions.

An imported catalog collection that still holds file observations or metadata
policies remains a functional scope. Its state is independent of the account's
live profile subscription. Each Reddit
profile has one source URL, such as `https://www.reddit.com/user/example/`.
New/top submissions and author-search passes are execution details of that source;
they are not separate editable subscriptions. Pausing or retiring the profile
blocks all its passes. **Automatic monitoring** is a separate, reversible switch
on performer source tabs, account review and source collections. Turn it off to
stop daily and periodic recovery checks while retaining the source, posts and
media. **Stop monitoring all linked accounts** uses exact current ownership,
including merge redirects; it never matches performer names or depicted people.
Manual downloads and initial imports remain independent. Queued monitored runs
are cancelled; a running attempt can finish delivering its data. An offline
snapshot cannot start a new attempt after its subscription has changed.

The route used to find a post is operational context, not another archived item.
Finding the same Reddit/Twitter post through a profile, search, subreddit or saved
feed reuses its source identity and unchanged content. Scrape-route fields do not
distinguish its stored content. Changes to post text or album items remain
retained, and different posts can share the same deduplicated media. Post browsing
shows content, publication dates and associated media without sighting counters,
first/last scrape times or discovery-route history. Collection/capture links and
delivery receipts remain internal inputs to scoped ingestion, metadata policies
and safe retries; these links do not copy the post payload.

Subscriptions live in the normal Stash database (`source_subscriptions`, schema
1000114). New sources default off. Migration does not subscribe observed authors
or all existing collections: deployments explicitly import their intended target
lists. Routine database backups cover subscriptions with the rest of the archive.
The authenticated application API reads `/api/v3/archive/subscriptions` and
changes `/api/v3/archive/collections/{uuid}/subscription` with
`{"expected_revision": 0, "enabled": true}`. The producer API provides a
read-only, root-scoped enabled list via `POST /api/v3/ingest/subscriptions/list`
with `root_uuid`, `namespace` and an optional `after` cursor in the JSON body.

The gallery-dl integration consumes that list; Stash does not hold extractor
configuration, website credentials, a gallery-dl list-file format or a scheduler.
Host launchers use `--subscriptions`; n8n source management runtime version 2
omits `lists` and updates the same subscriptions. Version 1 remains readable for
already saved workflow plans. Convert automatic callers together before treating
the Stash switch as authoritative; an explicitly supplied local list remains a
one-off input and is not monitored subscription work.
Merely creating or activating a collection does not schedule downloads or assign
performer metadata.
Worker activation and metadata policy remain separate operations. Existing
collections expose **Edit metadata rules**, including fixed performer selection
for folder scans. **Settings → Library** manages folders and their bindings.

## Studios for aggregate sources

**Source studio** associates a subreddit, repost account or other collection
with an existing studio. Studio names, aliases and URLs can help identify the
right record; this is independent of performer attribution. The association
is stored in the native database, not a gallery-dl configuration file.

Posts display the distinct studios of their associated sources. Shared source
albums display all those studios across every linked post, even when the post
list spans multiple pages. An album inherits its primary studio only when the
union contains exactly one studio. Multiple studios leave the inherited primary
empty. Explicit gallery studio assignments and clears take precedence. Overlap
merging accepts different inherited studios, retaining the full source union;
conflicting manual studio choices still require review. Manual/folder galleries
are not changed by this inheritance.

The association has its own revision, so changing it does not invalidate scraper
leases or collection metadata policies. `GET`/`PUT
/api/v3/archive/collections/{uuid}/studio` reads or saves
`{"expected_revision":0,"studio_uuid":"<studio UUID>"}`; `null` clears it.
`GET /api/v3/archive/posts/{uuid}/studios` returns the distinct studio references.
Gallery `album-posts` responses include `studios` across the complete album.
`POST /api/v3/archive/source-galleries/studios/reconcile`, with 1–100
`gallery_uuids`, repairs inherited primaries without changing protected choices.
Normal source and album changes update affected galleries automatically.
Media's own studio remains governed by its metadata rules or explicit edits.

Schema 1000119 stores the current source association in
`source_collection_studios`. Database backups include it automatically. A
transaction-local gallery work set is emptied at commit; it is not another
scrape queue or historical catalog. No duplicated post payload is introduced.

## Account review and incidental authors

Account review defaults to **Tracked accounts**: accounts directly associated
with a current source collection, including an imported account folder, or with
an explicit ownership choice. Pausing a source retains that association. Old
source revisions and superseded Reddit retrieval definitions do not independently
add accounts to review. Consolidated account identities resolve to one entry.

An author encountered in a subreddit, saved-post feed, repost or other account's
scrape remains available for post attribution. Downloaded media and captured
profile metadata alone do not add that author to the account review queue or
the general review inbox. **All known accounts** exposes these incidental authors
when needed; their details explain why they are outside the default queue.
Adding a direct source association or making an ownership choice brings the
account into the tracked scope. Source account pickers and identity lookups still
search all known accounts. No captured attribution, media, or account evidence
is deleted by this filtering.

Account cards, identifier details and consolidation pickers show service handles,
IDs and profile URLs. Imported `legacy_key` references remain internal for import
replay and identity resolution. Review queries exclude them before pagination, so
they neither fill a page nor count toward the card's additional identifiers.

## Media roots and server folders

Open **Media roots** from the desktop utility menu or mobile navigation drawer.
A root has a stable UUID and a name; its optional local binding records an
existing directory on the Stash server. Collections retain this UUID and their
relative folders, so a root can be restored or relocated without changing their
portable paths. Search root names and current server paths, filter by state,
and inspect one root at a time. Lists and history use pages of 25.

To bind a new folder, enable **Associate a folder on this server**, enter its
absolute path as seen by Stash (including container mounts), then choose
**Check folder**. The check returns the canonical path and opened directory's
identity. It creates no database record or folder. Save records that exact
binding and verifies that the directory has not changed since the check.
Editing the path clears the check. Saving never moves files, starts scans,
rewrites existing scene/image file paths or grants a worker access.

An unbound root preserves its identity and collection paths but cannot accept
file ingestion. Disabling a root preserves its binding; reactivation verifies
the directory again. Labels can be changed and roots disabled while their
unchanged folder is offline. Retired roots can also be restored to Disabled or
Active with the same UUID; activating a bound root rechecks the directory.
Neither disabling nor retiring deletes media or collections. Restoring a root's
binding is only one part of deployment relocation; file-path reconciliation and
worker configuration still need to agree with the restored filesystem.

Root saves use their own durable browser journal and caller UUID. Recovery
checks the original immutable revision before retrying the exact binding,
including after a later relocation or mount loss. It never silently checks and
accepts a replacement directory. Rejected changes require **Review current
root**, which reloads the saved definition so a replacement folder can be
checked again. A successful save remains confirmed if refreshing the view fails.
Expand **Media root history** to see prior names, states, paths and reasons;
technical directory identities are separately expandable.

## Metadata rules and direct file imports

Open a saved collection's **Edit metadata rules** section. Enable the policy,
choose whether it applies to file scans, and configure scene and image rules
separately. A new policy starts disabled. Saving records rules for future
processing; it does not immediately reapply metadata to existing library items.

Each mapping chooses a target from the backend's scene/image field schema and
either a plain jq expression or a fixed value. Expressions do not need JSON
escaping. Strings, dates, ratings and booleans use typed controls; structured
values use one JSON value. Fixed performer, studio and tag references use the
existing library picker and store native UUIDs. Name/alias matching is optional
for performer mappings; ambiguous names remain for review. Explicit library
edits and clears stay protected.

For purchased videos or other files without scrape metadata, associate a folder,
select **Use for file scans in this folder**, and add a **Performers** mapping
with a **Fixed value**. Search and select the depicted performers once for that
folder. The filename fallback fills an empty inherited title without its
extension. This does not turn a collection's source publisher into a depicted
performer. Existing file items require the separate existing-item processing
option before rescans may apply inherited values.

Creation rules run after an item's initial fields are stored. **Skip new items
created as organized** covers API imports that supply `organized=true` in their
creation request; ordinary file scans usually start unorganized. **Mark organized
after metadata is selected** requires a successful mapping without unresolved
names. A filename-only title fallback does not mark the item organized.

When marking organized is enabled, **Required metadata** lets you choose fields
that must have values after processing. For example, require both **Title** and
**Performers** for a purchased-video folder. Proposed values count only if they
can be applied; protected existing values count, while explicit clears remain
empty. Blank text, empty lists/objects and null do not meet a requirement. A
rating of zero is a selected rating. With no fields selected, there is no extra
completeness requirement. These checks only control setting the organized flag;
they never unmark an already organized item. Previews explain missing fields.

For scans, the most specific matching folder wins. A disabled policy with scan
selection enabled masks parent-folder policies. Equal folder matches require
review. Rules bind to a collection revision: after changing its folder or source,
review and save its rules again. Retired collections keep their rules read-only
until restored. Restoring a collection does not revive old queued work or remove
revision checks; workers and policies must use its current definition.

## Testing a draft

Expand **Test with a scene or image**, search for an existing library item and
choose its file. Available files are scoped to the collection's saved media root
and folder. ZIP members are not supported by folder policies. Source captures
come from the selected item's current attachment associations and evidenced
membership in this collection revision. Repeated source slots do not duplicate
the same capture/attachment option. Distinct captures remain selectable.

Choose a source capture or test file/library data alone. **Existing item** uses
existing-item rules; **New item** simulates creation using the sample's current
fields. The draft is evaluated without saving a policy, intake record or metadata
decision. Results describe proposed values, protected choices and unresolved
names. Editing the draft or selection hides the previous result. The expandable
jq sample contains `entity`, `source` and `context`; internal plugin settings
and mapping definitions are not part of its input. Draft previews have no Apply
digest and cannot be submitted to the saved-policy Apply endpoint.

Policy saves have their own durable browser journal, scoped to the backend mount
and collection. Recovery checks the exact immutable revision before retrying,
including when another policy edit has since occurred. Rejected drafts can be
retained for correction against freshly loaded revisions. Server validation
details remain visible, and confirmed success survives a failed view refresh.
Policy history loads on expansion in pages of 25.

## Save recovery

The browser generates a collection UUID before creation and saves the exact
request in IndexedDB before sending a revision-guarded PUT. Reloading the same
collection recovers that pending request. **Check and retry saved change** first
checks the expected historical revision. If it was committed, the browser accepts
that recorded definition even if another tab subsequently edited the collection.
Otherwise it retries the original UUID and bytes. A competing revision requires
review of the current definition; it is never silently overwritten. An unchanged
definition creates no new revision.

Unconfirmed writes remain pending after connection errors. A rejected change can
be cleared explicitly before reviewing another edit. Storage is isolated by the
full backend mount URL, and competing tabs share the same pending request for a
collection. Collection creation uses PUT with a caller UUID so a lost response
cannot create a second collection through a repeated anonymous POST.

## Application API

These routes are under `/api/v3/archive`, with the application's session and
same-origin mutation protection. Producer bearer credentials do not authorize
administration. Responses use `Cache-Control: no-store`.

| Route | Behavior |
| --- | --- |
| `GET /collections` | Current definitions; optional `q`, `kind`, `state`, UUID `after`, and `limit` |
| `GET /collections/{uuid}` | One current definition |
| `PUT /collections/{uuid}` | Create with `expected_revision: 0`, or edit the expected current revision |
| `GET /collections/{uuid}/history` | Immutable definitions including reason/origin; integer revision `after` and `limit` |
| `GET /media-roots` | Current root bindings; optional `q`, `state`, UUID `after`, and `limit` |
| `GET /media-roots/{uuid}` | One root, including its local binding |
| `POST /media-roots/probe` | Read-only directory check from `server_path`; returns canonical `path` and `directory_identity` |
| `PUT /media-roots/{uuid}` | Create/edit a root with `expected_revision`, `label`, `state`, nullable checked `binding` and `reason`; optional body `uuid` must match the path |
| `GET /media-roots/{uuid}/history` | Immutable root definitions including bindings, reason/origin and time; integer revision `after` and bounded `limit` |
| `GET /collections/{uuid}/metadata-policy` | Current saved policy or null |
| `PUT /collections/{uuid}/metadata-policy` | Save a definition against expected collection and policy revisions |
| `GET /collections/{uuid}/metadata-policy/history` | Immutable policy revisions; integer `after` and bounded `limit` |
| `GET /collections/{uuid}/metadata-policy/samples/{entity}/files` | Selected entity's physical files within the saved folder; required `collection_revision`, UUID `after` and bounded `limit` |
| `GET /collections/{uuid}/metadata-policy/samples/{entity}/sources` | Current capture/attachment choices; required `collection_revision`, paired `after_capture`/`after_attachment` cursor and bounded `limit` |
| `POST /metadata-policy/references` | Resolve at most 100 fixed-reference UUIDs to current names, following merge redirects without rewriting saved rules |
| `POST /metadata-policy/draft-preview` | Read-only unsaved definition preview, pinned to current collection/policy revisions, selected entity/file and optional source capture/attachment |

Collection/root list routes retain their existing default of 50 results, with an
explicit limit from 1 to 100. History defaults to 25. Search treats `%`, `_` and
backslashes literally and only visits current definition rows. Historical URLs
and labels do not match these management searches. No capture bodies or media
library rows are read. Existing target-history lookup APIs retain their own
semantics. Unknown roots, mismatched account namespaces, invalid definitions and
invalid filters return a correctable client error without leaving a partial
collection identity.

Root writes accept the exact `binding: {path, directory_identity}` returned by
the probe, or null to unbind. The earlier write-only `server_path` input is
rejected. New bindings and reactivation verify the actual directory before
creating a revision. Invalid folders return HTTP 400 `invalid_root_binding` with
a diagnostic; competing revisions and edits to retired roots return HTTP 409.
Anonymous `POST /media-roots` remains available, but browser creation uses a
caller UUID with PUT for safe recovery. A no-op save creates no history row.

Policy history defaults to 50 results and samples to 25, with explicit API limits
of 1–100. The application uses 25. Sample queries begin with the chosen entity's
file or attachment indexes; they do not scan the whole library or load capture
payloads to populate selectors. Reverse merge ancestry is limited to 1,024
identities and fails explicitly if exceeded. Draft validation checks constants
and expressions in both scene and image rules, including the kind not currently
selected as a sample. Producer tokens cannot edit policies or use administrative
sample/reference routes.

## Reddit profile retrievals

Schema 1000106 consolidates equivalent existing Reddit retrieval definitions into
one profile source per root and destination. Account, enabled state and metadata
and translation policies must agree. Arbitrary searches, subreddit sources and
saved feeds remain independent. Ambiguous or conflicting groups are retained for
review. Canonical profile URLs use lowercase usernames.

Old definitions remain as immutable aliases for execution receipts and captured
provenance; source lists exclude them, and old collection links open the canonical
profile. The profile activity view includes its historical retrieval runs. Pending
runs keep their original URLs, requests, progress, completed windows and retry
state, and must also pass the current profile's enabled-state and destination
checks. The migration does not claim that a scrape completed.

New producer calls expand a profile into the configured standard six Reddit
passes. `/collections/lookup` resolves each pass to the one current source and
returns its exact `retrieval_url`. Run requests retain that optional field and
validate it against the subscribed profile. Each pass has separate coalescing and
completion coverage, while the profile permits only one running pass at a time.
Backfill completion still requires all relevant passes. Current-source edits
continue to use normal revision checks; they do not rewrite immutable receipts.

When migrating an external scheduler, an owner can attach an existing queued or
deferred run with `PUT /api/v3/archive/runs/{uuid}/subscription`, passing
`expected_run_revision` and `subscription_revision`. This checks both revisions
and preserves the original request, windows, progress and retry deadline. The
immutable association makes a lost response safe to retry. Select only audited
automatic runs; manual and initial imports keep their independent intent.
