# Plugin settings and jq mappings

Plugins declare settings in their YAML manifest. Backend and v3 browser plugins
share the same definitions and persisted configuration; no `ui.entry` is needed.
Expand a plugin in **Settings → Plugins**, edit, then **Save**. Failed saves
preserve the draft. Expression previews evaluate sample JSON without saving or
running hooks. Settings with a declared entity preview also offer a scene or
image picker and **Load entity data**. The loaded input remains editable;
**Test expression** always uses the unsaved expression draft. Editing that draft
or the input hides the previous result.

Mapping settings use rows with separate **Target field** and **jq expression**
controls. When the plugin declares destination fields, **Target field** is a
dropdown with only those choices. The selected field shows its stored name,
expected value format and help text. Already-used targets cannot be selected
again. Unsupported targets saved by an older plugin remain visible for repair;
they cannot be tested or saved until replaced or removed. Write jq directly,
including quotes and line breaks; no surrounding JSON
object or string escaping is needed. For example, target `title` can use:

```jq
select(.fields | index("title"))
| .stash.title
```

Add or remove rows to change the map. Removing every row saves an empty object;
it does not reset the setting to its manifest default. Each target must be unique
and every row needs an expression. Existing native objects and saved JSON text
open in the same editor. The API and persisted configuration retain their existing
JSON representation.

Preview sections use the shared Base UI collapsible. Opening a preview loads
the picker; collapsing and reopening it preserves the sample and result.

## Manifest API

```yaml
apiVersion: 3
name: Example
settings:
  enabled:
    type: BOOLEAN
    displayName: Enable synchronization
    default: false
  retries:
    type: NUMBER
    default: 2
  direction:
    type: STRING
    editor: SELECT
    default: import
    options:
      - { value: import, label: Catalog to Stash }
      - { value: both, label: Both directions }
  notes:
    type: STRING
    editor: TEXTAREA
  mappings:
    type: JSON
    editor: JQ_MAP
    description: Target fields mapped to jq expressions
    mappingTargets:
      - name: title
        label: Title
        type: String
        description: The destination title
    default:
      title: .catalog.title // empty
```

Existing `type`, `displayName`, and `description` retain their meanings. Optional
`default` must match the type. Saved false, zero and empty strings override it.
Defaults are resolved on read, not written to config.

V3 types are `STRING`, `NUMBER`, `BOOLEAN`, and `JSON`. JSON values persist as
native objects, arrays, scalars or null. Editors are `TEXT`, `TEXTAREA`, `SELECT`, `JSON`, `JQ`, `JQ_MAP`.
`SELECT` requires distinct options with string `value` and `label`. `JQ`
compiles an expression; `JQ_MAP` validates an object of nonempty target names
and jq expressions. JSON editors can also validate text for an explicitly
declared `STRING` setting. Plugins still validate domain-specific values such
as allowed target fields or ranges.

`mappingTargets` is an optional, nonempty list for `JQ_MAP`. Each target has a
distinct nonblank `name`, a human-readable `label`, a `type` describing the
destination value format, and an optional `description`. Declaring this list
restricts mapping keys in both defaults and settings updates, including direct
API updates. An omitted list allows arbitrary destination keys for generic
plugins. `type` is descriptive, not a general JSON Schema interpreter: the
destination API still validates evaluated values. Plugins should derive these
definitions from their destination schema and enforce their allowed fields
when executing, including settings written through older configuration APIs.

Versioned manifests require the v3 backend and have no v2.5 plugin compatibility
requirement. See [plugin manifest versions](plugin-manifests.md). Upgrade Stash
before installing packages that declare `apiVersion: 3`.

## GraphQL API

```graphql
query {
  pluginSettingsV3(plugin_id: "catalogMetadata") {
    definitions {
      name display_name description type default_value editor
      options { value label }
      mapping_targets { name label type description }
      preview { entity description }
    }
    values
  }
}

mutation {
  updatePluginSettingsV3(
    plugin_id: "catalogMetadata"
    input: { dry_mode: true }
    reset: ["scene_import_mappings"]
  )
}
```

`updatePluginSettingsV3` validates the whole patch, serializes updates, preserves
unrelated settings, and persists the result. Failed validation or persistence
leaves the previous configuration intact. `reset` removes saved overrides so
defaults apply again. Unknown names and a name in both input and reset are errors.
The result includes defaults. Existing `configurePlugin` remains a whole-map
replacement; its behavior and `configuration.plugins` are unchanged.

`pluginsV3.settings` exposes these definitions through the independent
`PluginSettingV3` type. API availability can be discovered through GraphQL
introspection. Only manifests declaring `apiVersion: 3` can load. Native mapping
settings still read previously saved JSON text overrides, preserving those values
during migration without restoring an unversioned runtime adapter.

## Entity preview providers

A v3 `JQ` or `JQ_MAP` setting can declare a read-only context provider:

```yaml
  scene_import_mappings:
    type: JSON
    editor: JQ_MAP
    preview:
      entity: SCENE # or IMAGE
      description: Test custom mappings against the selected item and its catalog.
    default: {}
```

The picker searches titles and file paths and shows the item ID. Loading data
calls `pluginSettingPreviewV3(plugin_id: ID!, setting: String!, entity_id: ID!)`.
The backend validates the plugin and declared setting, then runs its executable
with these arguments (raw interface JSON):

```json
{"mode":"preview","setting":"scene_import_mappings","entity_type":"scene","entity_id":"42"}
```

The plugin must return its jq input as `{"output": <context>, "error": null}`.
Return an error for missing entities, missing files or unavailable data sources.
The handler must only read data: it must not invoke imports, exports, hooks,
relation creation, configuration saves or other write paths. It loads context
using saved settings; the expression draft is evaluated separately by the pure
jq API and does not need to be saved or sent to the executable.

Previews have a 30-second deadline and a 1 MiB serialized response limit.
Disabled plugins and undeclared preview settings are rejected. This is a
read-only provider contract for trusted plugin code, not an executable sandbox
or an enforcement boundary on the plugin's filesystem/API permissions. The
jq evaluator itself remains pure. Tests should verify that provider handlers
never call their normal write paths.

The result is expression output, not a complete import/export dry run or a
validation of the destination field types. Provider descriptions should explain
any simulated event inputs and any normal processing omitted from the preview.

## jq API

```graphql
query Evaluate($input: Any, $mappings: Map!) {
  pluginEvaluateMappings(input: $input, mappings: $mappings)
}
```

Example variables:

```json
{
  "input": {"catalog": {"title": "Example"}, "stash": {"performers": [{"name": "Alice"}]}},
  "mappings": {
    "title": ".catalog.title | ascii_upcase",
    "actors": "[.stash.performers[].name]",
    "unchanged": "empty",
    "cleared": "null"
  }
}
```

The result is `{"title":"EXAMPLE","actors":["Alice"],"cleared":null}`. Each
expression gets the complete input independently. An empty stream omits the key;
null, false, zero, empty strings and arrays are real values. Each expression must
produce at most one value. Wrap a stream in `[...]` to return an array. Any error
or multiple output fails the whole map. This API only evaluates JSON; plugins
interpret the target fields and perform writes.

`pluginEvaluateJQ(expression: String!, input: Any): [Any]!` returns the raw stream,
including nulls. Expression `[1, 2]` returns `[[1, 2]]`, `null` returns `[null]`,
and `empty` returns `[]`.

The interpreter is [gojq](https://github.com/itchyny/gojq), embedded in Go; no jq
binary or shell is required. Module loading, process environment, and external
inputs are disabled. Limits: 250 ms execution context deadline, 16 KiB per
expression, 1 MiB serialized input/output, 1,024 streamed results, 128 mappings.
Mappings share one deadline. These are execution and serialized-size limits,
not a hard process memory limit. Integer precision is retained in the backend;
browser JavaScript retains its usual JSON number precision limits.

Stash also provides the pure `utc_date` filter. It accepts a calendar date
(`YYYY-MM-DD`) or an RFC3339 timestamp with an explicit timezone, including
fractional seconds, and returns the UTC calendar date. For example,
`"2026-09-29T22:37:08.123456789-07:00" | utc_date` returns `"2026-09-30"`.
Date-only input keeps its day. Surrounding whitespace is ignored; null, empty
and whitespace-only input return null. Invalid dates, timezone-less timestamps,
non-string values and dates outside years 0001–9999 produce an error. Input is
limited to 64 bytes after trimming. No host timezone is assumed and an invalid
date is never rolled forward into another month.

Use `.published_at | utc_date | select(. != null)` to omit missing dates while
reporting invalid ones. A direct `utc_date` result of null remains an explicit
null value under the ordinary mapping rules. The same filter is available to
native collection policies and plugin/browser jq callers; it does not modify
the original input or load any external data.

## v3 browser host

The host has additive APIs within version 1:

```js
const { definitions, values } = await host.settings.get();
const input = await host.settings.preview("scene_import_mappings", "42");
await host.settings.update({ enabled: true });
await host.settings.update({}, ["mappings"]); // reset
const stream = await host.expressions.jq(".catalog.title", input);
const mapped = await host.expressions.map({ title: ".catalog.title" }, input);
```

Definitions come from the manifest. `host.settings` supplies the current plugin
ID and updates the host's configuration cache after a successful save. These are
ordinary authenticated GraphQL requests, not a sandbox or new permission
boundary. Backend plugins call the same GraphQL fields directly.
