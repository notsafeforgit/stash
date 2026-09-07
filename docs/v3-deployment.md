# Deploying the v3 rewrite

The current deployment has two builds: `notsafeforgit/stash` publishes the Stash
binary image, then `notsafeforgit/stash-s6` copies that binary into its runtime
image. Restart the local Quadlet only after both builds succeed and the wrapper
is confirmed to contain the intended Stash revision.

These commands describe the existing rootless deployment: user unit
`stash.service`, container `stash`, image
`ghcr.io/notsafeforgit/stash-s6:hwaccel-v3-rewrite`, and health URL
`http://127.0.0.1:8009/healthz`. Substitute the unit/container/port when using
another host. See [v3 development](../ui/v3/docs/development.md) for local checks.

## 1. Wait for the Stash image

Start from the intended commit on `v3-rewrite`, with a clean working tree and
completed validation. Record its full revision and push the branch:

```bash
stash_sha="$(git rev-parse HEAD)"
stash_build_tag="v3-rewrite-$(printf '%s' "$stash_sha" | cut -c 1-7)"
git push origin v3-rewrite
gh run list --repo notsafeforgit/stash --workflow ghcr-publish.yml \
  --branch v3-rewrite --commit "$stash_sha" \
  --json databaseId,headSha,status,conclusion,url
```

The [publisher](../.github/workflows/ghcr-publish.yml) installs dependencies,
generates bindings, builds **both** UIs, runs `make validate-fork`, compiles the
Linux amd64 binary, and publishes:

- `ghcr.io/notsafeforgit/stash:v3-rewrite` — moving branch tag.
- `ghcr.io/notsafeforgit/stash:v3-rewrite-<first-seven-SHA-characters>` — build tag.

Use exactly seven characters when constructing the tag. `git rev-parse --short`
may produce a longer abbreviation in this repository. Build tags have a 14-day
cleanup policy; retain image digests for deployment records.

Select the run with the recorded `headSha`, then wait and check its conclusion:

```bash
read -r -p "Stash publish run ID: " stash_run
gh run watch "$stash_run" --repo notsafeforgit/stash --exit-status
gh run view "$stash_run" --repo notsafeforgit/stash \
  --json headSha,conclusion,url
skopeo inspect --no-creds "docker://ghcr.io/notsafeforgit/stash:$stash_build_tag" \
  --format '{{.Digest}} {{index .Labels "org.opencontainers.image.revision"}}'
skopeo inspect --no-creds docker://ghcr.io/notsafeforgit/stash:v3-rewrite \
  --format '{{.Digest}} {{index .Labels "org.opencontainers.image.revision"}}'
```

Require success, the full intended revision in the source image label, and
matching build/floating digests. A canceled run may have been superseded by a
newer push; identify that revision before continuing.

## 2. Bake stash-s6 on its v3 branch

The workflow file is `develop.yml`. Its name on the `stash-s6` **v3-rewrite**
branch is `v3-rewrite-bake`; the default-branch UI may call it `develop-bake`.
Always specify the repository, workflow file, and branch:

```bash
gh workflow run develop.yml --repo notsafeforgit/stash-s6 --ref v3-rewrite
gh run list --repo notsafeforgit/stash-s6 --workflow develop.yml \
  --branch v3-rewrite --event workflow_dispatch --limit 5 \
  --json databaseId,createdAt,headSha,status,conclusion,url
```

Dispatch requires an account with write access to `stash-s6`. If GitHub returns
403, inspect `gh auth status` and the account's repository permissions; use the
authorized owner account before retrying. Read access alone can list runs but
cannot dispatch them. Do not print tokens into logs.

Choose the run created by this dispatch, then verify the upstream selection:

```bash
read -r -p "stash-s6 bake run ID: " stash_s6_run
gh run watch "$stash_s6_run" --repo notsafeforgit/stash-s6 --exit-status
gh run view "$stash_s6_run" --repo notsafeforgit/stash-s6 \
  --json headSha,conclusion,url
gh run view "$stash_s6_run" --repo notsafeforgit/stash-s6 --log \
  | rg 'Using upstream stash tag'
```

The [v3 workflow](https://github.com/notsafeforgit/stash-s6/blob/v3-rewrite/.github/workflows/develop.yml)
chooses the newest published Stash build tag matching `v3-rewrite-<SHA>` by
package-version creation time. It has **no input to pin a particular Stash
commit**. Require `Using upstream stash tag` to match `$stash_build_tag` before
deploying. If another publication wins the race, resolve the revision choice
before restarting the service.

The [bake targets](https://github.com/notsafeforgit/stash-s6/blob/v3-rewrite/docker-bake.hcl)
publish `alpine-v3-rewrite`, `hwaccel-alpine-v3-rewrite`, and
`hwaccel-v3-rewrite` for Linux amd64. The local Quadlet uses the last variant.
Record its published digest from the bake output and inspect that image:

```bash
stash_s6_image=ghcr.io/notsafeforgit/stash-s6
stash_s6_tag=hwaccel-v3-rewrite
skopeo inspect --no-creds "docker://$stash_s6_image:$stash_s6_tag" \
  --format '{{.Digest}} {{index .Labels "org.opencontainers.image.revision"}}'
read -r -p "Verified hwaccel image digest (sha256:...): " stash_s6_digest
```

Require the registry digest to match the baked `hwaccel-v3-rewrite` output.
The wrapper's revision label identifies the **stash-s6 commit**, not the Stash
binary revision. Its date tags also come from that wrapper commit's timestamp;
they do not establish when the bake ran. Verify the Stash binary after startup.
Public image inspection with `skopeo --no-creds` does not require a GitHub token
with package-read scope.

## 3. Pull, restart, and verify the local Quadlet

The unit is generated from `~/.config/containers/systemd/stash.container`.
It currently uses `Pull=newer` and `AutoUpdate=registry`. Run these commands as
the user who owns the rootless container:

```bash
podman pull "$stash_s6_image:$stash_s6_tag"
podman image inspect "$stash_s6_image:$stash_s6_tag" \
  --format '{{.Id}} {{json .RepoDigests}}'
```

Confirm the pulled image's repository digests include `$stash_s6_digest`, then
record its image ID and restart:

```bash
stash_s6_image_id="$(podman image inspect "$stash_s6_image:$stash_s6_tag" --format '{{.Id}}')"
systemctl --user restart stash.service
systemctl --user show stash.service \
  --property=ActiveState,SubState,ExecMainStartTimestamp,MainPID
podman container inspect stash \
  --format '{{.State.Status}} {{.State.Health.Status}} image={{.Image}} digest={{.ImageDigest}}'
podman exec stash /app/stash --version
curl --fail --silent --show-error http://127.0.0.1:8009/healthz
```

Allow startup to finish and confirm container health becomes `healthy`; the
configured health interval is one minute. After the app is ready, a manual
`podman healthcheck run stash` can confirm it sooner. The running image ID must
match `$stash_s6_image_id`, and `/app/stash --version` must identify the intended
`$stash_sha` (the version string abbreviates it). A floating tag can move between
inspection and restart, so check the running image even when the pull succeeded.

Record the source SHA, both Actions run URLs, source and wrapper digests, and
the running binary version. If startup or health fails, inspect
`journalctl --user -u stash.service -n 100 --no-pager` and `podman logs --tail 100 stash`
before retrying. Keep deployment credentials and private configuration out of
shared logs. Reverting a runtime image must also follow the database
[compatibility rules](../FORK.md); the
[v3-only schema promotion](v3-schema-promotion.md) remains a future transition.
