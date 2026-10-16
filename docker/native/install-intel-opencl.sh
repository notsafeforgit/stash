#!/bin/sh
set -eu

# Debian trixie does not ship intel-opencl-icd. These upstream Intel releases
# are also used by Jellyfin's trixie runtime. Keep the compiler/runtime pair
# pinned and checksum-verified; ordinary image starts never install packages.
# https://github.com/intel/compute-runtime/releases/tag/26.35.39758.10
# https://github.com/intel/intel-graphics-compiler/releases/tag/v2.41.5
[ "$(dpkg --print-architecture)" = amd64 ] || exit 0
package_dir=$(mktemp -d)
trap 'rm -rf "$package_dir"' EXIT
cd "$package_dir"

curl -fsSL --retry 3 \
  -O https://github.com/intel/compute-runtime/releases/download/26.35.39758.10/libigdgmm12_22.10.0_amd64.deb \
  -O https://github.com/intel/compute-runtime/releases/download/26.35.39758.10/intel-opencl-icd_26.35.39758.10-0_amd64.deb \
  -O https://github.com/intel/intel-graphics-compiler/releases/download/v2.41.5/intel-igc-core-2_2.41.5+22716_amd64.deb \
  -O https://github.com/intel/intel-graphics-compiler/releases/download/v2.41.5/intel-igc-opencl-2_2.41.5+22716_amd64.deb
sha256sum -c <<'CHECKSUMS'
6031a63d6e8a12ce61c14efc15f2c8e727061286e3820b8594e6d00615e04d54  libigdgmm12_22.10.0_amd64.deb
61712caaddeba3d38e4f79e2a0fb23fea25596ca2d72c3144c6eea2331ec4301  intel-opencl-icd_26.35.39758.10-0_amd64.deb
0a6e64a663ae65a0fa02d6912ae3b6b37cf85b90c21cc423fd9fef70aaf4f628  intel-igc-core-2_2.41.5+22716_amd64.deb
779e1b9e88098eb25711e9a8f67c2752665bad22f134aa40ed5649f6e1b87058  intel-igc-opencl-2_2.41.5+22716_amd64.deb
CHECKSUMS
apt-get install -y --no-install-recommends ./*.deb
