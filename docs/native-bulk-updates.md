# Native bulk updates

`bulkSceneUpdate`, `bulkSceneMarkerUpdate`, `bulkImageUpdate`,
`bulkGalleryUpdate`, `bulkPerformerUpdate`, `bulkStudioUpdate`, `bulkTagUpdate`
and `bulkGroupUpdate` return `BulkUpdateResult!`:

| Field | Explicit ID selection | Filter selection |
| --- | --- | --- |
| `status` | `COMPLETED` | `QUEUED` |
| `job_id` | `null` | Job ID to inspect |
| `selected_count` | Number of selected IDs | Number of matches when admitted |
| `updated_ids` | IDs committed in the transaction | Empty; admission does not claim completed edits |

Explicit IDs execute in one transaction. A failed item rolls back that entire
request and returns a GraphQL error, without successful-edit notifications.
An empty explicit selection completes with zero items.

Input translation stays in the API. The shared `pkg/bulkupdate` operations own
execution for both immediate edits and background workers, including validation,
relationship handling and custom fields. Its versioned command codec preserves
omitted fields, explicit clears, empty collections, numeric zero and date
precision. Unknown versions/fields, invalid relationship modes and oversized
commands are rejected. Changes to durable command semantics require an explicit
version change, not reinterpretation of already accepted edits.

Set `apply_to_items_matching_filters: true` with the entity's `*_filter_ast`
or `find_filter.q` to queue a selection. UI pagination and sort are ignored;
admission queries bounded pages in stable ID order. One SQLite writer transaction
captures the command, target UUIDs, relationship UUIDs and job record. A failed
admission accepts nothing. The accepted selection never expands when its filter
later changes. Removed or merged identities fail explicitly; a reused local key
cannot redirect an accepted edit.

Schema 1000126 checkpoints these ordinary bulk commands independently of source
tracking. Each item commits its edit and checkpoint together. Restart resumes
only remaining items, preserving the same job UUID and date/clear semantics.
Recovery advances an execution generation so an older process cannot commit an
edit, notification acknowledgment or terminal outcome after being superseded.
Server shutdown pauses this work. Explicit cancellation is durably recorded before
the executor is signalled, and survives a crash during cancellation.

Enabled post-hooks are captured with public UUID inputs and parent hook ancestry
at admission. Delivery follows the committed edit, uses a stable event ID, and
retries at most three times, including interrupted attempts. Delivery is at least
once: plugins must deduplicate their own effects by event ID. A failed hook does
not repeat the edit or prevent later items from running. Domain consistency
never depends on a plugin acknowledgment.

Inspect `findJob`, the queue and Settings → Tasks for progress and failures.
An infrastructure/checkpoint error leaves the job `PAUSED`, with its recovery
records intact and an error explaining that the next server start will resume
it. Paused work remains cancellable. Unsupported saved command versions fail
explicitly without executing or blocking other accepted jobs. Cancellation or
failure does not undo earlier committed items. Other ordinary tasks without
checkpoints still become `INTERRUPTED` at restart; they are not replayed.

Recovery storage has explicit admission limits:

- At most 32 unfinished bulk commands and 250,000 retained targets globally.
- At most 100,000 targets per command; narrower filters can split larger work.
- At most 64 KiB each for the command, prepared notification and relationship
  identity list; the command also permits at most 1,000 distinct relationships.
- At most 20 error samples and 32 KiB of sample JSON per command. Counts include
  every failure; individual sampled messages are limited to 1 KiB.

Terminal status and deletion of the command/targets commit together. Only the
last 1,000 compact ordinary task outcomes remain, with descriptions bounded to
1 KiB and errors to 16 KiB. Unfinished work is never expired behind the user's
back; its global admission budgets bound retained storage. Freed SQLite pages
are reused by subsequent jobs; completing each job does not run a whole-database
`VACUUM` or maintain a separate journal/cache file. Anonymized support exports
remove these records through the ordinary job-history cascade.

```graphql
mutation AddTag($input: BulkSceneUpdateInput!) {
  result: bulkSceneUpdate(input: $input) {
    status
    job_id
    selected_count
    updated_ids
  }
}
```

The duplicate `bulk*UpdateJob` fields, `bulkMovieUpdate`, `bulkMovieUpdateJob`
and `BulkMovieUpdateInput` are removed. The old entity-array result and `"sync"`
sentinel no longer describe these mutations. Use groups for movie collections.
The separate `scenesSetDateFromFileMTime` and `imagesSetDateFromFileMTime`
operations retain their existing scalar contract pending their own conversion.

All seven UI sheets use this result. Completed edits refresh immediately;
queued work keeps a client-owned completion monitor after the sheet closes.
The monitor refreshes on success, failure, cancellation or a recoverable pause.
Missing or inconsistent
acknowledgments produce an error instead of a success callback.

The audited manual host clients have [native replacements](https://github.com/notsafeforgit/stash-integrations/blob/main/integrations/library/README.md).
Passing the new selection to a historical server fails GraphQL validation before
executing a mutation. Installed Catalog Metadata
and the audited n8n workflows do not call the removed bulk fields.
