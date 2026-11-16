#!/bin/sh
# Install the production image's Jellyfin FFmpeg build (and avifenc) on an
# Ubuntu CI runner, so media tests run against the FFmpeg that ships.
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
. "$root/docker/runtime/jellyfin-ffmpeg.env"
codename="$(. /etc/os-release && echo "$VERSION_CODENAME")"

# The Azure mirror repeatedly exhausted the download budget. Use the
# runner's existing Ubuntu archive/security fallback mirrors.
if [ -f /etc/apt/apt-mirrors.txt ]; then
  sudo sed -i '/azure\.archive\.ubuntu\.com/d' /etc/apt/apt-mirrors.txt
fi
sudo apt-get update
sudo apt-get install --no-install-recommends -y ca-certificates curl gnupg
sudo sh "$root/docker/runtime/install-jellyfin-repo.sh" ubuntu "$codename"
sudo apt-get update
sudo apt-get install --no-install-recommends -y \
  "jellyfin-ffmpeg8=${JELLYFIN_FFMPEG_VERSION}-${codename}" libavif-bin
sudo ln -sf /usr/lib/jellyfin-ffmpeg/ffmpeg /usr/local/bin/ffmpeg
sudo ln -sf /usr/lib/jellyfin-ffmpeg/ffprobe /usr/local/bin/ffprobe
ffmpeg -hide_banner -version | head -n 1
