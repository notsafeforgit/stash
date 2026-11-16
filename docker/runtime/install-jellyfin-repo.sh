#!/bin/sh
# Add Jellyfin's signed apt repository after checking the key's fingerprint.
# Usage: install-jellyfin-repo.sh <debian|ubuntu> <suite>
# Requires curl, gpg and ca-certificates. Run as root.
set -eu

distribution="$1"
suite="$2"
here="$(dirname "$0")"
. "$here/jellyfin-ffmpeg.env"

key="$(mktemp)"
trap 'rm -f "$key"' EXIT
curl -fsSL --retry 3 https://repo.jellyfin.org/jellyfin_team.gpg.key -o "$key"
fingerprint="$(gpg --show-keys --with-colons "$key" | awk -F: '/^fpr/ { print $10; exit }')"
if [ "$fingerprint" != "$JELLYFIN_KEY_FINGERPRINT" ]; then
  echo "unexpected Jellyfin signing key: ${fingerprint:-none}" >&2
  exit 1
fi

mkdir -p /etc/apt/keyrings
gpg --dearmor --yes -o /etc/apt/keyrings/jellyfin.gpg "$key"
cat > /etc/apt/sources.list.d/jellyfin.sources <<EOF
Types: deb
URIs: https://repo.jellyfin.org/$distribution
Suites: $suite
Components: main
Signed-By: /etc/apt/keyrings/jellyfin.gpg
EOF
