# Native builds and deployment

Development continues on `develop` after the approved rewrite merge. The native
application is running in production. Routine updates replace its pinned image;
they do not repeat the original cutover or open the frozen compatible database.
Keep build/rehearsal databases separate from the live library. The original
compatible source and image digests remain preserved in the
[release manifest](releases/v2.5-compatible-final.json).

The main `notsafeforgit/stash` image contains the application and complete native
runtime, including Jellyfin FFmpeg, GPU drivers, image codecs and `uv`. A single
publisher builds and smoke-tests it. No `stash-s6` dispatch is needed. The image
uses `/config` directly, without upstream layout migration or compatibility mode;
see [container setup](container.md) for the one-time mount/path conversion.
Production images must be built and published by GitHub Actions and pulled from
GHCR. A local build, or uploading a locally built image to GHCR, does not satisfy
the owner's delivery requirement. Report push, publication and deployment as
separate outcomes, with the run URLs and actual running registry digest.

Personal automation has a separate build and release in `stash-ops`. Updating
its flows, worker deployment or backup policy does not publish a Stash image.
Only install those optional services when needed; see [optional integrations](optional-integrations.md).

## 1. Publish and verify the Stash image

Complete the [development checks](../ui/v3/docs/development.md#validation),
commit incremental changes, and push `develop`:

```bash
stash_sha="$(git rev-parse HEAD)"
stash_build_tag="develop-$(printf '%s' "$stash_sha" | cut -c 1-7)"
git push origin develop
gh run list --repo notsafeforgit/stash --workflow ghcr-publish.yml \
  --branch develop --commit "$stash_sha" \
  --json databaseId,headSha,status,conclusion,url
```

The [publisher](../.github/workflows/ghcr-publish.yml) calls the shared
[native validation workflow](../.github/workflows/native-ci.yml). It generates
bindings and validates/builds the UI once, shares those exact generated files
and app/login/share/offline assets with the Go jobs, and compiles Linux amd64 in
parallel with backend validation. SQLite tests, other Go tests and Go lint run
as separate jobs without gallery-dl. SQLite's main package is split across four
isolated runners, starting alongside the UI build because they do not depend on
its generated assets. Each discovers the actual tagged Go test registry and selects
its share of whole top-level tests, including every subtest and fuzz seed case.
The first shard also runs every SQLite subpackage. No handwritten test allowlist
is used, and each shard verifies that all its selected tests finished exactly
once. Tests remain sequential within each runner because some share global
configuration and database fixtures. All four shards must pass the publication
gate. Each job summary lists its slowest tests; the `sqlite-tests-*` artifacts
retain complete JSON output and timings for seven days, including on failure.
Each shard has its own Go build/result cache so all four selections can be
retained. Go's normal result cache remains enabled, and summaries distinguish
reused results from fresh execution. Set `GOFLAGS=-count=1` when measuring uncached
timings locally; cached results still contain the events needed by the coverage
check.
Only the other backend job installs media fixture tools; SQLite tests do not
launch FFmpeg or image converters.

The external Python packages run their own
unit tests and release workflow in `stash-integrations`. A separate client job
runs the focused real-server compatibility tests when API, model, schema or other
contract dependencies change. It installs the exact client commit pinned in
`integration-clients.json`; missing runtimes fail explicit compatibility runs.

Checks are selected against **every changed path since a successful ancestor
publish**. Frontend-only/Markdown changes skip backend and client checks; known
standalone media-engine changes skip client checks. Unknown paths, missing
history, and manual dispatches select everything. UI validation, embedded-entry
tests and Linux compilation always run. Failed, cancelled or unexpectedly skipped
selected jobs cannot publish; failed intervening pushes do not reset the baseline.

The required real-stack browser job runs that Linux binary behind a subpath
reverse proxy with a temporary library. Chromium and WebKit cover login, failed
settings writes, in-place session recovery and job history after restart. A
failure blocks publication. The broader fixture browser regressions retain their
separate workflow.

Pushes to `develop` no longer repeat this work in the older Build and
Lint workflows. Native PRs use the same validation workflow; `develop` retains
its downloadable development release using the publisher's validated artifacts.

Dependency caches cover pnpm's actual configured store, uv downloads/builds, Go
modules, and per-suite incremental Go compilation. BuildKit caches container
libraries independently of the application binary; base images are still
pulled. Dispatch with `refresh_container_dependencies=true` to rebuild those
layers, including their package installations, without the cache. Generated
bindings and embedded UI are fresh artifacts for each run, not reused builds
with stale revision metadata.

It publishes `ghcr.io/notsafeforgit/stash:develop` and the seven-character
revision tag above. The moving tag alone is not a deployment identity.
The immutable `v3.0.0-dev` source tag anchors native development version names;
`git describe` adds the subsequent commit count and revision. Frozen compatible
tags remain unchanged.
Both container and downloadable-binary builds set `UPDATE_REPO` to the current
GitHub repository, so the application's release check targets this fork.

Select the run with the exact `headSha`, wait for success, then resolve the
revision tag:

```bash
read -r -p "Stash publish run ID: " stash_run
gh run watch "$stash_run" --repo notsafeforgit/stash --exit-status
gh run view "$stash_run" --repo notsafeforgit/stash \
  --json headSha,conclusion,url
stash_digest="$(skopeo inspect --no-creds \
  "docker://ghcr.io/notsafeforgit/stash:$stash_build_tag" --format '{{.Digest}}')"
skopeo inspect --no-creds "docker://ghcr.io/notsafeforgit/stash@$stash_digest" \
  --format '{{.Digest}} {{index .Labels "org.opencontainers.image.revision"}}'
```

Require the full intended source revision in the label. Record the digest;
revision tags may be removed by normal cleanup. Frozen compatible tags and their
content manifests are excluded from that cleanup.

## 2. Deploy the verified native image

The publisher tests the pushed revision digest before promoting `develop`.
Tests compare the included binary with the validated Actions artifact and exercise
non-root startup, UID/GID overrides, supplementary groups, command arguments,
offline operation, image/video tools, health and direct shutdown signals. A
failed smoke test prevents promotion and leaves the workflow unsuccessful.

For an existing native Quadlet, pin `Image=ghcr.io/notsafeforgit/stash@sha256:...`
to that successful run's verified digest. Use the `/config` mount and process
identity described in [container setup](container.md). There is one source
revision, Actions run and image digest to record. Update the backup's application
runtime and deployment inventory to the same release; worker runtimes do not
change when only the application container changes.

Pull the image before restarting Stash. Keep unattended image switching disabled
when using explicitly reviewed digests. A routine image update does not repeat
the original catalog migration or restore rehearsal. New isolated rehearsals
still need distinct config, database, generated directory and local port; never
point an older incompatible binary at the native database.

## 3. Verify the running native instance

After starting the isolated unit (or completing the approved cutover), verify
its actual running image rather than assuming the pull selected it:

```bash
read -r -p "Native unit name: " stash_native_unit
read -r -p "Native container name: " stash_native_container
read -r -p "Native localhost health URL: " stash_native_health
systemctl --user show "$stash_native_unit" \
  --property=ActiveState,SubState,ExecMainStartTimestamp,MainPID
podman container inspect "$stash_native_container" \
  --format '{{.State.Status}} {{.State.Health.Status}} image={{.Image}} digest={{.ImageDigest}}'
podman exec "$stash_native_container" /usr/bin/stash --version
curl --fail --silent --show-error "$stash_native_health"
```

Confirm the main-image digest and binary revision match the deployment record.
Exercise login, the main app, media previews/playback/downloads, standalone
shares and offline assets without a UI flag. Verify native schema lineage,
import/reconciliation receipts and worker health separately; an HTTP health
response alone does not establish archive completeness.

Routine Stash restarts do not require draining a scrape pass, stopping n8n, or
pausing the worker timer. The external worker retains completed files and its
SQLite outbox during API downtime. It pauses new source work when ownership
cannot be renewed; after Stash returns, the dispatcher reclaims expired work and
resumes from the saved cursor/checkpoint. Existing download archives avoid
fetching completed files again. Receipt replay makes uncertain delivery safe.
Recovery may wait for the remaining lease, poll backoff and source cooldowns;
it is automatic, rather than necessarily immediate. Schema 1000107 makes lease
expiry independent of failure limits for both source runs and metadata jobs.

Producer-code updates still need a verified runtime and explicit policy upgrades
for retained work when the execution fingerprint changes. Coordinate that short
runtime handoff separately; never wait for a whole profile traversal to complete.
Keep the worker's durable outbox, download archives and pending requests intact.

When an update requires a schema migration, wait for the migration job's
`FINISHED` status as well as the expected schema and `systemStatus: OK` before
updating scraper policies or resuming workers. Schema readiness can become
visible while post-migration optimization still holds the write guard; native
administrative writes return `temporarily_unavailable` during that interval.
Retry an interrupted policy update with its retained request UUID and body after
the migration job finishes.

If startup fails, inspect that unit's journal and container logs before retrying.
Never run the frozen binary against a database after native writes begin.
Rollback follows the coordinated restore/export boundary in
[FORK.md](../FORK.md) and the full transition plan.
