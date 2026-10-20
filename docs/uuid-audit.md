# Numeric ID audit — 2026-10-20

This audit follows library identifiers through the current React client,
GraphQL schema/scalars, native REST routes, task/configuration inputs, plugin
notifications, media routes, scraper subprocess inputs and DLNA. It distinguishes
public identities from internal SQLite keys and unrelated numeric values.
It is a repository-wide static review with focused executable regressions,
not a claim that every application workflow has been exercised.

## Confirmed defects corrected

| Area | Failure | Correction and regression coverage |
| --- | --- | --- |
| Performer merge photos | The merge helper extracted an integer from a photo URL. UUID URLs fell through to an HTTP download, failing for relative URLs or authenticated deployments. | `EntityImageInput.performer_id` is a typed UUID reference. The UI sends the selected performer identity; the merge reads its photo inside the merge transaction. Real GraphQL/SQLite tests cover source/destination/clear, wrong-kind/numeric/unrelated references and conflicting image sources. |
| Performer merge names | Choosing the source name temporarily renamed it to a string containing a NUL byte. The SQL builder rejected it. This workaround addressed a uniqueness rule that the native name model no longer has. | Removed the temporary rename. Real SQLite tests choose the source name while preserving the destination name as an alias and retaining the destination UUID. |
| Merge and image-copy notifications | `previous_destination.id` and nested image-copy references could remain numeric after the top-level hook ID had become a UUID. | Capture merge profile UUIDs before deleting source rows. Convert the known nested hook reference fields, while preserving arbitrary custom values and remote identifiers. Tests include deleted source identity snapshots. |
| Identify task selections | The two “tag skipped results” fields were GraphQL strings and were parsed as integers during identification. Valid UUID selections failed. Scene selections were also converted before queueing. | Explicit `ID` fields validate the entity kind and preserve UUIDs for deferred work. Identify resolves them during execution, including tag resolution inside the write transaction. Tests cover both skip branches, redirects and deletion/key reuse. |
| Saved task selections | Configuration promotion/validation covered TV and front-page references but missed Identify defaults, source overrides, auto-tag defaults and task-dialog selections. | Extend the shared reference visitor. Promote old keys in an atomic configuration update before requests; preserve missing references as stable nonmatching UUIDs. New saves validate UUIDs. Tests cover publication failure, retry, idempotence and wrong-kind/numeric rejection. |

`TestLibraryIDSchemaCoverage` now requires every GraphQL `ID` field/argument
to declare its library kind or appear in the explicit inventory of other ID
domains. The real-server browser merge test runs through authenticated login
and a subpath reverse proxy, checks the submitted UUIDs, compares transferred
photo bytes and follows the surviving route.

## Remaining work identified

These are open findings, not completed conversions.

| Priority | Boundary | Evidence and recommended change |
| --- | --- | --- |
| High | Deferred generation/export/provider operations | [`task_generate.go`](../internal/manager/task_generate.go), [`task_export.go`](../internal/manager/task_export.go) and provider batch admission in [`manager_tasks.go`](../internal/manager/manager_tasks.go) retain converted numeric selections beyond the request. The public request accepts UUIDs correctly, but later work can still hold stale local keys. Identify's queued selection is now fixed, but [`SceneIdentifier`](../internal/identify/identify.go) still holds a scene model across its network scrape before applying results. Retain UUIDs across queue/network boundaries and re-resolve/check identity when applying work; test deletion/reuse and merges while a job waits. This risk was found by tracing code, not by changing production records. |
| Medium | Scraper subprocess protocol | [`script.go`](../pkg/scraper/script.go) still serializes local scene/gallery/image IDs and file/ZIP IDs as numeric strings. A script cannot safely reuse those as native GraphQL IDs. Convert both script inputs and matched `stored_id` outputs together, inventory installed scripts and test the process boundary. A one-sided input change would leave internal scraper matching inconsistent. |
| Medium | DLNA protocol | [`cds.go`](../internal/dlna/cds.go) emits numeric scene and category object IDs; [`dms.go`](../internal/dlna/dms.go) parses numeric scene parameters. Convert object browsing, category filters, metadata, artwork and streaming together. Preserve explicitly supported old media links and test real UPnP request/response payloads. Root IDs, pagination and ratings remain protocol values. |
| Medium | ID range predicates | The shared filter adapter translates public UUIDs to numeric `id` criterion operands, including range comparisons. UUID order is not repository-key order; missing-reference sentinels also need operator-specific treatment. Define equality/membership semantics for public identity filters, with explicit handling of existing saved predicates, before removing numeric range behavior. |

## Numeric values deliberately retained

- SQLite primary/foreign keys, repository parameters, joins, transaction-local
  model relationships and internal cache directories. These are not portable
  public identities. Replacing every `int` or `strconv.Atoi` would damage these
  layers rather than repair an API boundary.
- The shared GraphQL scalar's conversions and resolver parsing after that scalar
  has resolved a UUID. Schema annotations and transport tests establish which
  side of the boundary a value occupies.
- Previously issued numeric media links and numeric detail bookmarks, resolved
  through the dedicated media/identity readers. New application mutations still
  reject numeric library IDs.
- Historical database/catalog readers and their frozen ordinals/local mappings.
  This audit does not change those upgrade formats or rerun imports.
- Source-site numeric account/post identifiers, endpoint-qualified provider IDs,
  hashes, capture positions, review sequence/cursor numbers, schema versions,
  revisions and lease fences. They belong to different identity domains.
- Client pointer/timer/history sequence IDs, drag/drop insertion indexes,
  pagination, dimensions, durations and counters. The client review found no
  additional direct entity-ID number coercion beyond the already-fixed job
  monitoring code; the merge failure was in the server's URL interpretation.

Ordinary bulk edits already retain UUID targets and verify their identity at
each checkpoint. Auto-tagging already retains public selections and re-resolves
them during execution. Those implementations provide the pattern for the
remaining deferred-task conversions.

No database schema change is required for these fixes. Production validation
must not create test performers, merge real records, replay catalog imports or
run full-library reconciliation.
