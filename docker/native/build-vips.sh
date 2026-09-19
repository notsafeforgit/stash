#!/bin/sh
set -eu

# Keep image parsing fixes current even when the stable distro package lags.
version=8.18.6
sha256=3c41e1d5458081bfa4a5bc54e116c46259c75c6760a18027764555632b9dda3e
build_dir=$(mktemp -d)
trap 'rm -rf "$build_dir"' EXIT

curl -fsSL --retry 3 "https://github.com/libvips/libvips/releases/download/v${version}/vips-${version}.tar.xz" -o "$build_dir/source.tar.xz"
echo "$sha256  $build_dir/source.tar.xz" | sha256sum -c -
tar -xJf "$build_dir/source.tar.xz" -C "$build_dir"
meson setup "$build_dir/build" "$build_dir/vips-$version" \
  --prefix=/usr --libdir="${LIBHEIF_LIBDIR:-lib}" --buildtype=release \
  -Dheif=enabled -Djpeg=enabled -Dpng=enabled -Dwebp=enabled \
  -Dintrospection=disabled -Ddocs=false -Dcpp-docs=false -Dexamples=false
meson compile -C "$build_dir/build" -j "${BUILD_JOBS:-4}"
DESTDIR=/out meson install -C "$build_dir/build" --no-rebuild
rm -rf /out/usr/include /out/usr/share "/out/usr/${LIBHEIF_LIBDIR:-lib}/pkgconfig"
