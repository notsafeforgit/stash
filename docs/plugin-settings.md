# Plugin settings and jq mappings

Plugins declare settings in their YAML manifest. Backend and v3 browser plugins
share the same definitions and persisted configuration; no `ui.entry` is needed.
Expand a plugin in **Settings → Plugins**, edit, then **Save**. Failed saves
preserve the draft. Expression previews evaluate sample JSON without saving or
running hooks.

Mapping settings use rows with separate **Target field** and **jq expression**
inputs. Write jq directly, including quotes and line breaks; no surrounding JSON
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
    default:
      title: .catalog.title // empty
```

Existing `type`, `displayName`, and `description` retain their meanings. Optional
`default` must match the type. Saved false, zero and empty strings override it.
Defaults are resolved on read, not written to config.

V3 types are `STRING`, `NUMBER`, `BOOLEAN`, and `JSON`. JSON values persist as
native objects, arrays, scalars or null, independently of the legacy settings
contract. Editors are `TEXT`, `TEXTAREA`, `SELECT`, `JSON`, `JQ`, `JQ_MAP`.
`SELECT` requires distinct options with string `value` and `label`. `JQ`
compiles an expression; `JQ_MAP` validates an object of nonempty target names
and jq expressions. JSON editors can also validate text for an explicitly
declared `STRING` setting. Plugins still validate domain-specific values such
as allowed target fields or ranges.

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
introspection. Unversioned plugins are adapted into this API; their existing
string-based JSON settings remain strings. Native mapping settings also read
previously saved JSON text overrides, preserving them during migration.

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

## v3 browser host

The host has additive APIs within version 1:

```js
const { definitions, values } = await host.settings.get();
await host.settings.update({ enabled: true });
await host.settings.update({}, ["mappings"]); // reset
const stream = await host.expressions.jq(".catalog.title", input);
const mapped = await host.expressions.map({ title: ".catalog.title" }, input);
```

Definitions come from the manifest. `host.settings` supplies the current plugin
ID and updates the host's configuration cache after a successful save. These are
ordinary authenticated GraphQL requests, not a sandbox or new permission
boundary. Backend plugins call the same GraphQL fields directly.
