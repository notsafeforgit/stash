#!/bin/sh
# Run the Go packages whose tests execute FFmpeg inside the production image,
# so local results use the FFmpeg that ships instead of the host's build.
# Test binaries are built on the host and only executed in the container.
#
# Usage: scripts/test-media-image.sh [image]
#   MEDIA_TEST_IMAGE     default ghcr.io/notsafeforgit/stash:develop
#   MEDIA_TEST_PACKAGES  default: every package whose tests run FFmpeg
#   CONTAINER_ENGINE     default podman, else docker
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
image="${1:-${MEDIA_TEST_IMAGE:-ghcr.io/notsafeforgit/stash:develop}}"
packages="${MEDIA_TEST_PACKAGES:-pkg/ffmpeg pkg/previewimage internal/manager internal/ingest internal/api}"
# Match `make it`, including integration-tagged tests.
tags="${GO_BUILD_TAGS:-sqlite_stat4 sqlite_math_functions sqlite_fts5} integration"
engine="${CONTAINER_ENGINE:-$(command -v podman >/dev/null 2>&1 && echo podman || echo docker)}"
# Rootless Podman maps --user to a subordinate ID; keep the caller's identity
# so the private test-binary directory and the checkout stay readable.
userns=""
[ "$(basename "$engine")" = podman ] && userns="--userns=keep-id"

if [ ! -f "$root/ui/v3/build/index.html" ]; then
  echo "internal/api embeds the v3 build: run 'make ui' first" >&2
  exit 1
fi

bin="$(mktemp -d)"
trap 'rm -rf "$bin"' EXIT
for package in $packages; do
  (cd "$root" && go test -c -tags "$tags" -o "$bin/$(echo "$package" | tr / _).test" "./$package")
done

"$engine" run --rm --pull=missing --entrypoint ffmpeg "$image" -hide_banner -version | head -n 1
status=0
for package in $packages; do
  echo "== $package"
  # Unprivileged, offline and without relabelling the checkout.
  # shellcheck disable=SC2086 # $userns is empty or a single flag.
  "$engine" run --rm --pull=missing --network=none --security-opt label=disable $userns \
    --user "$(id -u):$(id -g)" -e HOME=/tmp --tmpfs /tmp:rw,exec \
    -v "$bin:/media-tests:ro" -v "$root:/src:ro" -w "/src/$package" \
    --entrypoint "/media-tests/$(echo "$package" | tr / _).test" \
    "$image" -test.count=1 -test.timeout 30m || status=1
done
exit "$status"
