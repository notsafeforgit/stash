# Migrating metadata policies

Legacy plugin settings and folder defaults are migration inputs. Native rules
live in collection metadata policies and run through core services. No imported
rule requires the old plugin or an external catalog database at runtime.

An import has a stable request UUID, one reviewed collection revision, an expected
current policy revision, a proposed native definition, retained source values,
and a conversion disposition for every source value. The API publishes the policy
and its receipt in one transaction. It does not scan files, edit existing media,
create performers, or grant producer access.

## Retaining source settings

The `stash_ingest.metadata_policy_import.snapshot` helper accepts the original
`config.py` bytes, plugin-only manifest defaults and saved overrides as JSON
bytes, the plugin version and a fixed capture timestamp. Python settings are read
as literal assignments; the module is never imported or executed. Nonliteral
configuration requires explicit conversion. Keep full Stash configuration and
credentials in the protected migration backup rather than passing them as plugin
settings.

The snapshot retains each layer under `python/name`, `declared/name` and
`saved/name`, together with the input file digests. `effective_settings` applies
those layers in that order, including the old JSON-string list/mapping settings.
Overridden values remain evidence. Unknown settings also remain present and must
receive a disposition. No automatic rewrite of arbitrary legacy jq is attempted:
native mappings use the documented source, entity and intake context.

Reviewed relationship mappings can use `reference_names: true` for performer,
studio, tag or group names, including supported aliases. The migration client
preserves that option in constants and jq mappings, and omits false switches
when computing the native input digest. Previously saved native `performer_names`
rules remain readable; the two options cannot both be enabled. Invalid switch
types and name matching on scalar fields are rejected before submission. The
native policy service still validates the target schema and reports ambiguous
or missing names when a rule is previewed or applied. Importing a rule does not
resolve those future matches or create missing entities.

Each disposition has an `action` and a nonempty `reason`:

| Action | Meaning |
| --- | --- |
| `mapped` | The proposed native rule represents this setting. |
| `replaced` | Native behavior deliberately supersedes it; explain the change. |
| `retired` | The setting belongs to a retired path; retain it as historical evidence. |
| `review` | Its conversion is unresolved. The proposed policy must be disabled. |

For example, export mappings targeting an old catalog writer can be retained as
retired settings because native edits already create core field decisions. A
custom export expression may encode another intent and needs review. The old
`cover_image` organized requirement must not silently disappear: it is outside
the native metadata target schema, so its replacement needs an explicit decision.

## Folder evidence and identity

Existing selected `folder.nfo` documents remain in native document storage with
their exact original bytes and parser evidence. Import references use both the
source UUID and selected head UUID. The server verifies the selection and the
literal canonical folder path against the target collection's path prefix. The
historical source collection and its revisions are not rewritten.
Some retained folder documents also reference a historical catalog post. That
association remains provenance and does not disqualify the document as a folder
default. Its selected head and exact folder path establish the applicable scope;
an unrelated post document or another folder's default is rejected.

Read nested actor names from original XML, not a flattened parser summary. Resolve
explicit names against canonical names and aliases; retain all ambiguous
candidates for review. Fixed native mappings use selected portable UUIDs. A
publisher account is not evidence that its owner is depicted in every file.

An enabled imported policy requires a target collection with a root/folder scope.
Register or review that scope separately. Importing a disabled policy can retain
unfinished choices before a local root is bound. Current root registration and
worker activation remain independent operations.

## Preview, apply and recovery

These routes require application authentication, not producer bearer tokens:

| Route | Result |
| --- | --- |
| `POST /api/v3/archive/metadata-policy-imports/preview` | Read-only plan for a complete import binding. |
| `POST /api/v3/archive/metadata-policy-imports` | Atomically apply `{binding, expected_plan_sha256}`. |
| `GET /api/v3/archive/metadata-policy-imports/{uuid}` | Original receipt and retained binding. |
| `GET /api/v3/archive/collections/{uuid}/metadata-policy-imports` | Bounded receipt page, with `after` UUID and `limit` up to 100. |

The binding contains `uuid`, `policy`, `document`, `dispositions` and
`folder_sources`. `policy` is a complete native metadata-policy input with
`origin: "migration"`. The document format is `legacy-metadata-policy`, version 1;
its fields are `plugin_version`, `captured_at`, `source_files` and `values`.
Dispositions must cover exactly the document's value keys. Folder references are
objects containing `source_uuid` and `head_uuid`.

Use the standard-library client with a frozen binding file:

```sh
python -m stash_ingest.metadata_policy_import --binding binding.json
python -m stash_ingest.metadata_policy_import --binding binding.json \
  --endpoint http://127.0.0.1:8010 > reviewed-plan.json
python -m stash_ingest.metadata_policy_import --binding reviewed-plan.json \
  --endpoint http://127.0.0.1:8010 --apply --expected-sha256 REVIEWED_PLAN_DIGEST
```

The client reads `STASH_API_KEY`, or a variable named with `--api-key-env`. It
checks the returned input/plan hashes and selected scope before acknowledging.
The input limit is 1 MiB, with at most 256 retained values, 32 source digests and
16 folder references. Policy definitions retain their existing 128 KiB limit.

The plan binds the collection, prior policy, referenced entity revisions and
selected folder heads. Changes before application require a new preview. After
application, repeating the same binding and digest returns the original receipt,
even if a later edit changes the current policy or folder selection. Reusing its
request UUID for different contents is rejected. A failed transaction leaves
neither a partial rule nor an incomplete receipt.

Receipts and folder references are normal native database tables and accompany
database backup/export. Reopening the database checks their relationships and
source/plan hashes. Anonymisation removes private settings and migration evidence.
These guarantees do not by themselves establish that every historical rule has
been assessed or migrated; final production reconciliation must account for all
retained configuration and folder inputs.
