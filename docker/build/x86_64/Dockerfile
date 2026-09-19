# Build patched HEIF/AVIF support against the runtime distribution.
FROM alpine:3.24 AS heif
RUN apk add --no-cache build-base cmake pkgconf curl ca-certificates aom-dev dav1d-dev libde265-dev x265-dev libjpeg-turbo-dev libpng-dev
RUN apk add --no-cache meson samurai xz glib-dev expat-dev libwebp-dev tiff-dev librsvg-dev pango-dev fontconfig-dev lcms2-dev fftw-dev libexif-dev orc-dev openjpeg-dev libjxl-dev poppler-dev cfitsio-dev libimagequant-dev highway-dev libarchive-dev openexr-dev cgif-dev imagemagick-dev
COPY docker/native/build-libheif.sh /build-libheif.sh
COPY docker/native/build-vips.sh /build-vips.sh
RUN sh /build-libheif.sh && sh /build-vips.sh

# This dockerfile should be built with `make docker-build` from the stash root.

# Build Frontend
FROM node:24-alpine3.24 AS frontend
RUN apk add --no-cache make git
## cache node_modules separately
COPY ./ui/v2.5/package.json ./ui/v2.5/pnpm-lock.yaml /stash/ui/v2.5/
WORKDIR /stash
COPY Makefile /stash/
COPY ./graphql /stash/graphql/
COPY ./ui /stash/ui/
# pnpm install with npm
RUN npm install -g pnpm
RUN make pre-ui
RUN make generate-ui
ARG GITHASH
ARG STASH_VERSION
RUN BUILD_DATE=$(date +"%Y-%m-%d %H:%M:%S") make ui-only

# Build Backend
FROM golang:1.27.1-alpine3.24 AS backend
RUN apk add --no-cache make alpine-sdk
WORKDIR /stash
COPY ./go* ./*.go Makefile gqlgen.yml .gqlgenc.yml /stash/
COPY ./graphql /stash/graphql/
COPY ./scripts /stash/scripts/
COPY ./pkg /stash/pkg/
COPY ./cmd /stash/cmd/
COPY ./internal /stash/internal/
# needed for generate-login-locale
COPY ./ui /stash/ui/
RUN make generate-backend generate-login-locale
COPY --from=frontend /stash /stash/
ARG GITHASH
ARG STASH_VERSION
RUN make flags-release flags-pie stash

# Final Runnable Image
FROM alpine:3.24
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates vips-tools vips-heif vips-jxl vips-magick vips-poppler ffmpeg libavif-apps aom-libs libdav1d libde265 x265-libs libjpeg-turbo libpng
COPY --from=backend /stash/stash /usr/bin/
# Replace distro libraries and modules with the matching security builds.
RUN find /usr/lib \( -name 'libheif.so*' -o -name 'libvips.so*' -o -name 'libvips-cpp.so*' \) -delete \
    && rm -rf /usr/lib/vips-modules-* /usr/lib/*-linux-gnu/vips-modules-*
COPY --from=heif /out/ /
RUN heif-info --version | grep -F '1.23.4' \
    && vips --version | grep -F '8.18.6'
ENV STASH_CONFIG_FILE=/root/.stash/config.yml
EXPOSE 9999
ENTRYPOINT ["stash"]
