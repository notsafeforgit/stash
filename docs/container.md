# Native Stash container

`ghcr.io/notsafeforgit/stash:native-preview` is the complete Linux amd64 runtime.
The main repository builds and tests it in one GitHub Actions workflow. There is
no downstream `stash-s6` build or runtime compatibility mode. Production pins the
verified registry digest; see [deployment](v3-deployment.md).

The Debian Trixie image includes Jellyfin FFmpeg 8, Intel VA-API/QSV drivers,
Intel OpenCL for Gen12/Arc and newer GPUs,
Mesa VA-API drivers, NVIDIA codec support, patched libvips/HEIF/AVIF tools,
translate-shell, Python 3.14 and version-pinned `uv`. GPU use still requires a
supported host driver and device access. A machine without a GPU uses software
codecs. Gallery-dl workers and backup tools remain separately installed host
or worker-container programs; the Stash image does not start them.

## Storage and process identity

Use the [Compose example](../docker/production/docker-compose.yml). Mount your
configuration directory at `/config`, with `config.yml` and the native SQLite
database inside it. Separate `/generated`, `/blobs`, `/cache` and `/metadata`
mounts are supported and their environment defaults are already set. Bind media
at paths of your choice and register them in **Settings → Library**.

For a rootful Docker launch, startup selects `PUID`/`PGID` (both default to 911),
then drops privileges and executes Stash directly as PID 1. Arguments and stop
signals reach Stash without a supervising shell. Directory roots can be created
and assigned to that user; existing file ownership is never recursively changed.
An inaccessible configuration fails startup instead of falling back to root.

For rootless Podman, use an explicit mapping to the host owner:

```ini
[Container]
UserNS=keep-id
User=1000
Group=1000
GroupAdd=keep-groups
Volume=%h/services/stash-native/config:/config
AddDevice=/dev/dri/renderD128:/dev/dri/renderD128
```

Substitute your host UID/GID and device. Non-root launches retain the selected
identity and supplementary groups; `PUID`/`PGID` do not remap an already non-root
process. Rootful launches accept additional numeric groups in `AVGID` (comma
separated). `AUTO_AVGID=true`, the default, adds mapped render-device groups.
Rootless `keep-groups` requires Podman's `crun` runtime.

NVIDIA hosts need the NVIDIA Container Toolkit and mapped GPU devices, for
example Docker Compose GPU reservations or Podman's CDI device selection. The
image advertises `compute,video,utility` driver capabilities. It does not patch
the host's NVIDIA driver or change its encoding-session limits.

H.264 HLS compatibility streams tone-map PQ/HLG HDR to BT.709 SDR. Intel QSV
and VA-API use OpenCL when the device passes a probe of the complete filter
chain. Other devices, or missing OpenCL support, use software tone mapping
with the selected encoder; this can be slower at 4K. Codec-copy HLS preserves
HDR, declaring the source's PQ/HLG range and probed codec/bit depth. The source
media is never rewritten. Intel VPP alone is not used for HLG conversion.

For a GPU regression check, build the `pkg/ffmpeg` test binary and run
`TestV3HLSHDRQSV` inside the image with `STASH_TEST_QSV_HDR_SOURCE` pointing to
a read-only HDR clip longer than 100 seconds. It exercises a seek with original
resolution and 1080p output, checking that the GPU path produces SDR. CI also
tests actual PQ/HLG pixel conversion, HDR-preserving remux and GPU fallback
without requiring a GPU.

## Upgrade from the compatible release

Changing the container mount layout and upgrading the database are separate
operations. The instructions below assume an existing **native** database.
If you are coming from the frozen compatible release or a supported upstream
schema, first follow the [schema promotion contract](native-schema.md).

Keep the original database, configuration and artwork at one consistent backup
boundary. Rehearse promotion on a copy and configure the native installation to
use a separate database path. Startup requires an explicit migration; it does
not silently promote an upstream database. Preserve any external catalogs and
producer state until their optional import has been verified. A local library
without these integrations needs no catalog import or scraper setup.

After native writes begin, switching back to an upstream or frozen binary is
not a database downgrade. The [final compatible release guide](releases/v2.5-compatible-final.md)
explains the stay-behind option. Retain the original copy until the native
installation and its recovery path have been verified.

## Move a native deployment out of the old layout

Do this once when replacing a native deployment's old wrapper image:

1. Stop Stash briefly so it cannot rewrite `config.yml` during the path edit.
2. Change the **container destination** of the existing config bind mount from
   `/root/.stash` to `/config`. Keep its host directory and files in place.
3. Update configuration paths beginning with `/root/.stash/`, including
   `database`, `plugins_path` and `scrapers_path`, to `/config/`. Inspect any
   installation-specific scripts using the old path as well.
4. Set the intended process identity and verify access to existing files and GPU
   devices. Remove `MIGRATE`, obsolete wrapper variables and the old mount.
5. Start the verified main-image digest. Check health, binary revision, database
   path and a hardware transcode before recording the deployment.

This does not copy or migrate the database or media. The new entrypoint does not
move files, create an old-path symlink, or detect an upstream-compatible mode.
Keep a small pre-edit configuration/unit copy for a failed-start rollback.

## Optional Python dependencies and certificates

Ordinary startup makes no package-installation requests. To install extension
dependencies, supply a nonempty `/config/requirements.txt` and explicitly set
`INSTALL_PYTHON_REQUIREMENTS=true`. Version pins and comments remain intact.
Mount `/pip-install` to persist its `install` and `cache` directories, writable by
the Stash user. `uv-pip` supports install, uninstall and requirements commands;
`uv-py` supports project/script execution. Installation failures stop startup.
The helpers originate in stash-s6; attribution is in
[NOTICE](../docker/runtime/NOTICE).

`INSTALL_PY_DEPS=true` explicitly installs compiler tools before requirements;
it requires container root during setup. Prefer prebuilt wheels. Custom `.crt`
certificates under `/config/certs` (or `CUSTOM_CERT_PATH`) can likewise be added
to system trust by a root-started container. For a non-root launch, mount a
prepared trust bundle and set `SSL_CERT_FILE` instead.

The image's health check uses `/healthz`. Operators can retain an authenticated
schema-aware check. Image CI checks the exact validated binary, default and
custom user IDs, supplementary groups, argument preservation, offline startup,
media tools and graceful shutdown. GPU encoding is verified on a real GPU host.
