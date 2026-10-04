# Native preview builds and deployment

Development continues on `v3-rewrite`. Production remains on the frozen
compatible release until the [cutover gates](native-archive-transition-plan.md#production-cutover-runbook)
and owner review pass. A native image must use a separate database and config;
never open the live compatible library merely to test a build. The original
compatible source and image digests are preserved in the
[release manifest](releases/v2.5-compatible-final.json).

There are two images: `notsafeforgit/stash` publishes the binary, and
`notsafeforgit/stash-s6` packages that binary with its runtime. Native builds
publish separate `native-preview` tags. Verify the exact source digest in the
wrapper before starting an isolated rehearsal or an approved cutover.

## 1. Publish and verify the Stash image

Complete the [development checks](../ui/v3/docs/development.md#validation),
commit incremental changes, and push `v3-rewrite`:

```bash
stash_sha="$(git rev-parse HEAD)"
stash_build_tag="native-preview-$(printf '%s' "$stash_sha" | cut -c 1-7)"
git push origin v3-rewrite
gh run list --repo notsafeforgit/stash --workflow ghcr-publish.yml \
  --branch v3-rewrite --commit "$stash_sha" \
  --json databaseId,headSha,status,conclusion,url
```

The [publisher](../.github/workflows/ghcr-publish.yml) installs only v3
frontend dependencies, regenerates Go/v3 bindings, builds the app/share/offline
entries and login locales, runs `make validate-fork`, and compiles Linux amd64.
It publishes `ghcr.io/notsafeforgit/stash:native-preview` and the seven-character
revision tag above. The moving tag alone is not a deployment identity.

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

## 2. Build the wrapper from that digest

The wrapper's `develop.yml` workflow on `v3-rewrite` is named
`native-preview-bake`. Dispatch it with the required full source digest:

```bash
gh workflow run develop.yml --repo notsafeforgit/stash-s6 --ref v3-rewrite \
  -f stash_digest="$stash_digest"
gh run list --repo notsafeforgit/stash-s6 --workflow develop.yml \
  --branch v3-rewrite --event workflow_dispatch --limit 5 \
  --json databaseId,createdAt,headSha,status,conclusion,url
```

Choose this dispatch's run and require success. Its source validation rejects
floating tags. The bake publishes Linux amd64 `alpine-native-preview`,
`hwaccel-alpine-native-preview`, and `hwaccel-native-preview`, with revision
suffixes containing the wrapper commit and selected source digest prefix.
Use the exact wrapper digest from the bake receipt:

```bash
read -r -p "Wrapper image digest from the verified bake: " stash_s6_digest
stash_s6_image=ghcr.io/notsafeforgit/stash-s6
skopeo inspect --no-creds "docker://$stash_s6_image@$stash_s6_digest" \
  --format '{{.Digest}} {{index .Labels "io.stash.source.image"}}'
```

Require `io.stash.source.image` to equal
`ghcr.io/notsafeforgit/stash@$stash_digest`. The wrapper revision label refers
to the wrapper source, not the Stash binary. Keep both Actions run URLs, both
source revisions and both image digests in the deployment record.

## 3. Prepare an isolated native unit

Use a separate unit, container, config, generated directory and native database.
Bind production media read-only for read-only rehearsals, or use copied fixtures
for write tests. Follow the transition plan for writer quiescence, common-boundary
backups, semantic reconciliation, restore validation and eventual production
cutover; publishing an image does not satisfy those gates.

For a native Quadlet:

- Pin `Image=ghcr.io/notsafeforgit/stash-s6@sha256:...` to the verified wrapper.
- Remove `Environment=STASH_ENABLE_V3_UI=true` and any `--enable-v3-ui` command
  argument. V3 is the sole application; the removed CLI flag is rejected.
- Remove obsolete `enable-v3-ui`/`enable_v3_ui` config keys from the native copy.
- Set the intended distinct native config/database paths and an unused local
  port. Do not enable source workers until the reviewed grants and source
  activation receipts are ready.
- Keep unattended image switching disabled through the migration and observation
  period. The frozen production unit retains its original configuration until
  the approved cutover.

The wrapper entrypoint starts `/app/stash --nobrowser` and does not inject the
retired UI flag. The live compatible `stash.container` still uses the opt-in
variable; remove it when preparing the native replacement, without modifying
the compatible deployment during development.

## 4. Verify the running native instance

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
podman exec "$stash_native_container" /app/stash --version
curl --fail --silent --show-error "$stash_native_health"
```

Confirm the wrapper digest and binary revision match the deployment record.
Exercise login, the main app, media previews/playback/downloads, standalone
shares and offline assets without a UI flag. Verify native schema lineage,
import/reconciliation receipts and worker health separately; an HTTP health
response alone does not establish archive completeness.

If startup fails, inspect that unit's journal and container logs before retrying.
Never run the frozen binary against a database after native writes begin.
Rollback follows the coordinated restore/export boundary in
[FORK.md](../FORK.md) and the full transition plan.
