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

Set `apply_to_items_matching_filters: true` with the entity's `*_filter_ast`
or `find_filter.q` to queue a selection. Pagination is ignored; matching IDs
are captured before admission. Background work commits each item separately.
Inspect `findJob` for progress and failures. Cancellation or failure does not
undo earlier committed items. This existing general bulk queue is process-local;
this API change does not make it durable across server restarts.

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
The monitor refreshes on success, failure or cancellation. Missing or inconsistent
acknowledgments produce an error instead of a success callback.

The audited manual host clients have [native replacements](../integrations/library/README.md).
Deploy those helpers with the native API at cutover. The compatible production
scripts remain unchanged until then; passing the new selection to an old server
fails GraphQL validation before executing a mutation. Installed Catalog Metadata
and the audited n8n workflows do not call the removed bulk fields.
