# v3 dependency maintenance

The 2026-09-19 refresh applies to v3, the shared Go backend, and runtime images.
The v2.5 source, manifest and toolchain are now retired from this branch; they
remain preserved in the frozen compatible release.

## Baseline

| Area | Updated baseline |
| --- | --- |
| Backend | Go 1.27.4; gqlgen 0.17.98; go-sqlite3 1.14.53; refreshed module graph |
| Codecs | libheif 1.23.4; libvips 8.18.6; current packages from Alpine 3.24 / the runtime's supported Debian or Ubuntu release |
| UI | React 19.3.1; Base UI 1.9; shadcn 4.23; Tailwind 4.3.5 |
| Tables | TanStack Table 9.3 with explicit features and server-controlled sorting |
| GraphQL | Apollo 4.3.8; GraphQL 17.1; Code Generator 7 (CLI) / 6 (TypeScript plugins) |
| Playback | Video.js 10.4.0; scoped hls.js 1.7.4 override |
| Tooling | Node 24 LTS (24.15 minimum); pnpm 12.21.0; TypeScript 7.0.2; Vite 8.3.8; Vitest 5.1; Biome 2.6; Playwright 1.65.1 |

v3 uses TypeScript 7 for builds and app/browser type checks. Following
[Microsoft's compatibility setup](https://devblogs.microsoft.com/typescript/announcing-typescript-7-0/#running-side-by-side-with-typescript-6-0),
`@typescript/native` aliases `typescript@7.0.2`, while `typescript` aliases
`@typescript/typescript6`. The latter supplies the supported 6.0.3 JavaScript
API to ESLint and code-generation tools. `tsc` runs 7.0.2; `tsc6` is only the
compatibility executable. Keep both aliases until those tools support the
native compiler API. See the [v3 compiler workflow](../ui/v3/docs/development.md#typescript-7).

Node types follow the 24 LTS runtime. Video.js packages remain on the same
current release; reassess the HLS override when updating its adapter.
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
Build images with `--pull` to receive updates for AV1/HEVC libraries, Python,
and other distro packages. Update the source-build scripts explicitly when new
libheif or libvips releases are required.

FFmpeg is Jellyfin's build, pinned to one exact version in
`docker/runtime/jellyfin-ffmpeg.env` together with the fingerprint of
Jellyfin's repository signing key (`docker/runtime/install-jellyfin-repo.sh`
refuses any other key). The image, the CI media tests
(`scripts/install-media-test-runtime.sh` installs the same release on the
Ubuntu runners) and `make test-media-image` all use that pin, so FFmpeg-backed
tests exercise the FFmpeg that ships. `make test-media-image` builds the
FFmpeg-dependent test packages on the host and runs them inside the image
(`MEDIA_TEST_IMAGE`, default `ghcr.io/notsafeforgit/stash:develop`); run
`make ui` first. Jellyfin's repository retains earlier releases, so a pinned
version stays installable; bump it deliberately in that one file.
Package installation also upgrades the base image's packages. Python package
installation uses uv; the images no longer install or bootstrap distro pip.

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
| Stash image (`docker/ghcr/Dockerfile`), Debian trixie | Jellyfin 8.1.3-2 (pinned) | 1.4.2 (source build) | 3.14.10 |
| CI media tests, Ubuntu 24.04 runners | Jellyfin 8.1.3-2 (same pin) | Ubuntu package | runner default |
| Optional local `make docker-cuda-build` image, Ubuntu 24.04 (not published) | 6.1.1 (Ubuntu updates) | 1.0.4 | 3.12.3 |

## 2026-11-15 refresh

Player and engine updates with user-visible fixes:

- Video.js 10.0.1 → 10.4.0. 10.1 stops extrapolating stalled native media
  time and samples playback time on animation frames; 10.3 fixes CEA-608/708
  captions on the hls.js path, re-rendered hls.js text tracks in Safari and
  rendition ids, and reports a pending autoplay as starting; 10.4 adds text track
  and time-slider marker APIs and applies media prop updates at commit. Stash
  drives its own position slider, so 10.2's seek-while-dragging change to
  `<TimeSlider>` does not apply.
- hls.js override 1.7.3 → 1.7.4 (see [player](../ui/v3/docs/player.md)).
- Base UI 1.8 → 1.9: drawer swipe and iOS keyboard fixes, combobox mount
  performance and stale focus fixes. The WebKit touch-click patch is still
  required and moves to `patches/@base-ui__react@1.9.0.patch` unchanged.
- Go 1.27.1 → 1.27.4 for the net/http, crypto/tls, crypto/x509, cgo and go
  command security fixes. golang.org/x modules, gqlgen 0.17.98 with
  gqlparser 2.5.64, go-sqlite3 1.14.53 (fewer cgo crossings per step and an
  error instead of a crash after `Close`), chi, koanf, gojq and surf move to
  their current releases. golangci-lint 2.13.2 cannot read Go 1.27.4's export
  data, so `make lint` moves to 2.15.0. Its newer vet and errcheck found two
  error messages that printed a stash-box index pointer instead of the index,
  and a lock wait that leaked its goroutine after the timeout.
- Apollo, React, Tailwind, TanStack, Vite, Vitest, React Intl, Zod, dash.js,
  GraphQL and graphql-ws take their current patch and minor releases. pnpm
  12.21.0 includes the bundled node-gyp security update. Playwright 1.65.1
  moves the pinned browser image to `v1.65.1-resolute` by digest. Its WebKit
  27.2 mobile emulation, like 26.6, hides the Fullscreen API as on iPhone, but
  now also deletes a page's own `Element.prototype.requestFullscreen` once the
  document element exists. The TV rejection test therefore stubs
  `HTMLElement.prototype`; the app's capability check is unchanged.
- Biome 2.6 adds Markdown and YAML formatting, which stays disabled so
  documentation tables and pnpm's workspace file are not rewritten, and the
  `noComponentHookFactories` rule, which is off because typed factories run once
  at module scope and tests declare local probe components.

Deliberately unchanged:

- chromedp 0.16 and its cdproto snapshot. chromedp 0.23 replaces its action
  and command API with generics; moving the CDP scraper is a separate migration.
- GraphQL Code Generator 8 and its TypeScript 7 plugins, released 2026-11-07
  after a long alpha series. They only change generated types; adopt them with a
  deliberate regeneration review.
- goqu 9.19. 9.20 to 9.25 (2026-11-03 to 11-05) stop prefixing SQLite upserts
  with `INSERT OR IGNORE`, so a custom field with a null value fails its NOT
  NULL constraint instead of being skipped, and they change typed-nil
  comparisons from `= NULL` to `IS NULL`. Their security fixes cover SQL Server
  string interpolation and untrusted identifiers; Stash uses SQLite with
  constant identifiers. Adopt them with a review of every upsert and nullable
  comparison.
- `@types/node` stays on the 24 line to match the Node 24 LTS runtime.
- TypeScript 7.0.2 is already the current stable release.

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
restore the retired v2.5 dependency tree.

The Go scan reports no affected symbols or imported packages. Its module-level
finding GO-2026-5932 concerns the unused `x/crypto/openpgp` package, which has no
fix and is not imported by Stash.

### 2026-09-19 validation

- The TypeScript 7 migration passed the full v3 validation/build, embedded-UI
  tests, and Chromium/WebKit PWA tests. Native watch detected and recovered from
  an intentionally introduced type error. On this workstation, one comparison
  of the same configuration took 10.39s / 10.01s with TypeScript 6 and
  1.48s / 1.62s with TypeScript 7 for app / browser checks respectively. All 289
  production JS/CSS chunk names and hashes remained unchanged.
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
