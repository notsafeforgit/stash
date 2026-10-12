# Public library identities

Schema 1000115 completes UUID adoption at the application's library interfaces.
The GraphQL `ID` scalar carries a canonical lowercase UUID for performers,
scenes, images, galleries, studios, tags, groups, files, folders, scene markers,
gallery chapters and saved filters. The same UUID identifies the entity in
detail-page URLs, native REST responses, plugin previews and plugin hook payloads.
This is a core library feature, including when source tracking is disabled.

Clients should treat IDs as opaque strings. Query entities to obtain their IDs;
pass those strings unchanged to mutations, relationship fields and filters.
Numeric library IDs are rejected by current API operations. UUIDs are validated
against their entity kind, so a tag ID cannot be used as a performer ID.
Optional relationship fields retain their existing null/empty clearing rules.

SQLite integer keys remain internal join keys. Existing integer rows and all
previously assigned UUIDs are preserved. Merges resolve an old UUID to the
surviving identity. Deleted identities cannot resolve through a reused integer
key: writes fail, and list/filter queries treat deleted or unknown UUIDs as
nonmatching references. Filter text, labels and custom fields are not rewritten.

## Existing links and saved state

- Detail-page numeric bookmarks resolve through the identity endpoint and
  redirect to the UUID URL, preserving the current tab, search and navigation
  state. New links and media URLs contain UUIDs.
- Previously issued media URLs and frozen share capability URLs continue to
  work. Share entries retain the original entity UUID where it is unambiguous;
  an ambiguous historical entry has no library link.
- The upgrade converts saved/default filter references and the front-page/TV
  references in server configuration. Missing old references become stable
  nonmatching UUIDs instead of later selecting a reused database key.
- Existing offline downloads retain their browser storage keys and files.
  Refresh resolves an old key and verifies its original source properties
  before attaching current metadata. A missing or replaced source does not
  delete the downloaded bytes. New downloads use UUIDs.

Library UUIDs do not replace other protocols' identifiers. StashDB IDs remain
endpoint-qualified remote IDs. Gallery-dl account/post IDs, plugin names, job
tokens, fingerprints and share access tokens keep their own contracts. The
historical catalog-import format remains a one-time migration input; it is not
a supported way to address current library entities.

## Implementation and upgrade

GraphQL schema directives identify library-reference fields explicitly. The
boundary translates UUIDs to typed repository keys on input and back to UUIDs
on output; shared filter criteria use the selected entity kind. REST responses
omit internal key fields. Plugin notifications capture UUIDs within the
committing transaction, including deletions and merges.

Migration 1000115 rebuilds the identity registry to include folders, markers,
chapters and saved filters, and converts stored references. Its SQL changes,
trigger restoration, reference conversion and schema-version update commit as
one transaction. A failure leaves the previous schema intact for retry.
Configuration references publish atomically before request handling starts.
Media files are not rewritten.

Rehearse this upgrade on a database copy. Update the UI and external callers
together, including installed `stash-gallery` source-management helpers. Once
the new writer commits, do not run the previous binary against that database.
