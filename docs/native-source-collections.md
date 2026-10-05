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
Worker activation and metadata policy remain separate operations. Root creation,
mount review, policy editing and manual folder/batch performer controls still
need native management screens; this page does not imply their completion.

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

Collection/root list routes retain their existing default of 50 results, with an
explicit limit from 1 to 100. History defaults to 25. Search treats `%`, `_` and
backslashes literally and only visits current definition rows. Historical URLs
and labels do not match these management searches. No capture bodies or media
library rows are read. Existing target-history lookup APIs retain their own
semantics. Unknown roots, mismatched account namespaces, invalid definitions and
invalid filters return a correctable client error without leaving a partial
collection identity.
