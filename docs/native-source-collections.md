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
selection for folder scans. Root creation, mount review and historical policy
import remain unfinished; this page does not imply their completion.

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

Policy history defaults to 50 results and samples to 25, with explicit API limits
of 1–100. The application uses 25. Sample queries begin with the chosen entity's
file or attachment indexes; they do not scan the whole library or load capture
payloads to populate selectors. Reverse merge ancestry is limited to 1,024
identities and fails explicitly if exceeded. Draft validation checks constants
and expressions in both scene and image rules, including the kind not currently
selected as a sample. Producer tokens cannot edit policies or use administrative
sample/reference routes.
