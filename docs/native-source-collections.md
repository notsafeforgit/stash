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

Retiring a collection is permanent and preserves its definition history. Merely
creating a collection does not schedule downloads or assign performer metadata.
Worker activation and metadata policy remain separate operations. Existing
collections now expose **Edit metadata rules**, including fixed performer
selection for folder scans. **Media roots** provides folder registration and
binding review. Historical policy import and live worker activation remain
unfinished.

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
unchanged folder is offline. Retirement is permanent and preserves history.
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
review and save its rules again. Retired collections are read-only.

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
