#!/bin/sh
set -eu

# Distro releases lag the upstream security fixes. Build the same release
# against each runtime's own codec libraries, with no external plugin loading.
version=1.23.4
sha256=d0c02b4b0e978f34a1974b6f3eea7975a537bf7a9195ffeea38e7242ff316fdd
build_dir=$(mktemp -d)
trap 'rm -rf "$build_dir"' EXIT

curl -fsSL --retry 3 "https://github.com/strukturag/libheif/releases/download/v${version}/libheif-${version}.tar.gz" -o "$build_dir/source.tar.gz"
echo "$sha256  $build_dir/source.tar.gz" | sha256sum -c -
tar -xzf "$build_dir/source.tar.gz" -C "$build_dir"
cmake -S "$build_dir/libheif-$version" -B "$build_dir/build" \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_INSTALL_PREFIX=/usr \
  -DCMAKE_INSTALL_LIBDIR="${LIBHEIF_LIBDIR:-lib}" \
  -DBUILD_SHARED_LIBS=ON \
  -DENABLE_PLUGIN_LOADING=OFF \
  -DWITH_LIBDE265=ON -DWITH_X265=ON \
  -DWITH_AOM_DECODER=ON -DWITH_AOM_ENCODER=ON -DWITH_DAV1D=ON \
  -DWITH_UNCOMPRESSED_CODEC=ON \
  -DWITH_EXAMPLES=ON -DWITH_EXAMPLE_HEIF_THUMB=OFF -DWITH_EXAMPLE_HEIF_VIEW=OFF \
  -DWITH_GDK_PIXBUF=OFF -DBUILD_TESTING=OFF -DBUILD_DOCUMENTATION=OFF
cmake --build "$build_dir/build" --parallel "${BUILD_JOBS:-4}"
# libvips is built in this stage too, against the patched headers and library.
cmake --install "$build_dir/build" --strip
DESTDIR=/out cmake --install "$build_dir/build" --strip
rm -rf /out/usr/include /out/usr/share "/out/usr/${LIBHEIF_LIBDIR:-lib}/cmake" "/out/usr/${LIBHEIF_LIBDIR:-lib}/pkgconfig"
