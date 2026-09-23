# TV mode

TV is a native v3 navigation item at `/tv`. Configure it in **Settings → TV**
at `/settings/tv`. Switches and selections save immediately; text and numbers
save on blur or Enter, like the other settings pages. The implementation follows
the [accepted plan](tv-mode-plan.md), including its recent performance constraints.
It needs no Stash TV plugin or separate application.

## Playback and controls

Choose a scene or marker feed, an app default, saved filter, or all items.
Saved search, criteria groups and sort retain the library's filter semantics.
Build include/exclude combinations in the app's saved-filter editor and select
that filter in TV. Choose **Random** in **Sort order** for a seeded order
that stays stable as you keep scrolling. Matching orientation
uses the viewing surface, including forced rotation, and includes square videos.

The feed loads more items automatically as you scroll, until the matching
library is exhausted. Request sizing and loading thresholds are internal;
there are no paging controls or item caps for a viewing session.

Swipe vertically, use a wheel gesture, or press Up/Down to change videos.
Tap the video to play or pause, hold it for temporary 2× playback, and use the
scrubber to seek. There are no separate transport buttons. Left/Right keys seek
between markers or by ten seconds; holding those keys retains forward-speed
and reverse-scrubbing controls, with Up/Down adjusting the held speed. Release,
blur, visibility changes and zoom gestures restore the previous rate and pause
state. A swipe cannot also become a tap or a speed hold. The guide lists the
remaining shortcuts.

Scenes can start at resume, beginning, a sampled marker, or a sampled position.
Playback can use the full scene or a fixed/random duration window, with stop,
loop, or advance at completion. Marker ranges use the parent's normal scene
stream: explicit valid end, next strictly later marker, or scene end. The scene's
actual duration, stream list, and Apollo identity remain unchanged.

The timeline covers only the selected segment: **0:00** at its start and the
segment duration at its end. This applies to markers and every scene start/window
setting, including a random or resume start followed by playback to scene end.
Scrubbing and keyboard seeking stay inside that range; replay returns to its
start. Quality changes and returning from settings retain the selected segment
and elapsed position. Sprite previews, marker metadata and activity still use
the original scene timestamps internally.

One saved **default quality for scenes and markers** is applied before each
item loads. A fixed resolution selects an advertised normal HLS transcode at
that resolution or below. If none is compatible, TV offers quality selection
or skipping; it does not silently load a higher quality or a direct/remux stream.
An explicit source selection lasts for the current item and preserves scene
time and playback state. It does not change the shared player's saved quality.
Transcodes are generated on demand by Stash; no generated video preview mode
is involved. Posters and timeline sprite images remain available.

**Prepared videos** defaults to **5**: the current item, two before it and two
after it. Choose **3** for one on each side, or **1** to load only the current
item. The window has fewer items at the start and end of the feed. Preparation
starts after the active video is ready and uses the same default quality and
playback range. Direct files preload media; HLS streams prepare their first
segments and retain an independent server session, including adjacent markers
from the same scene. Existing server lookahead limits still apply: encoders
suspend when their buffer is full instead of transcoding the whole scene.
Items leaving the window release their session. Leaving TV releases all of them;
background items never play audio or record activity. Preparing more items can
use more GPU, memory and bandwidth.

**Start muted** in Settings → TV controls the initial audio when entering TV
(enabled by default). The visible mute/unmute control in the bottom dock changes
audio for the current viewing session and carries across item changes. Browsers
may still require muted autoplay. Temporary holds never replace the saved
playback rate.
Playback menus include captions and supported device playback controls.
Fit/fill, zoom, rotation, left-handed actions, and hiding controls are available.
Navigation, mute and restore-controls stay reachable in the bottom
dock with chrome hidden. Pinned actions use that dock; the remaining action
rail is bounded above it, on the preferred hand's side.
Controls are transparent icons with small shadows and comfortable tap targets.
The bottom dock has no background panel, including when the UI is hidden;
its gaps, side gutters and bottom safe-area padding stay outside the video
gesture surface. Tapping those areas cannot pause playback, and video gestures
remain available above the dock.
Use the navigation drawer to switch to another top-level page; TV has no close
button. The drawer also remains available while loading or when media is
unavailable. Escape closes an open menu or exits fullscreen while
remaining on the TV page.

Fullscreen targets the entire TV container and is offered only when the browser
supports it. When unavailable or rejected, the action is hidden and TV retains
its ordinary inline layout. There is no simulated immersive fallback.
TV never requests native video fullscreen, including on iOS Safari. Browser
bars may remain visible. Dialogs and menus use a portal inside the presentation
surface, and rotated slider and zoom gestures map to its coordinates. Dialogs
keep Close reachable below a bounded scroller; long titles, descriptions and
tag/performer labels wrap without horizontal overflow.

## Actions and settings

Add the optional **Feed** action in **Settings → TV → Action rail** to switch
between **Scenes** and **Markers** with one tap while watching. Its icon shows
the current feed, and its label names the destination. Switching uses a brief
fade that respects reduced motion. It can be pinned or placed in a folder.
The choice applies to the current viewing URL; the saved
default changes only in settings. Returning to a recent feed restores its
selection and playback position. Switching clears source-specific URL filters
and item links and uses the destination's configured filters. A sort unavailable
in the destination falls back to its saved-filter order for that session.
An empty or unavailable feed also exposes the control, so it is possible to
switch away without leaving TV.

Quality, speed, volume and subtitles are available in the default Playback
folder. The optional **TV settings** action links directly to **Settings → TV**,
preserving the current selection and position for return. Add it anywhere in the
rail, pin it, move it into a folder, or remove it. The default rail has no settings
button; the settings page is always accessible through app navigation.

Metadata links open the app's existing detail routes. Returning to TV restores
the recent feed, selection, and scalar playback state while cached. Rating,
organized state, and O-counter operations target the parent scene in both modes.
**Information** toggles transparent metadata beside the lower action rail.
Its bounded scroller wraps long titles and tags; playback, swiping and automatic
advance continue outside it. Hiding the controls hides the metadata too.
The O-counter has a numeric badge and an anchored, nonmodal popover for Add O,
Decrement O and Reset O. Badges show four digits in full and cap larger values
at `9999+`; the popover shows the full, localized count. Custom pinned and folder
placements retain the same behavior.
The rating dialog uses the shared rating component and the server's configured
star or decimal format. Fractional stars expose a slider at the configured
precision; decimal ratings use 0.1 increments out of ten. Slider gestures preview
locally and save on release, and Clear rating removes the rating.
Tags target the scene or marker as appropriate, including marker primary tags.
Create markers through the shared editor or repeatable quick-create presets.
Deletion uses the shared confirmation and scene file/generated-file choices.

The action editor appears directly in the **Action rail** settings section. Its
code loads automatically when that page opens. It supports keyboard or pointer
reorder, pinned entries,
one-level folders, moving actions into/out of folders, custom labels, curated
icons, quick tags, and quick markers. Visibility remains at the top level.
Presets require real tag IDs. Only the versioned `tv` configuration
key is saved; local rotation is scoped to the backend URL in browser storage.
The shared save indicator reports progress. Failed saves retain the draft and
offer Retry; incomplete presets remain editable until valid. Invalid/future settings remain
recoverable until an explicit reset. The main settings search includes TV rows.

Settings version 5 removes the old page-size, loading-threshold and session-limit
preferences while retaining other settings. Version 4 removed the formerly
required, unchanged default settings button from older rails; customized shortcuts
and those deliberately added since version 4 remain intact. TV uses
the chosen saved/default filter without additional feed rules. Earlier settings
retain their selected filters and playback preferences;
retired rules are removed. Version 1 settings with Shuffle enabled load as Random;
other saved sort choices remain intact. Saving writes the new format without
rules or a shuffle flag. Sort labels use the same translations as the library menus.
Saved versions without a startup mute preference default to muted without
resetting their other preferences. Versions without a prepared-video preference
default to five without resetting any other settings.

## Activity and ownership

Scene detail, online scene lightbox, and TV share one automatic activity owner
under `ScenePlayer`. A visit accumulates observed media progress, excluding
seeks, buffering, source changes, paused time, and long suspended intervals.
The global tracking and minimum-play settings control writes and play eligibility.
Checkpoint, pause, selection change, visibility, and unmount flush actual watched
duration and absolute resume time. Completion accounting does not rely on a
route's `onEnded`, and eligible plays are counted at most once per selection visit.
Manual Add Play remains a separate explicit operation.

The per-Apollo-client writer serializes activity across surfaces for the same
scene. Server increments are not idempotent: an uncertain response quarantines
that delta rather than retrying it and risking duplicate statistics. Closing
the browser can interrupt a final request. Marker clips and offline playback
never write server activity; offline playback keeps its existing local resume.

## Implementation and verification map

| Plan features | Implementation |
| --- | --- |
| TV-01, TV-21 | Route/nav integration, `tv-settings.tsx`, `use-tv-settings.ts` |
| TV-02–04, TV-25 | `core/tv/feed-query.ts`, `feed-state.ts`, `tv-session-state.ts` |
| TV-05–07, TV-09–11, TV-27 | Shared player controls/sources, `playback-policy.ts`, `player-quality.ts`, `marker-range.ts`, `tv-timeline.tsx`, `use-tv-inputs.ts` |
| TV-12–13, TV-23 | `use-tv-presentation.ts`, `tv-slider.tsx`, scoped overlay portals and zoom coordinates |
| TV-15–20, TV-24 | TV controls, playback menus, edit panel, mutation adapter, rail editor, and guide |
| TV-22 | `core/scene-activity.ts`, `scene-activity-effects.tsx`, explicit scopes in all three online surfaces |

Apollo owns entity data. The feed stores IDs, paging state, tombstones, and a
small playback snapshot; its constructor is pure and effects own requests.
There is one active page request, bounded refill bursts, one stable player/video,
and three presentation slots. Adjacent slots use still images. The configured
media window owns cancellable metadata/media preparation and transcode leases;
it does not mount additional shared players. `use-tv-media-window.ts` retains
entries across scrolling and `tv-prepared-item.ts` prepares each one. The shared
`player-transcode-session.ts` scopes keepalive, quality changes and cleanup to
each owner. Released server sessions reject late requests so an aborted preload
cannot restart an evicted encoder. Timelines subscribe to scalar player state, and playback ticks
do not publish feed state. Settings editors and action dialogs mount on demand.
The existing content reveal, media attachment, deferred freeze-frame canvas,
route handoff, and offline download behavior are retained.

`core/tv/*.test.ts` covers filter composition, validation, seeded playback,
quality limits, ranges, paging, stale responses, and tombstones.
`core/scene-activity.test.ts` covers accounting, serialized writes, uncertain
responses, minimum-play qualification, and disabled tracking.
`tests/browser/tv.browser.ts` exercises real synthetic media, bounded rendering,
inline presentation, rotation, quality continuity, hold controls, autoplay,
settings persistence/failure, and activity. The lightbox browser fixture also
verifies shared online activity. Existing app/player/browser regressions remain
part of the gate. `TestMarkerQueryASTOrientation` verifies the additive marker
AST orientation condition against parent video dimensions and nested criteria.
`tv-preload.browser.ts` checks the 1/3/5 window for scenes and same-scene markers,
direct file preparation, rapid-scroll cancellation, exit cleanup and autosaving
the setting. Unit tests cover window ownership and bounded startup fetching;
Go tests cover session isolation, cleanup and late-request rejection.

Automated WebKit coverage is not a physical iPhone Safari test. Device-specific
casting, AirPlay, picture-in-picture, and iOS browser chrome still depend on the
device and browser capabilities. CRT effects, gamepads, generated video previews,
and executable JavaScript feed rules are intentionally excluded.
