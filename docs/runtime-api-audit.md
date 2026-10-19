# Native runtime API audit — October 2026

The maintained application contract is defined by current callers, not by the
upstream v2.5 schema. Historical database/configuration conversion remains under
the frozen legacy-upgrade-only boundary in [PROJECT.md](../PROJECT.md).

The October audit searched the current UI operations and source, bundled plugin
examples, the retained `apiVersion: 3` plugin contract, the external clients at
`integration-clients.json` revision `f3f7b6edd759783ddedf2c76ea880e890915cb7f`,
and the locally deployed host callers in `stash-ops` and `stash-integrations`.
The installed native plugin directory contained no additional plugin code.
Host configuration and credentials were not copied into this repository.

| Retired runtime surface | Actual consumers found | Native replacement |
| --- | --- | --- |
| `allScenes`, `allSceneMarkers`, `allImages`, `allGalleries`, `allPerformers`, `allStudios` | None in the checked callers | Corresponding `find*` query with explicit filter/pagination |
| `allTags` | Python, JavaScript and Go bundled plugin examples; converted together | `findTags { tags }` |
| `sceneIncrementO`, `sceneDecrementO`, `sceneIncrementPlayCount` | None in the checked callers | `sceneAddO`, `sceneDeleteO`, `sceneAddPlay`, including history results |
| `jobsSubscribe` | Tag tagger; converted to the shared completion monitor and progress stream | `findJob`/`jobHistory` for outcomes; separate lifecycle/progress subscriptions |
| Configuration password hash/sentinel | Security settings; converted together | `passwordConfigured`, write-only `newPassword`, explicit `clearPassword` |

Ordinary job IDs remain opaque GraphQL IDs/strings, now UUIDs instead of process
counters. Checked external scan/generate helpers consume the returned string;
they do not parse it as an integer. No external client rollout is needed for
these changes. API validation runs the pinned clients against a real server.

This is an inventory of the retired surfaces, not a claim that every deprecated
field is unused. Provider interchange still includes single-URL/career/alias
representations, and current scraping/plugin operations retain their tested
contracts. Remove those only after converting their actual consumers. The
retained native plugin API has a checked schema snapshot; current UI operations
are validated against the real schema. `TestRetiredRuntimeAPIsStayAbsent` prevents
accidental reintroduction of the retired roots and checks their replacements.

The real-stack browser suite uses the built Go application and a subpath reverse
proxy with a temporary library. It covers login, rejected settings writes,
in-place session recovery and durable job status after a process restart in
Chromium and WebKit. It runs as a required publication check alongside the
existing fixture browser suite. Locally run `make generate ui` then
`make test-ui-stack` with Playwright's system dependencies installed.
