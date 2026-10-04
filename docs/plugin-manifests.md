# Versioned plugin manifests

All plugins target the v3 plugin API by declaring `apiVersion: 3` in their
`<plugin-id>.yml` manifest. They do not need to implement v2.5 plugin APIs,
settings types, browser scripts, or fallback behavior. Plugin IDs still come
from filenames; `version` is the plugin's release version and is independent
of `apiVersion`.

```yaml
apiVersion: 3
name: Catalog example
version: 1.0.0
interface: raw
exec: [python, "{pluginDir}/catalog.py"]
hooks:
  - name: After scene edits
    triggeredBy: [Scene.Update.Post]
settings:
  mappings:
    type: JSON
    editor: JQ_MAP
    default:
      title: .stash.title
```

The versioned manifest has a strict parser. Missing or unsupported versions,
unknown fields and legacy UI injection fields fail with a load error. Rejected
manifests cannot register tasks, hooks or browser modules. **Settings → Plugins**
shows the manifest path and reason; `pluginLoadErrorsV3` exposes the same results
from the latest reload. Correct or remove a manifest and reload plugins to clear
its diagnostic. Saved settings are retained, including for rejected plugins.

Unversioned plugins require an explicit port to this contract. The frozen
`v2.5-compatible-final` release retains their old runtime; the native application
does not adapt them.

## Backend and browser plugins

Backend plugins use `exec`, `interface`, `tasks` and `hooks`. These execute on
the server and can react to successful operations from v3, other API clients,
and background jobs. Versioning the manifest does not tie hooks to an open
browser or change their after-commit semantics.

Browser plugins optionally declare `ui.entry`, `ui.assets`, `ui.requires`
and `ui.csp`. `ui.entry` is the ESM registration module's path under the
plugin's assets endpoint. Legacy `ui.javascript` and `ui.css` injection fields
are rejected in versioned manifests. A plugin without `ui.entry` can still
have backend tasks, hooks, and native settings controls.

The browser host's `host.version` versions its JavaScript registration API;
it remains `1`. It is separate from manifest `apiVersion: 3`.

## Supported API

The v3 UI and browser host use:

- `pluginsV3` / `PluginV3` for discovery and management, including `api_version`.
- `pluginTasksV3` / `PluginTaskV3` for runnable tasks.
- `pluginSettingsV3` / `PluginSettingV3` for definitions and effective values.
- `updatePluginSettingsV3` for validated, atomic settings patches.

The old `plugins`, `pluginTasks`, `pluginSettings` and `updatePluginSettings`
fields and their settings/discovery types have been removed. Native task
execution, entity mutations, jq evaluation and committed hooks remain available.
Plugin asset URLs continue to serve ESM modules and associated files; the
`/plugin/{id}/javascript` and `/plugin/{id}/css` concatenation endpoints are gone.
Use `ui.csp` to declare any additional browser resource origins.

`type: JSON` settings persist native objects, arrays, scalars or null.
`editor: JQ_MAP` restricts them to field/expression objects. Existing mapping
overrides saved as JSON text are read as objects after a plugin upgrades;
new writes use objects. This is a saved-data migration, not a v2.5 runtime
contract. See [settings and jq mappings](plugin-settings.md) for examples.

Older Stash servers reject `apiVersion: 3`; upgrade the backend before
installing these packages. The plugin repository must select its v3 schema
when validating a versioned manifest, independently of its legacy schema.

V3 browser pages can share the host's React runtime, form controls and navigation,
and call declared `operations` through `pluginQueryV3` / `pluginMutationV3`.
`PluginV3.operations` exposes operation names, descriptions and `QUERY` / `MUTATION`
kinds. This is part of the independent v3 contract; no legacy representation is
required. See the [browser host guide](../ui/v3/docs/plugin-host.md#backend-queries-and-mutations)
for manifest syntax, execution arguments, limits and the read-only authoring contract.

## Durable native intake notifications

Native file intake sends scene/image and source-gallery notifications after its
registration transaction commits. `hookContext.eventId` identifies the same
notification across worker retries. Delivery is at least once: use that identity
when a plugin needs to deduplicate external effects. A plugin failure cannot
undo committed media or source associations; it leaves the intake effects pending
or failed for inspection. Native archive consistency does not depend on a plugin.

These calls report plugin errors to the persisted worker and have a two-minute
timeout. Worker cancellation stops delivery before shutdown. Ordinary API edit
hooks retain their existing execution path until the general durable notification
conversion is complete; `eventId` is absent on those calls.
