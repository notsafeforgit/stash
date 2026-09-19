# v3 dependency maintenance

The 2026-09-19 refresh applies to v3, the shared Go backend, and runtime images.
`ui/v2.5/` retains its upstream source, manifest, lockfile, and pnpm version.

## Baseline

| Area | Updated baseline |
| --- | --- |
| Backend | Go 1.27.1; gqlgen 0.17.95; go-sqlite3 1.14.52; refreshed module graph |
| Codecs | libheif 1.23.4; libvips 8.18.6; current packages from Alpine 3.24 / the runtime's supported Debian or Ubuntu release |
| UI | React 19.3; Base UI 1.8; shadcn 4.21; Tailwind 4.3 |
| Tables | TanStack Table 9.2 with explicit features and server-controlled sorting |
| GraphQL | Apollo 4.3; GraphQL 17; current Code Generator plugins |
| Playback | Video.js 10.0.0-rc.2; scoped hls.js 1.7.3 override |
| Tooling | Node 24 LTS (24.15 minimum); pnpm 12.4.2; TypeScript 6.0.3; Vite 8.3; Vitest 5; Biome 2.5 |

TypeScript 7 is available, but the current ESLint parser supports versions below
6.1. Keep the compiler within that supported range until the parser can be
upgraded with it. Node types follow the 24 LTS runtime. Video.js packages remain
on the same current release; reassess the HLS override when updating its adapter.
Go updates keep the existing major import paths for libraries such as JWT v4,
nullable database values, and mockery v2. Moving these to new APIs or generator
configuration is a separate migration; the runtime vulnerability scan has no
affected symbols or imported packages.

Go 1.27 requires macOS Ventura 13 or newer. The compiler Dockerfile uses the 13.3
SDK and a 13.0 deployment target. The active Linux workflows use `go.mod` and
Node 24 directly. Rebuild `docker/compiler/Dockerfile` before enabling other
cross-compilation targets; the upstream compiler image predates these changes.

## Native security updates

[libheif 1.23.4](https://github.com/strukturag/libheif/releases/tag/v1.23.4) fixes
parser resource exhaustion, recursion, decoder deadlocks, and invalid memory
reads. Distro packages lag this release, so `docker/native/build-libheif.sh`
builds a checksum-verified archive against each runtime's own codec libraries.
It replaces the shared library and SONAME links; images remove the superseded
library and check the loaded version. `build-vips.sh` also builds libvips
8.18.6 with its parser security fixes, preserving the distro image formats.
Images replace its shared libraries and format modules together. HEIF codecs
are built in and external plugin loading is disabled to avoid loading older
distro plugins.

The source-built libheif version is additional to the distro package metadata:
use `heif-info --version` and `vips --version` to verify the loaded libraries.
Build images with `--pull` to receive updates for FFmpeg, AV1/HEVC libraries,
Python, and other distro packages. Update the source-build scripts explicitly
when new libheif or libvips releases are required.
Package installation also upgrades the base image's packages. Images that use
the distro pip explicitly upgrade it to at least 26.2 (26.2.1 in this refresh).

The separate `stash-s6` repository copies only the Stash executable. Its runtime
needs the same libheif fix independently; its matching refresh also moves to
Alpine 3.24 and Jellyfin FFmpeg 8 on Debian. Building a Stash image does not update
an existing `stash-s6` container.
The matching local change is `stash-s6` commit `632c242` on
`deps/security-refresh-20260919`.

Other native packages follow their supported distribution rather than building
every codec from upstream. The verified amd64 runtime versions are:

| Runtime | FFmpeg | avifenc | Python |
| --- | --- | --- | --- |
| Stash / basic stash-s6, Alpine 3.24 | 8.1.2 | 1.4.1 | 3.14.7 |
| stash-s6, Debian trixie | Jellyfin 8.1.2 | 1.2.1 | 3.14.7 |
| stash-s6 with hardware support, Alpine 3.24 | Jellyfin 7.1.3 | 1.4.1 | 3.14.7 |
| CUDA runtime packages, Ubuntu 24.04 | 6.1.1 (Ubuntu updates) | 1.0.4 | 3.12.3 |

## shadcn practice

Base UI is [shadcn's current default](https://ui.shadcn.com/docs/changelog/2026-07-base-ui-default).
Keep using local wrappers. Updating the CLI does not update copied component
source: review `shadcn add --dry-run` and `--diff` before applying changes.

Preserve shortcut suppression, lightbox portal targets, localized labels, and
mobile scrolling. This refresh merges button hover, field and checkbox/switch
focus, calendar classes, and Base UI's underlying fixes. Keep the app's tab
orientation and drawer behavior when reviewing registry defaults. Test focus
return, nested overlays, mobile scrolling, and playback after primitive updates.

## Refresh checks

v3's overrides and build-script permissions live in `pnpm-workspace.yaml`; the
existing `esbuild` and `msw` script denials are preserved. Exact release-age
exceptions record versions selected during this refresh without exempting
future versions. The esbuild overrides target only parents requesting the
vulnerable version.

Code Generator writes schema types to `generated-schema.ts` and operations to
`generated-graphql.ts`, which re-exports schema types. Both are ignored generated
files. Keep mocks and offline adapters checked against these types, including
explicit nulls and response type names.

Run the [validation sequence](../ui/v3/docs/development.md#validation), browser
tests, `pnpm --dir ui/v3 audit`, and
`go run golang.org/x/vuln/cmd/govulncheck@latest ./...`. Verify HEIC/AVIF round
trips through libvips in final images, not only in codec build stages. Do not
upgrade v2.5 as part of this workflow.

The Go scan reports no affected symbols or imported packages. Its module-level
finding GO-2026-5932 concerns the unused `x/crypto/openpgp` package, which has no
fix and is not imported by Stash.

### 2026-09-19 validation

- `make validate-fork` passed, including Go lint/integration tests, v3 lint and
  type checks, 378 unit tests, and the v2.5 compatibility baseline. The unchanged
  v2.5 UI also passed its own validation and production build.
- The 720-case Chromium/WebKit suite exposed stale fixtures and HLS request
  matchers, which were corrected. All failed cases passed on focused reruns.
  The final 48-case cover/marker run also verifies the live frame-capture
  timestamp fix without interrupting playback or losing unsaved edits.
- PWA tests passed in Chromium (4) and WebKit (3, plus the expected skip for
  Chromium-only Background Fetch). Local Chromium PWA tests use its full
  browser channel because the minimal headless shell lacks the required
  background download service.
- The v3 package audit and the application image's Python audit report zero
  known vulnerabilities. pnpm peer checks pass.
- The production v3 bundle and Linux executable build. HEIC/AVIF encode/decode
  round trips pass in the application and all three stash-s6 runtime images.
  The Ubuntu codec build and isolated runtime package check pass too. GPU
  execution, non-amd64 images, and the full cross-compiler were not tested.

These are local build and test results; no image was published or service
restarted as part of the refresh.
