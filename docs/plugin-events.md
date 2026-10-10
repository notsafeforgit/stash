# Backend plugin notifications

Backend plugins do not need a v3 `ui.entry`. An entry point opts into the
[browser plugin host](../ui/v3/docs/plugin-host.md); manifest `exec` and `hooks`
run on the server even when no browser is open. For example, `titleFromFilename`
and `catalogMetadata` can remain backend-only plugins.

## Subscribe to deletions and edits

Declare the events in the plugin manifest:

```yaml
apiVersion: 3
name: Library change listener
version: 1.0.0
exec:
  - python
  - "{pluginDir}/changes.py"
interface: raw
hooks:
  - name: library_changes
    triggeredBy:
      - Scene.Destroy.Post
      - File.Destroy.Post
      - Scene.Update.Post
      - File.Update.Post
      - Image.Update.Post
      - Gallery.Update.Post
      - Performer.Update.Post
      - Performer.Merge.Post
      - Studio.Update.Post
      - Group.Update.Post
      - Tag.Update.Post
```

The plugin receives `args.hookContext` through its normal input protocol:

| Property | Meaning |
|---|---|
| `type` | Event name, for example `Scene.Update.Post` |
| `id` | Numeric ID of the affected entity or file |
| `input` | Mutation input or the event-specific payload below; can be null |
| `inputFields` | Fields supplied or affected by the operation, when known |
| `parentHooks` | Optional ordered list of `{pluginId, type}` parent hooks whose API calls led to this event |

These are notifications after a successful database commit. A rolled-back
transaction or failed commit emits no notifications. A plugin cannot veto,
replace, or undo the originating operation through its return value. A plugin
can make subsequent GraphQL mutations using the supplied server connection.
Existing hook order, enable/disable settings, and recursion protection apply.

Parent hooks are propagated through the supplied plugin session. Direct user/API
operations have no parents; a caller that discards the plugin session also loses
this history. For example, a title update made by Title From Filename during
scene creation has a `Scene.Create.Post` parent. Metadata exporters can ignore
that initialization so it does not become a manual catalog override. Plugins
can also mark their own update mutations with `clientMutationId` to suppress
import/export feedback. See [settings and jq mappings](plugin-settings.md).

## Scene deletion

`Scene.Destroy.Post` already covers single and bulk scene deletion and scene
cleanup. Its `input` includes `path`, `checksum`, and `oshash`, plus the original
deletion options such as `delete_file` and `destroy_file_entry`. The deleted
scene can no longer be fetched by ID. In a bulk deletion, each deleted scene
receives its own event.

Deleting a scene and deleting its file records are separate operations. A
scene-only deletion retains its files; a file shared with another scene is
retained even when file deletion was requested.

## File deletion

`File.Destroy.Post` fires for each removed file record. It covers direct
`deleteFiles` / `destroyFiles`, file removal during scene/image/gallery deletion,
archive members, and cleanup jobs. Shared files that are retained do not emit a
file-deletion event. The payload preserves identity before removal:

```json
{
  "id": "42",
  "path": "/library/example.mp4",
  "basename": "example.mp4",
  "zip_file_id": null,
  "fingerprints": { "md5": "...", "oshash": "..." }
}
```

This event means the database entry was removed. Depending on the originating
operation, the physical file may be kept, moved to trash, or deleted. It is not
a guarantee that final filesystem cleanup completed. Do not attempt to fetch
the deleted record or assume its ID can still be used to find its path.

## Performer merges

`Performer.Merge.Post` fires once after `performerMerge` commits, with the
destination performer as `hookContext.id`. Its `input` contains `destination`
(the final identity), `previous_destination`, and `sources` (identities captured
before deletion). Every snapshot contains string `id`, `name`, `disambiguation`,
`alias_list` (strings), and `urls` (strings). Empty lists are arrays, not null.

The original profiles survive in the notification even when the merge's chosen
values discard some source aliases or URLs. Sources can no longer be queried
by ID. Failed or rolled-back merges emit nothing. This is a distinct merge
event; it does not synthesize `Performer.Update.Post` or individual destruction
events. Plugins can use it to redirect external identity references without
inferring a merge from deletions or treating shared media as identity evidence.

## Field edits

Entity `Update.Post` hooks cover every field accepted by the corresponding
update mutation, including custom fields and explicit nulls that clear values.
Bulk updates notify each affected entity. No per-field manifest declarations
are necessary. A listener can handle every update or filter `inputFields`:

```python
context = plugin_input["args"]["hookContext"]
fields = context.get("inputFields")
if not fields or "title" in fields or "custom_fields" in fields:
    # Query the committed values using context["id"].
    # Missing inputFields means the changed fields are not known.
    pass
```

For ordinary entity mutations, `inputFields` lists supplied fields, rather than
an old/new value diff. Setting a field to its existing value can still notify.
The input follows the originating mutation's shape; bulk and single inputs may
differ. `input` or `inputFields` can be absent for background operations, so
listeners must handle that case. Individual custom-field keys are nested in
`input.custom_fields`; the top-level field name is `custom_fields`.

Specialized edit actions also emit the entity update hook with `input: null`
and the affected field names:

| Action | Event | Fields |
|---|---|---|
| Save/reset scene activity | `Scene.Update.Post` | `resume_time`, `play_duration` (as requested) |
| Change scene play history/count | `Scene.Update.Post` | `play_count`, `play_history`, `last_played_at` |
| Change scene counter/history | `Scene.Update.Post` | `o_counter`, `o_history` |
| Assign a file to a scene | `Scene.Update.Post` | `files` |
| Generate/regenerate a scene cover | `Scene.Update.Post` | `cover_image` |
| Change image counter | `Image.Update.Post` | `o_counter` |
| Add/remove gallery images | `Gallery.Update.Post` | `image_ids` |
| Set/reset gallery cover | `Gallery.Update.Post` | `cover` |
| Add/remove/reorder subgroups | `Group.Update.Post` | `sub_groups` |

Groups emit only `Group.Create.Post`, `Group.Update.Post` and
`Group.Destroy.Post`. The former `Movie.*` triggers are rejected by native
plugin manifests. Scene relationship inputs use `groups` with `group_id` and
`scene_index`; background scene update payloads use that same native shape.

File edits use `File.Update.Post`. This includes moves/renames, scanned file
metadata, fingerprints, and captions. Its `input` contains `id`, `before`, and
`after` snapshots, and `inputFields` contains the changed top-level file JSON
properties. Snapshots include base file properties and image/video metadata,
with fingerprints represented as a map of type to string value and captions as
an array. A write that changes none of these properties emits no event. Multiple
writes to one file in a transaction can produce multiple ordered notifications;
each snapshot describes that write within the successfully committed transaction.

These hooks describe Stash operations, not arbitrary writes made directly to
SQLite by another process. Existing background entity hooks retain their
operation-specific coverage; this is not a general database change journal.
Plugins that mutate data in response should compare values first to avoid
repeatedly triggering themselves.
