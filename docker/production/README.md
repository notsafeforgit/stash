# Native container installation

This fork publishes one complete image at
`ghcr.io/notsafeforgit/stash:develop`, including Jellyfin FFmpeg, hardware
acceleration support and Python/uv. It does not require a stash-s6 wrapper.
The current published platform is Linux amd64.

Use Docker with Compose, or rootless Podman. Download this fork's example:

```sh
mkdir stash && cd stash
curl -fLo docker-compose.yml https://raw.githubusercontent.com/notsafeforgit/stash/develop/docker/production/docker-compose.yml
```

Set your host UID/GID, timezone and bind mounts in that file. Configuration lives
at `/config`; media can be mounted at `/data` and registered in **Settings →
Library**. For production, replace the moving image tag with a verified release
digest from the [deployment workflow](../../docs/v3-deployment.md).

```sh
docker compose up -d
```

Open `http://localhost:9999` and complete setup. Source tracking and external S3
backups remain optional and off by default.

See [container setup](../../docs/container.md) for GPU devices, rootless Podman,
Python dependencies, and the one-time `/root/.stash` → `/config` mount/path
conversion for an existing installation. Do not start the new image against an
old-layout mount without updating its configuration paths.
