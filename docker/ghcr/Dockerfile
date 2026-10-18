# syntax=docker/dockerfile:1
# One native image, including the hardware runtime formerly in stash-s6.
FROM debian:trixie AS jellyfin-setup
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates gnupg
COPY docker/runtime/jellyfin.sources /etc/apt/sources.list.d/jellyfin.sources
RUN mkdir -p /etc/apt/keyrings \
    && curl -fsSL --retry 3 https://repo.jellyfin.org/jellyfin_team.gpg.key -o /tmp/jellyfin.key \
    && gpg --dearmor -o /etc/apt/keyrings/jellyfin.gpg /tmp/jellyfin.key

# Build patched HEIF/AVIF support against the runtime distribution.
FROM debian:trixie AS heif
RUN apt-get update && apt-get install -y --no-install-recommends build-essential cmake pkg-config curl ca-certificates libaom-dev libdav1d-dev libde265-dev libx265-dev libjpeg-dev libpng-dev
RUN sed -i 's/^Types: deb$/Types: deb deb-src/' /etc/apt/sources.list.d/*.sources \
    && apt-get update \
    && apt-get build-dep -y --no-install-recommends vips \
    && apt-get install -y --no-install-recommends meson ninja-build xz-utils
COPY docker/native/build-libheif.sh /build-libheif.sh
COPY docker/native/build-vips.sh /build-vips.sh
RUN export LIBHEIF_LIBDIR=lib/$(dpkg-architecture -qDEB_HOST_MULTIARCH) \
    && sh /build-libheif.sh && sh /build-vips.sh

FROM debian:trixie AS avif
RUN apt-get update && apt-get install -y --no-install-recommends build-essential cmake git pkg-config curl ca-certificates libaom-dev libdav1d-dev libjpeg-dev libpng-dev libxml2-dev libyuv-dev
COPY docker/native/build-libavif.sh /build-libavif.sh
RUN sh /build-libavif.sh

FROM python:3.14-slim-trixie AS runtime
ENV HOME=/config \
    STASH_CONFIG_FILE=/config/config.yml \
    STASH_GENERATED=/generated \
    STASH_CACHE=/cache \
    STASH_BLOBS=/blobs \
    STASH_METADATA=/metadata \
    TZ=Etc/UTC \
    UV_TARGET=/pip-install/install \
    PYTHONPATH=/pip-install/install \
    UV_CACHE_DIR=/pip-install/cache \
    UV_BREAK_SYSTEM_PACKAGES=1 \
    NVIDIA_DRIVER_CAPABILITIES=compute,video,utility \
    NVIDIA_VISIBLE_DEVICES=all \
    STASH_HW_TEST_TIMEOUT=10
COPY --from=ghcr.io/astral-sh/uv:0.13.0 /uv /uvx /usr/local/bin/
COPY --from=jellyfin-setup /etc/apt/sources.list.d/jellyfin.sources /etc/apt/sources.list.d/jellyfin.sources
COPY --from=jellyfin-setup /etc/apt/keyrings/jellyfin.gpg /etc/apt/keyrings/jellyfin.gpg
COPY docker/native/install-intel-opencl.sh /tmp/install-intel-opencl.sh
RUN sed -i 's/Components: main$/Components: main contrib non-free/' /etc/apt/sources.list.d/debian.sources \
    && apt-get update && apt-get upgrade -y \
    && apt-get install -y --no-install-recommends \
       ca-certificates curl jellyfin-ffmpeg8 libvips-tools libavif-bin libxml2 \
       libaom3 libdav1d7 libde265-0 libx265-215 libjpeg62-turbo libpng16-16t64 \
       locales nano translate-shell tzdata wget util-linux \
    && if [ "$(dpkg --print-architecture)" = amd64 ]; then \
         apt-get install -y --no-install-recommends \
           i965-va-driver-shaders intel-media-va-driver-non-free mesa-va-drivers; \
       fi \
    && sh /tmp/install-intel-opencl.sh && rm /tmp/install-intel-opencl.sh \
    && rm -rf /var/lib/apt/lists/* \
    && ln -s /usr/lib/jellyfin-ffmpeg/ffmpeg /usr/bin/ffmpeg \
    && ln -s /usr/lib/jellyfin-ffmpeg/ffprobe /usr/bin/ffprobe \
    && ln -s /usr/lib/jellyfin-ffmpeg/vainfo /usr/bin/vainfo \
    && sed -i 's/^# en_US.UTF-8 UTF-8/en_US.UTF-8 UTF-8/' /etc/locale.gen \
    && locale-gen \
    && groupadd -g 911 stash \
    && useradd -u 911 -d /config -s /usr/sbin/nologin -g stash -G video stash \
    && mkdir -p /config /generated /cache /blobs /metadata /pip-install \
    && chown stash:stash /config /generated /cache /blobs /metadata /pip-install

RUN find /usr/lib \( -name 'libheif.so*' -o -name 'libvips.so*' -o -name 'libvips-cpp.so*' \) -delete \
    && rm -rf /usr/lib/vips-modules-* /usr/lib/*-linux-gnu/vips-modules-*
COPY --from=heif /out/ /
COPY --from=avif /out/ /
RUN ldconfig \
    && heif-info --version | grep -F '1.23.4' \
    && vips --version | grep -F '8.18.6' \
    && avifenc --version | grep -F '1.4.2' \
    && trans -no-init -version

COPY docker/runtime/root/ /
COPY docker/runtime/NOTICE /usr/share/doc/stash-container/NOTICE
RUN chmod 755 /usr/local/bin/stash-entrypoint /usr/local/bin/uv-pip /usr/local/bin/uv-py \
    && ln -s /usr/local/bin/uv-pip /opt/uv-pip \
    && ln -s /usr/local/bin/uv-py /opt/uv-py

# Binary changes do not invalidate the runtime/dependency layers above.
COPY --chmod=755 dist/stash-linux /usr/bin/stash
LABEL org.opencontainers.image.title="Stash" \
    org.opencontainers.image.description="Native Stash with Jellyfin FFmpeg, hardware acceleration and uv" \
    org.opencontainers.image.url="https://github.com/notsafeforgit/stash" \
    org.opencontainers.image.documentation="https://github.com/notsafeforgit/stash/blob/develop/docs/container.md" \
    org.opencontainers.image.source="https://github.com/notsafeforgit/stash" \
    org.opencontainers.image.licenses="AGPL-3.0" \
    io.stash.runtime="native"
WORKDIR /config
EXPOSE 9999
HEALTHCHECK --start-period=30s CMD curl --fail --silent --max-time 10 http://127.0.0.1:${STASH_PORT:-9999}/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/stash-entrypoint"]
CMD ["stash", "--nobrowser"]
