# Versioned plugin manifests

New plugins target the v3 plugin API by declaring `apiVersion: 3` in their
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

The versioned manifest has its own strict parser. Unsupported versions and
unknown fields fail with a load error; they never silently fall back to a
legacy interpretation. Omit `apiVersion` for an existing legacy manifest.
That format continues to work through a host adapter, with its existing
settings representation. Do not add v3 features to the legacy manifest or
GraphQL plugin types.

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

## API separation

The v3 UI and browser host use:

- `pluginsV3` / `PluginV3` for discovery and management, including `api_version`.
- `pluginTasksV3` / `PluginTaskV3` for runnable tasks.
- `pluginSettingsV3` / `PluginSettingV3` for definitions and effective values.
- `updatePluginSettingsV3` for validated, atomic settings patches.

These types are independent of `Plugin`, `PluginSetting`, their enums, and
their nested types. Required fields and new setting types on the v3 contract
do not change generated v2.5 plugin models. The original `plugins` and
`pluginTasks` queries expose only unversioned plugins. Legacy settings
endpoints direct versioned plugins to their v3 equivalents.

The unversioned `pluginSettings` endpoint returns `PluginSettingDefinition`
metadata with a required `options` list. This metadata does not extend the
original v2.5 `PluginSetting` type, so neither settings contract needs nullable
options to accommodate v2.5 generated clients.

The v3 API adapts unversioned plugins so they remain manageable in v3. This
adapter is the host's responsibility; v3 plugin authors do not write one.
Shared task execution, entity mutations, jq evaluation, and committed hooks
remain available to both generations.

`type: JSON` settings persist native objects, arrays, scalars or null.
`editor: JQ_MAP` restricts them to field/expression objects. Existing mapping
overrides saved as JSON text are read as objects after a plugin upgrades;
new writes use objects. This is a saved-data migration, not a v2.5 runtime
contract. See [settings and jq mappings](plugin-settings.md) for examples.

Older Stash servers reject `apiVersion: 3`; upgrade the backend before
installing these packages. The plugin repository must select its v3 schema
when validating a versioned manifest, independently of its legacy schema.
