#!/bin/sh
set -eu

# Distro avifgainmaputil builds can omit JPEG gain-map support. Pin the tools
# and explicitly enable libxml2, leaving the distro's shared libavif ABI alone.
version=1.4.2
sha256=2b645287340ba5a631d268b551dc2d72bd73ac33335962dd36dcdb6d8366921d
build_dir=$(mktemp -d)
trap 'rm -rf "$build_dir"' EXIT

curl -fsSL --retry 3 "https://github.com/AOMediaCodec/libavif/archive/refs/tags/v${version}.tar.gz" -o "$build_dir/source.tar.gz"
echo "$sha256  $build_dir/source.tar.gz" | sha256sum -c -
tar -xzf "$build_dir/source.tar.gz" -C "$build_dir"
cmake -S "$build_dir/libavif-$version" -B "$build_dir/build" \
  -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF \
  -DAVIF_BUILD_APPS=ON -DAVIF_BUILD_TESTS=OFF \
  -DAVIF_CODEC_AOM=SYSTEM -DAVIF_CODEC_DAV1D=SYSTEM \
  -DAVIF_LIBYUV=SYSTEM -DAVIF_LIBXML2=SYSTEM
cmake --build "$build_dir/build" --parallel "${BUILD_JOBS:-4}"

# Test the feature, not just the executable's presence or reported version.
"$build_dir/build/avifgainmaputil" convert \
  "$build_dir/libavif-$version/tests/data/paris_exif_xmp_gainmap_bigendian.jpg" \
  "$build_dir/gainmap.avif" -s 8
"$build_dir/build/avifgainmaputil" tonemap "$build_dir/gainmap.avif" \
  "$build_dir/sdr.png" --headroom 0 --cicp-output 1/13/6 -d 12
"$build_dir/build/avifdec" --info "$build_dir/gainmap.avif"

mkdir -p /out/usr/bin
for tool in avifenc avifdec avifgainmaputil; do
  install -m 755 "$build_dir/build/$tool" "/out/usr/bin/$tool"
  strip "/out/usr/bin/$tool"
done
