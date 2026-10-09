# Native preview builds and deployment

Development continues on `v3-rewrite`, and the native application is now running
in production. Routine updates replace that native application's pinned image;
they do not repeat the original cutover or open the frozen compatible database.
Keep build/rehearsal databases separate from the live library. The original
compatible source and image digests remain preserved in the
[release manifest](releases/v2.5-compatible-final.json).

There are two images: `notsafeforgit/stash` publishes the binary, and
`notsafeforgit/stash-s6` packages that binary with its runtime. Native builds
publish separate `native-preview` tags. Verify the exact source digest in the
wrapper before deploying an update or starting an isolated rehearsal.
Production images must be built and published by GitHub Actions and pulled from
GHCR. A local build, or uploading a locally built image to GHCR, does not satisfy
the owner's delivery requirement. Report push, publication and deployment as
separate outcomes, with the run URLs and actual running registry digest.

### When dispatch reports Actions disabled

Inspect both `GET /repos/{owner}/{repo}/actions/permissions` and the workflow's
state. An enabled repository policy and an active workflow do not prove GitHub
will accept a dispatch. On October 9, both readbacks were enabled/active for
`notsafeforgit/stash`, but push created no checks and workflow dispatch returned
HTTP 422, `Actions has been disabled for this repository.` Reapplying the enabled
policy and enabling the workflow did not clear the error, including with REST
API version `2026-03-10`.

[GitHub documents a separate GitHub-controlled disabled state](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository#managing-github-actions-permissions-for-your-repository)
that settings changes cannot clear and advises contacting Support for review.
Keep the HTTP status, response message, UTC timestamp and `X-GitHub-Request-Id`
with the enabled/active readbacks. Do not diagnose billing or a disabled settings
toggle without evidence. Publication remains blocked until a run actually
starts and succeeds; neither a successful push nor registry access establishes
that the publishing workflow ran. Do not substitute a local image.

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
