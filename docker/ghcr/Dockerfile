# Build patched HEIF/AVIF support against the runtime distribution.
FROM alpine:3.24 AS heif
RUN apk add --no-cache build-base cmake pkgconf curl ca-certificates aom-dev dav1d-dev libde265-dev x265-dev libjpeg-turbo-dev libpng-dev
RUN apk add --no-cache meson samurai xz glib-dev expat-dev libwebp-dev tiff-dev librsvg-dev pango-dev fontconfig-dev lcms2-dev fftw-dev libexif-dev orc-dev openjpeg-dev libjxl-dev poppler-dev cfitsio-dev libimagequant-dev highway-dev libarchive-dev openexr-dev cgif-dev imagemagick-dev
COPY docker/native/build-libheif.sh /build-libheif.sh
COPY docker/native/build-vips.sh /build-vips.sh
RUN sh /build-libheif.sh && sh /build-vips.sh

FROM alpine:3.24

# Copy the statically compiled amd64 binary from the dist folder into the alpine image
COPY dist/stash-linux /usr/bin/stash

RUN apk upgrade --no-cache && apk add --no-cache ca-certificates python3 py3-requests py3-requests-toolbelt py3-lxml py3-pip ffmpeg libavif-apps tzdata aom-libs libdav1d libde265 x265-libs libjpeg-turbo libpng vips vips-tools vips-heif vips-jxl vips-magick vips-poppler \
    && python3 -m pip install --break-system-packages --upgrade "pip>=26.2" \
    && python3 -m pip install --break-system-packages mechanicalsoup cloudscraper stashapp-tools
# Replace distro libraries and modules with the matching security builds.
RUN find /usr/lib \( -name 'libheif.so*' -o -name 'libvips.so*' -o -name 'libvips-cpp.so*' \) -delete \
    && rm -rf /usr/lib/vips-modules-* /usr/lib/*-linux-gnu/vips-modules-*
COPY --from=heif /out/ /
RUN heif-info --version | grep -F '1.23.4' \
    && vips --version | grep -F '8.18.6'
ENV STASH_CONFIG_FILE=/root/.stash/config.yml

# Basic build-time metadata as defined at https://github.com/opencontainers/image-spec/blob/main/annotations.md#pre-defined-annotation-keys
LABEL org.opencontainers.image.title="Stash" \
    org.opencontainers.image.description="An organizer for your porn, written in Go." \
    org.opencontainers.image.url="https://stashapp.cc" \
    org.opencontainers.image.documentation="https://docs.stashapp.cc" \
    org.opencontainers.image.source="https://github.com/stashapp/stash" \
    org.opencontainers.image.licenses="AGPL-3.0"

EXPOSE 9999
CMD ["stash"]
