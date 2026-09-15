# Native v3 TV mode implementation plan

Status: **implemented**. See [TV mode](tv-mode.md) for the current behavior,
module boundaries, and verification coverage. This document retains the
accepted implementation contract and its starting context. The user selected a native feature in the v3
rewrite, with a separate navigation item and the major Stash TV capabilities
defined below. CRT effects, gamepad support, and generated scene/marker video
preview modes are explicitly excluded. TV configuration belongs in the main
app's settings, including one default playback quality for both scenes and
markers. The implementation must be strictly type safe, maintainable, and
consistent with the existing v3 application. On iOS Safari, TV keeps playback
inline with its Reels/TikTok-style feed and controls; native video fullscreen
is not a TV presentation mode or fallback.

## 1. Product decision and completion contract

Build **TV** at `/tv` in `ui/v3/`. It is a full-viewport scene/marker feed with
vertical navigation, using the app's shared backend, authenticated Apollo
client, player engine, design system, and configuration. Ship it with the
normal v3 build. There is no separate application, iframe, plugin bundle, or
GitHub fork to create.

Add **Settings → TV** at `/settings/tv` inside the existing settings layout.
Its shared default quality provides the lower-bandwidth option through normal
v3 scene streams and on-demand transcoding. Marker playback uses the same
quality policy on its parent scene's stream, bounded to the marker range.

Keep the entire TV interface in control of presentation. Fullscreen targets
the TV container only where the browser supports it. Otherwise keep the normal
inline TV layout and hide the fullscreen action. Swiping, the action rail, metadata and app navigation
must remain available in both presentations.

Unify automatic activity tracking as shared player behavior and wire it into
TV, the scene detail player and the online scene lightbox in this implementation.
A reusable helper without the lightbox integration is not completion.

The feature matrix in section 4 is the delivery scope. Milestones establish a
safe implementation order; completing the basic swipe player is not completion
of the feature. Preserve the existing scene detail, lightbox, image, and offline
experiences when extracting shared behavior.

Use the current stack: React 19, strict TypeScript, TanStack Router/Form,
Apollo 4 typed documents, Zod, Tailwind 4, shadcn/Base UI, Lucide, dnd-kit where
reordering is needed, and the existing version-aligned Video.js 10 packages.
Keep existing versions unless a demonstrated requirement warrants a change.

## 2. Starting context and source of truth

Code and performance contracts were rechecked on **2026-09-14** against:

- Main checkout: `notsafeforgit/stash`, branch `v3-rewrite`, revision
  `9c9ea053ff3ee525206077fdf7167fe6fda8e92f`.
- Behavioral reference: [secondfolder/stash-tv at the inspected revision](https://github.com/secondfolder/stash-tv/tree/cd0c4667a994df4faeb0f71cbd0b11ac8be9d948),
  revision `cd0c4667a994df4faeb0f71cbd0b11ac8be9d948`.
- A shallow, unmodified reference checkout exists at `../stash-tv` relative to
  this repository, currently `/home/andrew/src/private/stash-tv`. Its Stash
  submodule has not been initialized and dependencies have not been installed.
- The main checkout already had an unrelated, untracked
  `ui/v3/pnpm-workspace.yaml`. Preserve user changes and inspect the current
  working tree before starting; these revisions are reference points, not
  instructions to reset either checkout.

Read [AGENTS.md](../AGENTS.md), [root guidance](../../../CLAUDE.md),
[architecture](architecture.md), [development](development.md),
[feature expectations](plan.md), and [fork policy](../../../FORK.md) first.
The actual code at implementation time takes precedence over outdated file
descriptions here. Reconcile relevant changes without repeating the product
decision or silently reducing the feature matrix.

### Recent rewrite changes included in this plan

The following commits define relevant integration and performance requirements.
Use their final implementations at the reviewed HEAD, including later fixes
to earlier changes. The current architecture guide describes these contracts;
the sections below apply them to TV and the shared activity migration.

| Reviewed commits | Contracts to preserve |
| --- | --- |
| `9c9ea053f` — Eliminate late route flashes and speed up view changes | Prepare route reveals before destination paint; skip extra reveals on browser history traversal; mount nearby Home cards and mobile actions on demand; defer freeze-frame GPU allocation/readback |
| `2e9a8d484` — Smooth view changes and harden offline PWA downloads | Restore Home seeds/positions and cached rows without retaining page DOM; warm only essential typed data; use navigable pending routes with `pendingMs: 600` and `pendingMinMs: 0` |
| `0ad148d16`, `f2263c4ae` — Shared view motion and WebKit reveal fixes | Animate bounded empty paint surfaces with shared timings and cancellation; preserve opaque media, player identity and lightbox exit/history ownership |
| `5f87eba03`, `f58621fec` — Navigation, lazy Home and mobile layout foundations | Reuse the persistent mobile navigation drawer, visual reveal leases, deferred mounting and safe-area layout helpers |
| `35566097d` — HDR AVIF previews for v3 | Use the shared typed `preview_image`/`PreviewImage` pipeline for covers and marker stills; these images remain in scope despite excluding generated video-preview playback |

Reference files, relative to the sibling `stash-tv` checkout:

| Source | Behavior to inspect |
| --- | --- |
| `README.md` | Published user-facing features |
| `docs/media-loading.md` | Feed pagination, filtering and mutation races |
| `docs/video-player.md` | Playback behavior and legacy workarounds to avoid importing |
| `docs/action-buttons.md` | Action registry, folders, pinning, repeatable actions and presets |
| `docs/state-and-config.md` | Persistent preferences and device-specific rotation |
| `packages/tv-ui/src/store/tvConfig.ts` | Complete settings inventory and shipped action layout |
| `packages/tv-ui/src/components/settings/SettingsTab/index.tsx` | Settings semantics and advanced controls |
| `packages/tv-ui/src/components/slide/MediaSlide/index.tsx` | Random starts/windows, marker-aware seeking, hold/rewind gestures |
| `packages/tv-ui/src/hooks/useMediaItems.ts` | Scene/marker loading, identity and duration handling |
| `packages/tv-ui/src/components/action-buttons/buttons/` | Individual actions and per-action configuration |
| `packages/tv-ui/src/hooks/useViewportRotate.ts` | Presentation rotation and coordinate issues |
| `packages/tv-ui/src/components/settings/KeyboardShortcutsInfo/KeyboardShortcutsInfo.md` | Keyboard behavior |

Use these as behavior references. Do not transplant the legacy player, copied
Stash frontend, Bootstrap, Formik/Yup, Zustand stores, private Video.js access,
DOM patching, or synthetic global keyboard/mouse dispatch. If adapting source
or an asset, retain its applicable attribution and notices.

## 3. Reuse map and known gaps

Paths in this table are relative to `ui/v3/`.

| Existing surface | Use and necessary boundary |
| --- | --- |
| `src/components/layout/nav-items.tsx`, `src/hooks/use-nav-hotkeys.ts` | Add a real built-in TV destination and an unused navigation chord |
| `src/components/layout/app-shell.tsx`, `bottom-tab-bar.tsx`, `mobile-navigation.tsx` | Give TV control of mobile chrome and open the existing persistent navigation drawer through `useMobileNavigation` |
| `src/styles/globals.css`, `src/hooks/use-mobile-keyboard-layout.ts` | Reuse safe-area variables and the existing viewport/keyboard layout policy at the appropriate surface boundary |
| `src/components/layout/route-viewport.tsx`, `content-reveal.tsx`, `src/core/route-transitions.ts`, `content-reveal.ts`, `motion.ts` | Preserve commit-time route reveals and shared local-view motion; the media subtree stays outside decorative animation |
| `src/core/paint-animation.ts`, `src/components/lightbox/use-lightbox-motion.ts`, `lightbox-motion-surface.tsx`, `use-lightbox-history.ts` | Reuse cancellation/paint ownership; preserve the lightbox's completed exit and single history consumption during activity migration |
| `src/components/detail/detail-route-state.tsx`, scene/performer route loaders | Reference shared-client cache warming, retained outgoing pages and navigable delayed/error states |
| `src/components/frontpage/front-page-state.ts`, `recommendation-row.tsx`, `preload-front-page.ts` | Reference scoped lightweight visit state, nearby mounting, stable placeholder geometry and targeted query warming |
| `src/components/shared/deferred-mount.tsx`, `src/components/layout/mobile-detail-chrome.tsx`, `src/components/cards/entity-card.tsx` | Mount expensive content on demand; preserve opened form state; skip hover-only tooltips and truncation measurement on touch |
| `src/components/shared/preview-image.tsx`, `graphql/data/preview-image.graphql` | Render typed scene/marker poster renditions through the common image fallback policy |
| `src/routes/settings.tsx`, `src/components/settings/settings-navigation.ts`, `settings-layout.tsx`, `setting-row.tsx` | Add Settings → TV using the existing layout, navigation, field rows and search conventions |
| `scripts/generate-settings-search-index.mjs` | Generate searchable TV settings from the settings route; do not hand-edit the generated index |
| `src/components/player/scene-player.tsx` | Retain one player root/store/native video throughout a TV session |
| `src/components/player/scene-video.tsx` | Keep ownership of the native element and direct/HLS engine switching here |
| `src/components/player/use-scene-player-sources.tsx` and `scene-player-transitions.ts` | Reuse source selection, absolute-time seeking, quality changes, recovery and pending resumes |
| `src/components/player/scene-player-sources.ts`, `scene-player-source-url.ts`, `src/core/generated-graphql.ts` | Reuse source eligibility and generated resolution values; separate TV's controlled quality policy from the global saved-label preference |
| `src/components/player/player-controls.tsx`, `player-menus.tsx`, `player-markers.tsx` | Extract a small typed control surface; share seek/quality/subtitle behavior; route fullscreen through TV's presentation owner |
| `src/components/player/use-player-transcode-session.ts` | Continue releasing outgoing transcodes and maintaining paused leases |
| `src/components/player/use-freeze-frame-overlay.tsx` | Keep minimal initial canvas buffers, deferred GPU warm-up, buffer reuse and cancellation of obsolete exports |
| `src/components/player/platform-media-effects.tsx` | Reference the public store subscription and effect-cleanup pattern for the activity observer; preserve existing Media Session/wake-lock ownership |
| `src/components/lightbox/scene-carousel.tsx` | Proven three-slot, one-active-player lifetime pattern; horizontal YARL navigation is not a vertical feed controller |
| `src/components/lightbox/scene-slide-content.tsx` | Reference active-scene identity checks, retained suspended player, marker bounds and overlays |
| `src/routes/scenes/$sceneId.tsx` | Replace route-owned automatic activity writes with the shared integration; preserve explicit manual add-play actions |
| `src/components/lightbox/lightbox-scene-player.tsx` | Pass explicit online scene/marker/offline activity scope to the shared player; enable tracking for eligible scene slides |
| `src/components/offline/use-offline-resume-writer.ts` | Preserve local offline resume writes and their separation from server activity |
| `src/components/lightbox/use-scene-lightbox.tsx`, `use-marker-lightbox.tsx` | Reference page-boundary behavior; these hooks depend on list/lightbox ownership and are not drop-in TV feed hooks |
| `src/models/list-filter/filter.ts`, `filter-ast.ts` | Canonical saved-filter decoding, AST composition and typed query variables |
| `src/components/filters/`, `src/core/saved-filters.ts`, `src/hooks/default-filter.ts` | Reuse filter selection/editing and default/conflict behavior |
| `src/components/detail/marker-edit-form.tsx`, `delete-dialog.tsx`, scene actions and shared form controls | Reuse mutation/form behavior through suitable typed inputs |
| `src/core/config.ts`, `config-schema.ts`, `src/hooks/config.tsx`, `stored-state.ts` | Typed configuration boundaries, tracked saves, browser storage and subscriptions |
| `graphql/data/scene.graphql`, `queries/scene-marker.graphql`, `mutations/scene.graphql` | Existing v3 streams, AST queries and scene/marker/activity mutations |
| `tests/browser/fixture/scene-lightbox.tsx`, `scene-lightbox.browser.ts`, `video-sources.browser.ts` | Extend real-player fixtures and retain regression coverage |
| `tests/browser/route-transitions.browser.ts`, `interaction-motion.browser.ts`, `home.browser.ts`, `scene-detail.browser.ts`, `safe-area.browser.ts` | Extend the current navigation, mounting, painted-motion and deferred-canvas regression evidence |

Three gaps require explicit work:

1. The shared player has callbacks and internal controls, but no small public
   control contract suitable for TV's customizable rail and input adapters.
   Introduce that boundary without exposing library internals. Its existing
   fullscreen override can delegate to TV, but returning `false` enables a
   native-video fallback. TV must handle every fullscreen request itself and
   supply presentation state for accurate control labels and exit behavior.
2. Quality selection currently reads/writes the global `stash-player-quality`
   label and may fall back to the original source. Add a narrow typed quality
   policy for TV so its saved default applies to scenes and marker ranges
   before media loads, including recovery. Keep preference resolution pure and
   persistence at the owning boundary; preserve existing consumers' behavior.
3. Activity tracking is not supplied automatically by the lightbox/player.
   The scene detail route currently writes the full file duration and adds a
   play on end; the online scene lightbox has no automatic activity writer.
   Replace that route logic with one shared implementation used by scene detail,
   scene lightbox and TV. `ScenePlayer.handleEnded` calls `onNext` instead of
   `onEnded` during automatic advance, so accounting must observe playback and
   completion independently of navigation callbacks.

No backend schema or database migration is expected for the main scope.
Normal playback uses `sceneStreamsV3`, as the existing v3 fragment already
does. Preserve `sceneStreams` and legacy clients. Any demonstrated backend gap
must be documented and addressed additively in fork-owned code.

## 4. Required feature matrix

Every row is required unless an explicit exception appears in section 5.
The final implementation report must map rows to code and verification.
Requirement IDs remain stable; removed IDs are not reused.

| ID | Capability | Acceptance behavior |
| --- | --- | --- |
| TV-01 | Separate navigation | TV appears in desktop navigation and the mobile navigation sheet, highlights correctly, and opens `/tv` without a plugin installed; its navigation drawer remains reachable in every state and switches top-level pages without a TV close button |
| TV-02 | Vertical scene and marker feeds | Vertical touch drag, mouse wheel/trackpad and keyboard navigation select one item predictably without previous/next item buttons; large libraries load incrementally |
| TV-03 | Saved and default filters | Scene and marker saved filters retain full v3 nested AST semantics, search and sort; first launch respects the relevant configured default; explicit all-items selection is available |
| TV-04 | Shuffle and orientation filtering | A stable seed preserves random order across pages; reshuffle creates a new session; matching-orientation filtering includes square media and composes with the selected filter |
| TV-05 | Autoplay, looping and automatic advance | Autoplay preference is respected; stop/loop/advance have one unambiguous completion policy; no duplicate advance at clip end or native EOF |
| TV-06 | Scene start and end policies | Start at resume, beginning, or a random marker/random valid position; stop at scene end, after a fixed duration, or after a sampled duration within validated bounds |
| TV-07 | Marker playback | Markers use their parent scene's normal streams at the selected TV quality with bounded scene-time playback; explicit end times, coincident markers, missing duration and invalid bounds are handled |
| TV-09 | Playback controls | Play/pause, volume/mute, playback rate, subtitles and source/quality selection share the v3 engine; audio/rate continue across swipes; quality changes preserve time/state and follow TV-27 |
| TV-10 | Seeking and timeline information | The scrubber and timestamps span 0 to the selected segment duration for markers and all scene start/window policies; scrubbing and marker-aware keyboard seeking stay inside that range; thumbnails and marker metadata resolve in absolute scene time; show current marker/tag information |
| TV-11 | Hold controls | Tap the video to play/pause and hold it for temporary 2× speed; keyboard forward/reverse holds retain speed adjustment; release/cancel restores the previous state without saving a temporary rate; unsupported negative playback rates are not assumed |
| TV-12 | Presentation | Fit/contain versus fill/crop, left-handed rail, hide/show controls, and media zoom coexist; navigation, mute, settings and restore-controls remain reachable in a bottom dock; pinned actions use that dock and the remaining rail is bounded above it |
| TV-13 | Forced landscape and supported fullscreen | Rotate the viewing surface and its controls without modifying files; offer fullscreen for the whole TV container only where supported; unavailable/rejected requests retain normal inline TV and hide the action; iOS Safari retains swipe navigation, rail, metadata, menus and app navigation; TV never requests native video fullscreen |
| TV-15 | Metadata and details | Scene title, performers, studio, tags and marker information are available; links use v3 routes and returning restores the feed context |
| TV-16 | Rating, organized status and O-counter | Update the parent scene from either feed mode, with pending/error feedback and correct normalized cache updates; counter increment/decrement/reset follow existing app operations |
| TV-17 | Tag editing and quick tags | Scene feed edits scene tags; marker feed edits marker tags and primary tag correctly; pinned tags and repeatable quick-tag presets are supported |
| TV-18 | Marker creation | Open the shared editor at a valid scene time or use repeatable quick-create presets with title/primary/additional tags and icon choice; prevent duplicate submissions |
| TV-19 | Deletion | Delete the selected scene or marker through the shared confirmation contract, retain scene file/generated-file choices, and advance/refill only after success |
| TV-20 | Configurable action rail | Add/remove/reorder actions, pin top-level entries, group actions into folders, move entries into/out of folders, edit per-action options and choose curated icons |
| TV-21 | Main app settings | All TV preferences and action layouts are configurable in Settings → TV at `/settings/tv`; shared settings persist, browser-only rotation remains local, defaults/reset work, and failures do not erase unrelated configuration |
| TV-22 | Shared activity accounting | Scene detail, online scene lightbox and TV use one automatic tracking implementation with one owner per playback session; respect global tracking/minimum-play settings, save actual watched duration/resume and eligible play counts, and exclude preloads, marker clips and offline playback from server activity |
| TV-23 | Keyboard controls | Preserve the documented navigation/player shortcuts, including holds, rotation, focus ownership, blur and visibility cleanup |
| TV-24 | Help and feedback | A discoverable guide explains touch/mouse gestures and keyboard shortcuts; loading, empty, exhausted, missing-media, request-error and retry states are explicit |
| TV-25 | Feed limits and performance | Optional item limit and bounded page/prefetch controls work; one active player and three slots bound media/DOM costs; preserve current reveal, cache-return, lazy-mount and deferred-canvas contracts; activity/progress updates do not rerender the whole feed |
| TV-26 | Removed: additional feed rules | Use the existing saved-filter editor and select one saved/default filter per feed; no separate TV rule system |
| TV-27 | Shared default quality | One persisted TV default applies before loading every scene and marker range; lower resolutions use normal v3 streams; available-quality fallback and temporary per-item overrides follow section 9 |

Action inventory for TV-20: settings, UI visibility, scene information, rating,
O-counter, organized toggle, tag editor, quick tag, create marker, delete current
media, rotation, fullscreen, volume/mute, fit mode, loop/completion mode,
playback rate, subtitles and resolution. Not every setting needs a default rail
button. Settings is an optional direct link to the main app's Settings → TV page,
absent from the default rail and freely removable or movable into a folder.

## 5. Explicit product choices and scope boundaries

- CRT effects and gamepad support are out of scope. Do not add their settings,
  shortcuts, modules or acceptance tests. Keyboard, mouse, trackpad and touch
  controls remain required.
- Generated scene montage playback and generated marker video-preview playback
  are out of scope. Do not consume their video URLs, add preview-mode toggles,
  or build a preview-specific player descriptor/time mapping. Ordinary scene
  playback, marker ranges, random scene windows, posters and scrubber thumbnail
  images remain in scope. TV must work without generated video previews.
- All TV configuration lives in the main app's Settings → TV page. One default
  quality applies to both feeds; use normal v3 sources for lower-bandwidth
  playback. TV's rail retains immediate playback controls and a link to that
  settings page.
- TV uses inline video and custom controls, including on iOS Safari. Native
  video fullscreen is excluded even as an error/unsupported-browser fallback.
  Container fullscreen is an optional enhancement to the normal inline TV
  experience; unavailable or rejected requests have no simulated fallback; support is determined at runtime, without Safari-version
  assumptions. Apply this policy to TV without changing other player consumers.
- Shared activity tracking includes migrating scene detail and enabling it in
  the online scene lightbox now. The offline lightbox retains local resume
  persistence without server activity or a new offline synchronization feature.
  Global tracking and minimum-play settings stay in their existing settings
  location; do not add competing TV or lightbox tracking preferences.
- Use the app's saved-filter editor for include/exclude combinations and choose
  one saved/default filter per feed in TV. Additional TV feed rules and stored
  JavaScript are out of scope. TV retains sorting in either direction, seeded
  Random ordering, orientation matching and item limits. Reverse ordering is a
  query direction change, not reversal of each newly fetched page.
- Preserve major behavior, not implementation quirks. Native TV uses the v3
  implicit marker-end policy: explicit valid end, otherwise the next strictly
  later marker, otherwise scene duration. The plugin used a fixed-duration
  fallback for implicit ends. Document this difference in the user guide and
  reuse one pure helper with the existing lightbox; keep its behavior stable.
- Retain the current v3 cast/PiP capabilities wherever available; do not make
  a separate receiver, native television client or offline feed part of this scope.
  These are explicit user actions, never substitutes for TV fullscreen.
- Existing plugin settings import and automatic migration are not required.
  Start in a new native namespace and leave the plugin's configuration alone.
  A later importer can be a separate, validated feature.
- Legacy development-only log panels, arbitrary event-name settings and hidden
  multi-tap developer switches are not parity requirements. Reuse the app's
  diagnostics; expose normal help visibly.

## 6. Architecture and dependency direction

Use a thin route, pure domain modules, focused effect-owning hooks and small
view components. The dependency direction is:

~~~text
TanStack /tv route
  -> TvPage / session provider
       -> feed controller -> typed Apollo requests
       -> viewing surface -> shared ScenePlayer
       -> input adapters -> typed TV commands
       -> action rail / sheets -> existing mutations and forms
       -> settings adapter -> typed UI config / local storage

TanStack /settings/tv route
  -> settings forms / action-layout editor -> same settings adapter

Scene detail / online scene lightbox / TV
  -> shared ScenePlayer activity integration (one owner per playback session)
       -> pure activity policy / visit ledger
       -> shared typed Apollo writes / cache updates

Pure TV models and policies <- consumed by hooks and components
Shared player modules      <- never import TV modules
~~~

Suggested modules, to create only as their responsibilities become concrete:

| Path beneath `ui/v3/` | Responsibility |
| --- | --- |
| `src/routes/tv.tsx` | Validate search state, set document title and mount the feature |
| `src/routes/settings/tv.tsx` | TV settings inside the main settings layout; searchable Setting* rows and composed editors |
| `src/core/player-quality.ts` | Shared semantic quality schema, typed resolution ordering and pure selection policy; no React or TV dependencies |
| `src/core/scene-activity.ts` | Shared readonly activity observations, explicit eligibility, pure visit ledger and watched-time/completion rules; no React, Apollo or TV dependencies |
| `src/core/tv/feed-types.ts` | Generated-fragment projections, item identity, query/session/event unions |
| `src/core/tv/feed-state.ts` | Pure queue transitions, page receipts, selection, history and tombstones |
| `src/core/tv/feed-query.ts` | AST/filter-to-operation conversion and deterministic query identity |
| `src/core/tv/playback-policy.ts` | Deterministic scene start/end/window and completion decisions |
| `src/core/tv/action-config.ts` | Zod action/folder schemas and pure edit operations |
| `src/core/tv/settings.ts` | Versioned settings schemas, defaults and migrations; no React dependencies |
| `src/components/tv/tv-page.tsx` | Compose the feature and its single session owner |
| `src/components/tv/use-tv-feed.ts` | Page requests, cancellation, race checks and bounded prefetch |
| `src/components/tv/tv-session-state.ts` | Bounded client/backend-scoped visit snapshots for return navigation; IDs, seed, page receipts and position only, with no entity-data or media-DOM copies |
| `src/components/tv/tv-surface.tsx`, `use-tv-navigation.ts` | Three-slot vertical track, drag/settle lifecycle and accessible navigation |
| `src/components/tv/tv-player.tsx` | Adapt the selected item/policy to the shared player, without a second playback engine |
| `src/components/tv/use-tv-input.ts` | Keyboard/pointer input-to-command translation with teardown and focus arbitration |
| `src/components/tv/tv-viewport.tsx` | Scoped rotation, coordinate conversion, portal container and supported TV-owned container fullscreen |
| `src/components/tv/actions/` | Narrow typed action components, metadata registry and rail/editor UI |
| `src/hooks/use-tv-settings.ts` | One typed settings adapter shared by the settings route and TV consumers; tracked persistence and local-storage subscriptions |
| `src/components/tv/tv-info.tsx`, `tv-help.tsx` | Focused presentation surfaces |
| `src/components/player/scene-player-controls.tsx` | Proposed shared typed control context/adapter |
| `src/components/player/use-scene-activity.ts` | Single player-level activity integration for scene detail, scene lightbox and TV; consumes typed observations and owns automatic flush scheduling through a shared mutation/cache boundary |
| `graphql/data/tv.graphql`, `graphql/queries/tv.graphql` | Small typed feed fragments and operations where existing ones overfetch |

Do not create an all-purpose plugin framework, generic repository layer,
application-wide event bus, parallel Apollo client, or a single massive
`useTv()` hook. Reuse a helper when there are concrete consumers; do not copy
lightbox implementation into TV or force TV into list-page abstractions whose
pagination and scroll ownership differ.

### Loading and update cost

Keep TV and Settings → TV code split at their route boundaries. The nav registry
and core config schemas must not import the player, rail editor or metadata
forms. Mount heavy editors/help/action configuration on first use, then retain
their drafts for that owning page/session as existing mobile drawers do. Load
picker catalogues only when needed. Do not mount every registered action's
popover/editor simply to display its lightweight rail button.

Use the existing player's typed selector/subscription path for observed state.
Keep stable commands separate from frequently changing playback observations;
only consumers such as the progress display subscribe to the fields they need.
Drag offsets, time updates and activity sampling must not publish a fresh
whole-session context through `TvPage`, the feed queue, every rail action and
the settings form on each tick. Frame/gesture writes belong at their owning
effect/event boundary, with cancellation; selection commits remain explicit
state transitions. No alternate player store, render-time ref mutation or
unsafe memoization is justified by performance.

TV still mounts exactly three feed slots. `DeferredMount` retains children once
revealed, so wrapping every admitted feed item in it would accumulate media/UI
instead of bounding the feed. Use the shared deferred/first-use patterns for
ancillary content where that lifetime is appropriate. Touch controls omit
hover-only tooltip roots and truncation measurements; accessible names and
explicit metadata views provide the information without synchronous layout work.

### Navigation and shell

Add a localized **TV** item with a Lucide TV icon after the existing Markers
item. This also avoids displacing the first three mobile quick tabs. Use
`g r` if still unused when implementing; `g t` already belongs to Tags.
Use generated route types and regenerate the route tree through the build.

Keep the desktop app header in ordinary viewing. Mobile TV owns the viewport
and supplies a visible app navigation affordance instead of the global bottom
bar. Supported container fullscreen hides ordinary app chrome while keeping the TV
interface reachable. Unsupported browsers retain their ordinary inline layout
and do not offer a fullscreen action. Make the shell
change small and route-scoped, using the router's normalized route identity
where possible. Leaving TV exits any TV-owned container fullscreen and restores
the shell and focus. Do not change layout for unrelated routes.

TV's navigation affordance calls `useMobileNavigation`; do not instantiate a
second drawer when hiding the global mobile bar. Keep TV inside the existing
`RouteViewport` and let the shell own route reveals. Its drawer lease delays
only the visual reveal, never routing, data loading or source cleanup. Preserve
the current dimmed navigation backdrop rather than adding a backdrop filter
over playing media. If TV container fullscreen is active, return to normal inline viewing
layout before opening the shared drawer so its existing portal remains visible.

Use `motion.ts` timings and the shared empty-surface reveal helpers. Route
reveals are prepared by the viewport's layout effect before destination paint,
with the transparent final frame retained until cleanup. Do not start another
entrance from `onResolved`. Browser Back/Forward traversal stays free of an
extra reveal; Smart Back retains its typed direction hint and app-navigation
reveal. Search/filter/item replacement and ordinary cache updates must not
restart route motion. Never enable native View Transition snapshots, key the
page/player by route, or animate the whole media subtree for a decorative reveal.

Warm required configuration/default/selected-filter definitions through the
shared Apollo client, as Home warms its configured definitions. The feed
controller owns paginated scene/marker requests; route loaders do not download
media, fetch the full filter catalogue, or keep a second entity-data cache.
For essential loader work, follow the current `600ms` pending threshold and
zero minimum pending duration, with TV-appropriate navigable pending/error UI.
This threshold is not a delay before swipes or an animation wait. Detail links
retain their existing typed cache-first loaders and Retry/Back behavior.

Use the existing platform URL helpers and router for deployment prefixes.
Validate URL state for feed mode, saved-filter ID, seed and optional selected
item. Mode and item identity must agree. Filter/seed changes are meaningful
history entries; per-item selection uses replacement or session history state
so Back does not traverse every swipe. Dismiss TV overlays before exiting.
Opening Settings → TV pauses playback, flushes eligible activity and releases
the source/transcode lease. Preserve lightweight feed/selection/playhead state
for Back or an explicit return link, without retaining a hidden player. Returning
from settings or a detail link restores that context; apply saved playback
defaults on return and rebuild the queue only if its query identity changed.
An outgoing TV page retained during a destination loader is already suspended
after exit intent; its continued DOM presence must not restart playback or
activity. Route navigation never waits for an activity mutation acknowledgment.

Add `/settings/tv` to `SETTINGS_NAV_ITEMS` and use the existing settings title,
layout, mobile navigation and search. Keep searchable `Setting*` rows in the
route file, as the current index generator scans that directory. Regenerate
the route tree and settings search index through the normal scripts/build.

## 7. Type safety and state contracts

These are implementation requirements, including for new tests and fixtures:

1. Retain `strict`, `noUncheckedIndexedAccess` and existing purity/type-contract
   lint. No `any`, double assertions, `as never`, broad assertion-based
   projections, or new TypeScript suppression comments to satisfy a build.
   `as const` and `satisfies` are appropriate for literal registries. Narrow
   nullable query results, unknown inputs and DOM events before use.
2. Query data and mutation variables derive from generated operations/fragments.
   Add a fragment for a real capability when needed; do not hand-maintain
   parallel GraphQL entity interfaces or manufacture partial objects typed as
   complete `SceneData`.
3. Model scenes and markers as a discriminated union. Scene actions derive
   their parent-scene target from the selected union member; marker actions
   require the marker member. Distinguish a media item's key from its parent
   scene ID, so two markers on one scene are different selections.
4. Use absolute scene time and explicit validated bounds for marker clips and
   scene windows. Adapt the real scene plus playback policy without replacing
   its duration, streams, captions or resume fields in Apollo. Model quality as
   a validated semantic preference, separately from the resolved source and
   current item's override; source URLs are runtime data, not saved preferences.
5. Use unions for state and commands: initializing/ready/empty/error feed;
   idle/dragging/settling navigation; scene/marker selection; closed/info/help/
   action popover/editor state; stopped/loop/advance completion. Avoid sets of
   booleans that permit impossible combinations.
6. Decode storage, JSON scalars, URL values and optional browser capabilities
   from `unknown` once at the owning boundary with Zod or a documented type
   guard. Components receive validated values. Parsing inside every button's
   render is not the architecture.
7. Define action config as a Zod discriminated union with per-kind fields.
   Infer TypeScript types from the schemas. A quick-tag action requires a tag
   ID; a marker preset requires its validated defaults; unrelated actions do
   not receive these fields. Use exhaustive switches or a correlated mapped
   registry that keeps each component paired with its config type.
8. Keep action folders one level deep, matching the reference behavior. Children
   are actions, not folders. Top-level pinning and child configuration should
   have different structural types, preventing invalid nested/pinned layouts.
   Validate unique instance IDs, singleton actions and essential-action rules.
9. Persist plain serializable values, never callbacks, player refs, Maps or
   library objects. Use immutable reducer/state updates. No conditional hooks,
   render-time mutation of shared state, or refs used as a substitute for
   observable state. Use `useCommittedRef` only at effect/event boundaries.
10. Pure reducers/policies receive clocks/seeds as inputs. Generate a session
    seed at a session-creation event/effect boundary and preserve it; do not
    call random/time APIs in render or resample on ordinary rerenders.
11. Activity scope is an explicit shared discriminated union for online scene,
    marker, offline or disabled playback. It is independent of TV's feed types.
    Do not infer eligibility from the presence of a scene ID or `clipRange`:
    markers and offline slides also carry parent-scene data. Observations carry
    captured scene/visit/source identity and absolute scene time, not live refs
    that a delayed write could resolve against a different selection.

Keep the shared player interface small. A typed context rendered beneath the
existing player provider can expose readonly capabilities/state and semantic
commands such as play, pause, scene-time seek, volume, rate, subtitle choice and
source preference. Presentation commands delegate to TV's viewport owner;
normal/container-fullscreen state is distinct from the video store's
fullscreen flag. Make disabled/unavailable capabilities explicit. TV's
input handlers and action rail call this same interface; they do not inspect
private Video.js fields, locate players with selectors or create another store.
Extract a narrower generated playback fragment if the current player prop
contract requires unrelated metadata.

## 8. Feed, filtering and request lifecycle

### Query construction

Reuse `ListFilterModel.configureFromSavedFilter`, `makeFindFilter()` and
`makeFilterAST()`. The differently named `makeFilterAst()` produces saved
criteria; do not confuse that representation with GraphQL input. Apply
`scene_filter_ast` or `scene_marker_filter_ast` to the correct operation.
Compose orientation into a cloned AST with AND; never mutate
the saved filter or flatten nested groups to legacy `object_filter`.

Wait for configuration/default/saved-filter resolution before issuing the
initial feed request. A missing selected saved filter, invalid AST or unresolved
default conflict must not silently produce an unfiltered request. Offer an
explicit filter selection/retry. TV selection does not change the main app's
default filters. Selecting all items is an intentional empty-filter query.

Feed identity includes backend scope, entity kind, canonical query input,
effective orientation, sort/seed, page size and item limits. Changing
identity cancels pending work and starts a new generation. Playback-only
settings such as quality, volume or fit mode do not reset the queue.

### Queue ownership and pagination

- One route-owned controller owns ordering, page receipts, selection, lightweight
  history and deleted-item tombstones. Consumers subscribe to this state; each
  component must not instantiate its own accumulator.
- Apollo owns normalized entity data. Queue records reference stable item keys
  and typed summary identities; metadata changes must reach overlays without
  rebuilding the ordered queue from a refreshed first page.
- Use independent typed page requests. Do not add a global `findScenes`/
  `findSceneMarkers` cache merge policy. Advance the next page only after that
  request succeeds, independently of rendered item count or item deletions.
- Use a constant page size within a generation; start with 20 and prefetch when
  two admitted items remain. An advanced change to page size starts a new
  generation. Never fetch page 1 with 20 and page 2 with 5 under page-based
  offsets. Deduplicate results by kind plus ID.
- Allow one next-page request per generation. Tag every page/detail request,
  completion and retry with generation/selection identity. Abort where the
  existing link supports it, and independently reject stale completions.
- Prefetch neighbor metadata/posters and at most one upcoming scene detail.
  Do not open media sources or start transcodes to preload offscreen slides.
- Keep playback resources constant: a stable active video and inert neighboring
  posters. Keep full-detail retention bounded; do not keep copies of full scenes
  in every queue entry. Lightweight history may grow with admitted items;
  distinguish that from constant player/decoder resource usage.
- End-of-feed is based on consumed server pages/count and the requested item
  limit, not the number of valid/unique items left after transformation. An
  all-invalid page must not cause either false exhaustion or an unbounded
  automatic request loop. Bound each automatic refill burst and provide an
  explicit continue/retry state if more scanning is needed.
- Errors retain the current usable item and a retry for the failed operation.
  An exhausted feed offers replay/reshuffle or filter selection rather than an
  infinite loading sentinel. A missing/unsupported item is skippable, with a
  useful reason and without silently cycling forever.

### Cached return state

Follow Home's per-client visit-lifetime pattern for a bounded cache of lightweight
TV snapshots: backend/query identity, seed, ordered IDs/page receipts, selected
item, resolved window and absolute playhead. Generate seeds at the existing
explicit session boundary; the reuse is of ownership and lifetime, not permission
to mutate caches or generate random values during render. Apollo remains the
only entity-data cache. Exactly one mounted TV controller owns requests and
playback; a retained snapshot has no timers, subscriptions or media DOM.

On return, restore the selected slot/poster and cached metadata immediately
when valid, without an unconditional page-1 refetch, new shuffle seed or
skeleton pass. Resolve missing/stale entities with targeted requests and the
existing generation/mutation checks. Preserve slot aspect ratio and rail geometry
while images or data resolve. Use `PreviewImage` and generated `preview_image`
data for scene/marker stills; retain the common rendition/fallback policy.
Navigating through TV to Home/lists must also preserve their existing seeds,
scroll offsets and virtualizer measurements under the shared restoration rules.

### Mutations and offset pagination

The existing API is page-based and does not promise a database snapshot across
requests. A random seed fixes ordering for an unchanged result set; it is not
a cursor or snapshot token. Do not claim otherwise.

For local edits that can change filter membership/order, preserve the current
selection and visited history, invalidate future page receipts, and rescan from
page 1 with already-admitted/deleted keys excluded in the controller. Replace
unvisited queued results deliberately; never let a mutation's first-page
refetch truncate accumulated history. Coalesce activity-triggered invalidation
at safe selection boundaries instead of restarting on each telemetry flush.
Use the same bounded refill policy during rescans.

On confirmed deletion, tombstone the key before applying pending pages, choose
the next surviving item (or previous/end state), and reconcile offsets. Do not
advance on mutation failure or resurrect a deleted item from an old response.
Tag/marker writes target the item captured when the editor/action opened, even
if navigation later changes the active item. Keep the visible item stable while
an editor is open. Concurrent external library edits can change future results;
document this live-feed behavior instead of adding a database snapshot subsystem
without a demonstrated need.

## 9. Playback, time and input behavior

### Stable player and vertical surface

Follow the lightbox's lifetime pattern with three fixed slots. The center owns
one mounted `ScenePlayer`; previous/next slots show posters. A small vertical
controller animates a transform, commits selection once, then recenters. Keep
the center component/store/video identity stable through swipes, source modes,
filter changes and pending loads. No per-item `key` on the player, DOM
reparenting, document-wide player tracking or extra offscreen players.

The vertical drag/settle transform and forced rotation are functional geometry
changes, confined to their stable TV surfaces. Decorative entrance/exit or
view-change effects use a separate, bounded empty paint layer; do not fade/scale
the player subtree or stack a route reveal and a second TV entrance over it.
Use the shared interruptible motion owner where appropriate, retaining its
Reduced Motion, visibility, cancellation and unsupported-browser behavior.

Feed identity and playback identity differ. `playbackKey` changes for a new
scene/marker selection or selected playback window;
ordinary quality swaps retain the current playback identity/time. During a
pending scene/detail result, suspend the retained player and release its source.
Accept detail results only for the selected scene and generation.

Retain existing absolute-time conversion, fixed clip bounds across quality
changes, source reload nonces, iOS/MMS seek policy, freeze-frame feedback and
explicit WebKit `canplay` resume. Audio/rate and temporary held-speed cleanup
retain their documented behavior. Native-fullscreen recovery remains available
to existing consumers that use it; TV stays on the inline playback path. Do not
change the codec/transcode engine to implement TV.

Preserve the current freeze-frame allocation schedule. The canvas begins with
a minimal backing buffer; GPU texture allocation/readback waits for playable
data, a paint and idle time (the existing timer fallback where needed). A real
early capture can prepare it on demand. Reuse prepared buffers without clearing
a captured frame, and cancel warm-up/export work on departure or supersession.
Do not pre-warm a full-resolution canvas on TV mount, during adjacent-item
prefetch, or from the activity hook; initial navigation and dragging must not
depend on optional canvas work.

### Playback policy

Resolve a pure playback plan once per selection from validated settings,
duration, markers and a stable seed. Resume/beginning/random starts are distinct.
Random start prefers a valid marker, otherwise samples a valid scene position.
Fixed/random lengths must be positive finite durations with ordered limits and
must fit the remaining scene. Resample only on a deliberate new selection or
session action. Reject unusable zero-length ranges visibly.

Every ready plan carries an explicit scene-time range, including starts followed
by playback to scene end. Display elapsed time as `sceneTime - range.start` and
duration as `range.end - range.start`; the slider spans `0..duration` and maps
seeks back to scene time at the control boundary. Preserve that range across
quality changes and settings round trips rather than rebasing it to the current
playhead. Sprite lookup, marker labels and activity accounting retain absolute
scene coordinates.

Use the shared marker-bound helper for marker playback at every quality.
Full-scene window settings do not redefine an explicit marker range. Centralize end
handling with the existing range effect: loop, stop or advance fires once for
both bounded clips and native EOF. Add a controlled completion policy for TV
without silently overwriting the lightbox's stored preference.

### Default quality and lower-bandwidth playback

Settings → TV exposes one **Default quality** for both scene and marker feeds.
Persist `ui.tv.defaultQuality` as a discriminated preference: best available,
or a fixed tier from a validated subset of the generated streaming enum
(excluding `ORIGINAL`). Current choices are Best available, 240p, 480p, 720p,
1080p and 4K; Best available is the initial default. Reuse a single typed
resolution ordering. Do not persist URLs, array positions or display labels,
and do not add separate scene/marker defaults.

Resolve the preference against the selected parent scene's `sceneStreamsV3`
catalog after browser and clip-mode eligibility checks. Classify endpoint kind
and resolution once in the shared source adapter using validated URL parameters
and actual source dimensions; do not infer them from display-label text or
fabricate stream URLs. A narrow controlled quality input/change callback lets
TV supply this policy without reading or writing `stash-player-quality`.
Existing scene-detail/lightbox consumers retain their current preference path.
Wait for TV config hydration and resolve quality before attaching any source,
so a low-quality selection does not briefly load the original first.

Keep source eligibility aware of the presentation policy. The current iOS
marker filter excludes direct streams to accommodate the native fullscreen
slider; that reason does not apply to inline TV. Scope that restriction to
consumers that can enter native video fullscreen, while preserving actual
codec/seek restrictions and clip bounds. TV still uses the same lower-quality
transcode selection below; inline presentation does not relax that preference.

For Best available, reuse the player's existing compatibility preference order.
For a resolution, prefer the matching offered HLS transcode, otherwise the
highest offered lower-resolution transcode. An original-resolution transcode
may qualify when known source dimensions are already below the requested tier;
use the backend's resolution semantics. Do not automatically fall back to
direct/remux or a higher-resolution stream for a lower-quality request. If no
eligible source remains, show a recoverable quality-unavailable state with
retry and an explicit option to change quality for this item. Error recovery
must respect the same policy, with bounded attempts at distinct candidates.
Show the actual selected quality when it differs from the saved default.

A rail/menu quality change overrides only the current item and preserves its
absolute playhead, clip bounds, pause state and rate. Offer **Use TV default**.
The next scene or marker, a new session, and return from settings use the saved
default; a temporary override does not save a new default. Apply these changes
at explicit selection/settings-return events, not on every render. Quality
changes do not reset feed identity, resample windows or create a new activity
visit. Other playback-only defaults changed elsewhere take effect at the next
selection rather than interrupting an active item.

Lower quality uses existing on-demand HLS transcoding; no generated video
preview is required. Explain in settings that reducing quality can reduce
bandwidth and may require server transcoding. Resolution is not an exact
bitrate cap. Respect the catalog's server-configured limits without changing
global transcoding settings or introducing a new encoding pipeline.

### Shared activity accounting

Put exactly one activity integration beneath the stable `ScenePlayer` provider.
It consumes the player's typed observations and explicit activity scope, using
one shared hook and a pure activity policy/visit ledger. Surfaces pass scope
and selection identity; they do not start their own timers, subscriptions or
automatic mutations. `SceneVideo` and control buttons remain playback/UI
primitives. The shared modules must not import TV settings, feed state or routes.
Unconfigured player consumers do not start recording server activity implicitly.

| Consumer | Required integration |
| --- | --- |
| Scene detail player | Opt into shared scene tracking; remove automatic `handleOnEnded` writes of full duration and play count; retain explicit toolbar add-play through the common mutation/cache helper |
| Online scene lightbox | Pass eligible scene scope through `SceneSlideContent` and `LightboxScenePlayer`; track the active scene, flush on swipe/close, and exclude retained or suspended playback during loading gaps |
| TV scene feed | Use the same integration, including scene windows; flush on selection change, settings navigation and exit |
| Marker playback in either feed | Pass explicit marker scope; never write parent-scene resume, watched duration or automatic play counts |
| Offline lightbox | Pass offline scope; retain `useOfflineResumeWriter` and local resume behavior with no server activity writes |

Respect `ui.trackActivity` and `ui.minimumPlayPercent` consistently across all
three online scene surfaces; the latter is a percentage in `[0, 100]`, normalized
once by the shared policy. Disabled tracking prevents new automatic writes,
including cleanup flushes. No TV-specific or lightbox-specific threshold logic.
Eligibility is full-scene playback, including intentional TV scene windows.
Do not count prefetched media, startup before actual playback, buffering or seeks
as watched duration. Offline/marker exclusions hold even when a marker's invalid
bounds leave `clipRange` absent or retained scene data remains mounted.

Use readonly observations of actual playback progress, monotonic sample time,
absolute playhead, rate, seeking/buffering state and lifecycle transitions.
The shared player adapter owns time conversion and emits a completion observation
before native EOF or range completion dispatches loop/stop/advance. Keep this
accounting signal independent of `onEnded`/`onNext`; do not call both navigation
callbacks just to record activity. Capture the outgoing observation before
source replacement, suspension or teardown so cleanup cannot read the new video
time against an old scene ID.

A visit identifies one selected scene playback, not the mounted component or
source URL. Quality changes, seeks, loops and opening the detail focus viewer
retain that visit; selecting another scene and later returning starts a new one.
One stable player has one activity owner through these transitions. Effect
reconnection/Strict Mode must not create an extra play or resend a flush.

Keep sampling local to this integration and publish UI/cache updates only at
meaningful state changes or acknowledged flushes, not every media/frame tick.
Use one bounded flush cadence across surfaces; do not add an
independent polling loop per consumer or refetch library roots on each sample.
Activity recording performs no thumbnail decoding, canvas readback or synchronous
storage serialization in the playback/gesture path. Verify that enabling it
does not cause the whole page, queue or action rail to rerender continuously.

The lightbox retains its current `useLightboxMotion`/history exit owner. Close,
Escape and Back capture the final eligible activity once while the library
finishes its exit and disposes the player. Do not bypass that exit, consume
history twice or hold navigation/animation open while mutations complete.

`SceneSaveActivity.playDuration` is **incremental**: the backend adds it to
`play_duration` in `pkg/sqlite/scene.go`. Track acknowledged and pending deltas;
serialize per-scene writes through the shared persistence boundary, including
outgoing flushes during visit/surface handoff. Advance the local acknowledged
watermark only on success. Keep pending request identity after the observer
unmounts; late responses cannot update a new visit's ledger. Save absolute
resume on pause, selection change and controlled exit; reset it to zero on
original-scene EOF, not at an arbitrary scene-window boundary. Periodic bounded
flushes reduce loss on page close. Do not send the entire scene duration after
a short view or depend on `onEnded`, which auto-advance can bypass.

Centralize generated activity/play-count mutations and targeted Apollo cache
updates so detail history/statistics and lightbox/TV overlays agree. The current
activity mutation returns a success flag rather than an updated scene; reconcile
acknowledged values or use a targeted refresh without resetting list/feed order.
Capture backend/scene identity for writes and cache updates; do not let an old
resume response overwrite a newer acknowledged position. Keep serialization
scoped to the current Apollo/backend instance; no application-wide event bus or
general analytics framework is needed. Manual add-play remains a deliberate
action, separate from automatic eligibility and visit deduplication.

Count a play at most once per visit when actual eligible playback reaches the
configured fraction of the original duration; with a zero threshold, require
actual positive playback. Loops and quality switches do not duplicate it. Use a
documented watched-time calculation with seek/stall gaps excluded and cover
playback rate changes in tests. Incremental writes are not idempotent: do not
blindly retry an ambiguous transport failure and promise exact-once delivery.
Mark ambiguous deltas as uncertain and exclude them from later automatic flushes
so the next successful request cannot silently send them again. Keep confirmed
failures visible and document page-close delivery limits. Apply the same
no-blind-retry rule to play-count increments. A backend idempotency extension
is a separate additive decision if required.

### Input arbitration

All inputs dispatch one typed command union. Never simulate keyboard events to
drive other handlers. Establish ownership in this order: modal/form/menu,
focused slider or editor, media zoom/pan, rail scrolling, then feed navigation.
Use local pointer capture and a gesture threshold/axis decision; honor canceled
events and modified keyboard shortcuts. One trackpad gesture must not skip
multiple items because of momentum.

Default shortcuts follow the reference: Up/Down previous/next, Left/Right
marker-aware seek, Space play/pause, D delete dialog, E tags, F supported container fullscreen, I info, L loop, M mute, O presentation rotation and S
subtitles. Hold Left/Right
for reverse/forward control; while holding, Up/Down adjusts speed rather than
moving to another item. Suppress conflicting shared-player shortcuts only inside
the TV surface. Escape closes an overlay or exits fullscreen, staying
on the TV route. Closing an overlay must not also advance the feed.

Reverse control uses bounded repeated scene-time seeks because native negative
playback rate is not a portable contract. Rate/seek holds release on pointer
cancel, blur, visibility change and route teardown. A forward-speed
hold across automatic advance follows the existing player lifetime contract and
restores the original rate on release; stale seeks never target a new selection.

Apply rotation once in the shared coordinate/input adapter so keyboard and
pointer controls agree with the displayed orientation.

### Presentation, portals and accessibility

Retain `playsInline` on the actual native video through all source changes and
keep native video controls disabled. Inline playback prevents the automatic
fullscreen transition on iPhone; it does not bypass autoplay/audio permission.
See [WebKit's inline playback policy](https://webkit.org/blog/6784/new-video-policies-for-ios/).

All TV presentation controls, including the rail, touch player button and `F`
shortcut, delegate to one TV viewport owner. Reuse the existing
`onToggleFullscreenOverride` through a typed adapter: report the command as
handled synchronously and never return `false`, even if a container request
later fails. TV must not call `webkitEnterFullscreen`, fullscreen the video
element, or fall through to the player's store-level fullscreen toggle. Route
any additional player fullscreen gesture through the same owner or disable it
for TV. Extend the small public contract for controlled presentation state if
needed; do not patch Video.js internals or add global DOM interception.

Request fullscreen only on the stable TV container containing the video,
vertical navigation, rail, metadata and overlay portal host. Check capability
at runtime, request from a user action, and handle rejection by retaining the ordinary inline view and hiding the action.
Track actual container entry/exit through the Fullscreen API; show the correct action
label for each mode. Synchronize browser-initiated exit and Escape with this
state, restoring the prior layout without remounting the player. Route teardown
exits TV-owned fullscreen and cleans up listeners and pending requests. See the
[Fullscreen API contract](https://fullscreen.spec.whatwg.org/#api).

Use a TV-scoped viewport with dynamic viewport units and the shared safe-area
variables/layout helpers. Reuse the keyboard viewport policy at the appropriate
footer/editor boundary, with one owner per inset and no extra render/frame
between Safari's viewport pan and its layout correction.
Reflow with Safari's changing toolbars, device orientation and software keyboard
so controls stay reachable in the available viewport. The ordinary Safari tab
must provide the full TV UI.
Rotation swaps logical dimensions and transforms only that surface. Supply
Base UI overlays with an appropriate stable portal container and normalize
coordinates at one boundary. Do not rotate `body`, patch global event prototypes
or dispatch replacement mouse events. Test sliders, folder popovers and editable
sheets in both orientations and while fullscreen.

Use existing shadcn wrappers, semantic colors, `cn()`, react-intl and Lucide.
Read the relevant component documentation before implementing new compositions.
Every icon action has an accessible name, touch targets are at least 44 CSS px,
dialogs/sheets have titles, focus restores correctly, and reduce-motion settings
remove nonessential transitions. Custom media zoom remains available; preserve
the app's general page-zoom and text-selection policy.

Hidden UI retains a stable restore-controls affordance, app navigation and mute.
Use transparent icon controls with subtle shadows and comfortable tap targets.
The bottom dock must not paint a panel over portrait video; its empty space
passes taps and swipes to the video, including when the other UI is hidden.
Settings remains accessible through app navigation. Keep essential controls
outside fading/inert ancestors. Container
fullscreen and ordinary inline viewing retain the same TV controls and swipe behavior.
Explain the two presentations in help and verify the inline experience on
physical iPhone/iPad Safari devices.

## 10. Actions and configuration

### Main app settings ownership

Settings → TV is the configuration home, accessible without opening a TV feed.
Save changes automatically using the shared settings conventions and save indicator:
switches/selections immediately, text/numbers on blur or Enter. Keep serialized
TV-key writes and recover failed saves with Retry; do not add a Save/Reset toolbar.
Group playback (including Default quality), feed behavior/limits,
presentation, and action-layout editing using existing settings sections and
TanStack Form/Zod patterns. Device-only rotation is edited here too, clearly
labeled as local. The optional TV settings rail action navigates here directly
with return context. The default Playback folder contains the immediate playback
controls; no quick-settings modal or mandatory gear is needed. Saved preferences remain
owned by the main settings page; immediate playback menus and metadata editors
remain available in the viewing surface.

Show the action-rail editor directly in its settings section, without a
Customize button. Keep its module lazy and mount it automatically when the
settings page opens; detailed action forms still open on demand.

### Actions

Separate action definitions, typed config, presentation and mutation commands.
Buttons declare only the capabilities they consume. Repeatable quick-tag and
marker-preset actions have unique instance IDs; action kind is not a React key.
Curated icon IDs map to Lucide components at the UI boundary.

Support one open action popover/folder at a time within the TV session. Use
dnd-kit for the rail editor and keyboard-accessible move controls. Present the
same order in the editor and the rail. Pinned entries remain reachable when
the rest of the rail scrolls. Settings can be removed or nested; restore-UI must
remain at the top level; only top-level entries can be pinned. Validate these rules
at both the editor and persisted-data boundary.

Unknown action kinds or future config versions render an inert explanatory
placeholder and remain recoverable, instead of crashing or disappearing on
save. Retain raw unknown data only in the storage/compatibility envelope;
never pass it as a pretend known action config. Refuse destructive rewriting
of a future-version settings document unless the user explicitly resets it.

Reuse scene/marker mutations and cache/invalidation helpers. Use pending state
per target/action so repeated clicks cannot duplicate writes. Scene rating,
organized and O-counter operations target the parent scene in either feed.
Tag changes distinguish scene tags from marker primary/additional tags.
Quick marker creation captures a valid absolute scene timestamp and validated
preset; manual creation uses the shared editor. Preserve relevant fields when
an update mutation requires a full marker input. Editors must report failures
and retain drafts rather than advancing/closing as if a save succeeded.

Deletion uses the existing confirmation/form semantics. Do not carry over the
plugin's destructive-button autofocus if the shared v3 dialog uses safer focus.
The keyboard shortcut opens the same dialog and never deletes immediately.

### Persistence and defaults

Add a versioned `ui.tv` namespace to `IUIConfig` and its Zod codec. Persist
shared TV settings through the existing tracked `configureUISetting` path.
Write only that key, preserve other UI/plugin fields, and serialize/coalesce
pending TV saves using the latest draft. Do not replace the full configuration
object. Avoid importing React components into core config types.

Store forced presentation rotation in a separate versioned browser key scoped
by the normalized backend mount identity. Reuse the current platform/storage
helpers; two servers behind different prefixes must not share TV-local state.
Keep this feature's persistence ownership explicit rather than silently adding
competing global player storage writers. Active volume/rate live in the one
player store; persisted TV defaults initialize it and deliberate user changes
update the settings adapter. They do not repeatedly overwrite it on rerenders.

Validate finite volume/rate values, positive page/limit/window values,
ordered duration limits, IDs and action layouts. Hydration completes before
saving defaults. An invalid configuration shows a usable recovery/reset path
without automatically overwriting the original. Settings use TanStack Form
with Zod, tracked save/error feedback and local drafts. Browser storage failure
preserves in-page operation. Cross-device saves follow the app's existing
last-write behavior; this feature does not introduce a distributed editor.

Initial defaults: scenes using the configured default filter; saved sort order
(Random is a sort choice, with no separate shuffle toggle); resume start;
scene-end completion with automatic advance;
TV autoplay enabled but still subject to the app's global autostart preference
and browser permission; Best available quality for both feeds; fit/contain;
normal orientation; right rail; UI visible; page size 20; two-item prefetch
threshold; no item limit. Start muted is enabled by default and configurable in
Settings → TV. Apply it when entering TV, subject to browser autoplay permission;
retain the viewer's audio choices between items. Resolve the initial rate from
shared preferences, otherwise use 1x. Respect the
existing maximum-short-clip loop preference through the single completion policy.

Ship a compact default rail with essential actions and metadata/playback folders,
while making the complete action inventory available in its editor. Provide
explicit reset-to-defaults and replayable help. Treat control layout changes as
UI changes; they must not refetch the feed or remount the player.

## 11. Implementation milestones

### A. Establish typed foundations and navigation

- Recheck current contracts, including the reviewed performance commits in
  section 2, and compare the feature matrix with reference code.
- Add pure settings/action/feed/playback models, shared activity contracts and
  generated feed operations.
- Add the localized TV nav entry, `/tv` route, shell ownership and error/empty
  scaffolding, with the feature loaded through normal route code splitting.
- Integrate the existing route viewport/mobile drawer and targeted cache warming;
  verify loader pending/error behavior without duplicate transitions or requests.
- Add `/settings/tv` to the main settings layout/navigation and establish its
  validated persisted defaults, including the shared quality preference.
- Keep domain modules independent of React and preserve unknown config data.

Exit evidence: typed route/query/config boundaries; full AST query construction
verified; direct navigation under root and a deployment prefix works.

### B. Build the scene feed and shared player boundary

- Implement the route-owned queue, stable seed, page receipts, cached return
  snapshots and retry states, with no entity-data copies or offscreen players.
- Introduce the narrow shared player controls and controlled quality policy.
- Establish typed activity observations and the single shared player integration;
  migrate scene detail and wire eligible online scene lightbox/TV playback to it.
  Remove the old automatic route writer when enabling the shared owner.
- Wire inline playback and TV-owned presentation controls, including capability
  detection and rejection handling, before exposing any fullscreen action.
- Implement the stable three-slot vertical surface and command arbitration.
- Make scene playback, quality changes, source suspension and return navigation
  work before adding advanced controls.

Exit evidence: the native video/store identity survives multiple swipes and
pending loads; only one source plays; old page/detail responses are ignored;
fullscreen actions never fall through to native video fullscreen; existing
player/lightbox browser regressions still pass; scene detail, scene lightbox
and TV use the same automatic activity path with no duplicate route writer.
Warm return restores the same selection/seed without placeholder flashes, and
delayed media does not trigger eager full-size canvas warm-up or block navigation.

### C. Complete playback and media modes

- Add marker mode with shared marker bounds and the same saved quality policy
  as scenes; complete source fallback and temporary per-item overrides.
- Add scene start/end policies, stop/loop/advance, thumbnails, marker-aware seek
  and hold/rewind control.
- Complete shared activity accounting, including captured visit identity,
  serialized deltas, auto-advance, lightbox swipe/close and exclusion policies.
- Verify that activity/progress updates stay local and do not drive feed-wide
  rendering, per-tick Apollo writes or repeated page invalidation.

Exit evidence: correct absolute scene time and marker bounds at every quality;
the configured source is used from first load and across swipes/recovery;
each completion is handled once; autoplay refusal and pause/rate continuity are
exercised; the same playback sequence yields consistent eligible watch stats
in scene detail, the scene lightbox and TV, while marker/offline playback does
not write server activity.

### D. Complete metadata actions and action customization

- Add every action in TV-16 through TV-20 and the remaining playback actions.
- Reuse shared forms and mutations, adding narrow extension props where needed.
- Implement folders, pinning, repeatable presets, icon choice, reordering and
  malformed/future-config recovery.
- Load heavy editors and their option queries on demand, preserving drafts
  after first use and keeping the initial rail inexpensive to mount on touch.
- Handle deletion and filter-affecting mutations without corrupting pagination.

Exit evidence: target identity is preserved across async writes; failed writes
retain drafts/selection; configured actions survive reload; all layout constraints
are enforced and usable with a keyboard.

### E. Complete presentation, input and preferences

- Add left-handed layout, fit/crop, hide/restore controls and rotation.
- Complete keyboard mappings, speed holds and cancellation behavior.
- Complete Settings → TV forms, action-layout editing, shared/local persistence,
  limits and help; verify settings search and return state.
- Validate inline iOS playback, normal/container presentation, mobile portals,
  gestures and desktop header width with the new navigation entry.

Exit evidence: every required feature-matrix row has an implementation and an
identified automated or manual acceptance check. No major feature is silently
deferred to make the first release appear complete.

### F. Validate and document the finished feature

- Run the appropriate gates below and resolve regressions in shared consumers.
- Record comparative performance evidence against the reviewed baseline for
  startup, cached return, sustained swipes and shared activity integration.
- Update architecture/development references for actual shared-contract changes.
- Write `docs/tv-mode.md` with user behavior, settings, controls, persistence,
  intentional differences and browser limits; link it from the root doc index.
- Keep this plan marked as a plan and record completion evidence against its
  matrix. Do not describe proposed behavior as already implemented.

## 12. Verification strategy

Use behavioral tests for the difficult boundaries, rather than tests that
mirror JSX or simply assert that an action registry contains its own keys.

| Level | Required evidence |
| --- | --- |
| Type contracts | Generated variables/results; exhaustive item/action/command dispatch; invalid action/config combinations rejected; no unsafe assertions or new suppressions |
| Pure unit tests | AST preservation/composition; deterministic seeded order/windows; marker bounds; page receipts and deduplication; stale generations; mutation/tombstone reconciliation; configuration migrations and layout validation |
| Quality policy | Typed preference decoding; exact/lower/missing tiers, smaller originals, server limits and browser/clip eligibility; scene/marker parity; overrides reset on selection/return; recovery cannot silently escalate quality |
| Presentation contract | Supported, absent and rejected container-fullscreen requests; rail/touch/keyboard delegation; native-video fullscreen APIs are never called by TV; accurate enter/exit labels, retained overlay/gesture access, teardown and stable player identity |
| Controller integration | Delayed/out-of-order pages, rejected detail requests, empty/invalid pages, saved-filter removal, filter changes during requests, retry and deletion while stats/tags update |
| Activity policy tests | Playback intervals versus seeks/stalls; percentage thresholds and zero-threshold positive playback; tracking disabled; rate changes; EOF versus window-end resume; visit/loop/quality-switch deduplication; failures, uncertain increments and serialized additive deltas |
| Shared activity integration | The same playback sequence through scene detail, online scene lightbox and TV records equivalent duration/resume/play counts; swipe/close/settings-exit flushes; captured outgoing identity; loading gaps and Strict Mode; no duplicate route writer; manual add-play preserved; marker/offline scopes never issue server activity |
| Startup and return performance | Targeted definition/detail warming without duplicate queries; retained outgoing page and navigable pending/error state; cached return preserves seed/selection/poster without a page-1/skeleton reset; Home/list return retains prior scroll and geometry |
| Motion and mounting regressions | No native snapshot capture, late reveal flash or extra browser-Back fade; one persistent mobile drawer; stable video through motion; real intermediate paint and cancellation; only three feed slots and on-demand heavy editors/queries |
| Player and observer cost | Delayed media causes no eager GPU readback; early capture still works and obsolete warm-up/exports cancel; no adjacent-item media requests; activity/time/drag observations do not continuously rerender the page/queue/rail or write Apollo on every tick |
| Mutation/config tests | Correct scene versus marker target, double-click prevention, failure recovery, normalized metadata updates, unknown-field preservation, backend-prefix isolation and no TV writes to global player quality |
| Chromium and WebKit | Real TV surface plus production player; inline element identity across direct/HLS/marker/quality/presentation transitions; first media request honors TV quality; wheel/touch navigation, holds, zoom arbitration, delayed pages and video errors |
| Browser interaction | Main settings navigation/search/automatic saves/retry; unreadable-settings recovery; returning preserves feed/position and applies default quality; rotated sliders/popovers/dialogs, small safe-area viewport, focus/escape, hidden-UI recovery, reduced motion, action editor and nav overflow |
| Manual devices | Physical iPhone/iPad Safari inline autoplay/audio/MMS, toolbar/keyboard resizing and rotation; swipe/rail/metadata/navigation remain usable after presentation and quality changes; Android Chrome touch; desktop keyboard/mouse/trackpad, including hold cancellation on blur |
| Regression | Scene detail, scene/marker lightbox, image lightbox and offline player still satisfy their existing contracts, including native fullscreen and source eligibility for consumers that support it |

Extend the existing route-transition, interaction-motion, Home, scene-detail
and safe-area fixtures where the shared contracts change. Retain the new tests
for a committed page never painting clearly and then dimming, browser history
staying still, nearby card mounting, cached Home returns and deferred canvas
readback. Extend lightbox activity tests through its real exit/history path.
Assertions should check observable behavior, request/mount counts and captured
paint frames, not simply the presence of an animation attribute or a timer.

For shared Home/detail/lightbox changes, compare the reviewed baseline and
implementation with the same representative fixture data and device/browser
conditions. TV has no pre-implementation runtime baseline: measure its cold
entry, warm return and sustained swipes against the resource/request limits,
and compare activity enabled/disabled on the same surface. Record network
requests, mounted media/expensive views, relevant React commits, canvas
allocation/readback timing and visible frame/long-task behavior where measurable.
Use traces to investigate regressions; do not invent FPS/latency claims or add
brittle wall-clock CI thresholds. Structural limits and request/paint behavior
belong in regression tests; record which physical-device measurements ran.

Extend the existing `*.browser.ts` fixture suite with a TV fixture rendering the
real controllers/components. Its current network harness rejects non-GET and
off-origin traffic: use a typed in-memory Apollo link or narrowly registered
fixture operations rather than disabling that protection. Fixtures must not
contact the user's live library or mutate real media. Use actual generated
MP4/HLS fixtures for playback assertions, not a mocked replacement for the player.

During iteration, run focused Vitest and browser cases for the changed
contracts. Before completion, from the repository root:

~~~sh
make ui-v3-only
make validate-ui-v3
make test-ui-v3-browser
~~~

The build regenerates route information; do not hand-edit generated files to
make standalone type checks pass. `validate-ui-v3` includes generation, Biome,
purity/type-contract lint, TypeScript, formatting, locales, unit tests and the
pinned v2.5 compatibility check. Browser tests run Chromium and WebKit; install
the documented browser prerequisites when necessary.

If backend/schema code changes, also run the affected Go tests and required
generation/compatibility checks. Before a later push, the repository's complete
gate is the documented `make generate`, `make ui`, `make ui-v3-only`, then
`make validate-fork` sequence with both UI dependency trees installed. Publishing
and deployment are separate actions governed by the deployment runbook.

Only claim device behavior that was actually exercised. Emulator/desktop WebKit
success does not establish physical iPhone inline/autoplay/MMS or viewport
correctness. Report any unavailable manual checks explicitly, with the exact
behavior still needing verification.

## 13. Final acceptance and next-context instruction

- [ ] TV is a separate native v3 navigation item and works without stash-tv.
- [ ] All 25 requirements listed in section 4 are delivered under the explicit
  section 5 choices.
- [ ] One stable player and one active playback source serve the TV session.
- [ ] The reviewed motion, cached-return, lazy-mount and deferred-canvas contracts
  survive; shared activity does not introduce feed-wide update work, and the
  relevant browser regressions and comparative performance checks are recorded.
- [ ] Saved AST filters, queue generation and mutation reconciliation are correct.
- [ ] Scene-time bounds and scene/marker activity eligibility remain correct
  across quality changes.
- [ ] One shared automatic activity implementation is wired into scene detail,
  online scene lightbox and TV; manual add-play and local offline resume retain
  their distinct behavior, and no duplicate automatic writer remains.
- [ ] Settings → TV owns configuration; one default quality applies to every
  scene and marker before loading, with no generated video-preview dependency.
- [ ] All actions, presets, folders and settings remain strictly typed at runtime
  boundaries and throughout the application.
- [ ] Mobile gestures/rotation/portals and keyboard/pointer cleanup are verified.
- [ ] iOS Safari keeps video inline with the TV feed/rail/metadata; unavailable
  or rejected container fullscreen retains ordinary inline TV and hides the action
  with no native-video fallback.
- [ ] Existing v3 player/lightbox/offline behavior and v2.5 compatibility survive.
- [ ] Relevant checks pass and untested device cases are recorded honestly.
- [ ] Current user/architecture documentation describes the final implementation.

Suggested instruction for the implementation context:

> Implement `ui/v3/docs/tv-mode-plan.md` in the v3 rewrite. The decision is to
> build native TV at `/tv` with its own navigation item and the complete required
> feature matrix, excluding CRT effects, gamepad support and generated video
> preview modes. Put TV configuration in main Settings → TV at `/settings/tv`,
> with one default quality for both scenes and markers using normal v3 streams.
> Keep video inline on iOS Safari with the complete Reels/TikTok-style TV UI.
> Tap the video to play/pause, hold for temporary 2× speed, and seek with the
> scrubber. Keep navigation, mute and pinned actions in a reachable bottom dock.
> Offer fullscreen for the whole TV container only where supported. Unsupported
> or rejected requests retain normal inline TV; never use native video fullscreen.
> Unify automatic activity tracking and wire the same implementation into TV,
> scene detail and the online scene lightbox. Replace the route's automatic
> writer, retain manual add-play/offline resume behavior, and verify one owner
> per playback session; a reusable helper without lightbox integration is incomplete.
> Preserve the recent performance contracts reviewed through `9c9ea053f`,
> including shared paint-surface motion, cached returns, on-demand mounting,
> deferred canvas work and efficient activity observations. Follow the current
> code if newer commits refine these contracts, and verify the performance cases.
> Follow the typed boundaries and shared-player invariants,
> preserve unrelated working-tree changes, and complete the milestones through
> verification and documentation. Use the sibling stash-tv checkout as a behavior
> reference; do not create a fork or import its legacy frontend. Treat the explicit
> advanced-customization choice in section 5 as part of the scope. Do not stop
> after a minimal swipe feed or use unsafe casts to make the implementation pass.
