# TV mode

TV is a native v3 navigation item at `/tv`. Configure it in **Settings → TV**
at `/settings/tv`, then select **Save TV settings**. The implementation follows
the [accepted plan](tv-mode-plan.md), including its recent performance constraints.
It needs no Stash TV plugin or separate application.

## Playback and controls

Choose a scene or marker feed, an app default, saved filter, or all items.
Saved search, criteria groups and sort retain the library's filter semantics.
Additional saved-filter rules combine criteria with AND; text search belongs
in the primary filter. Shuffle keeps a seed across pages. Matching orientation
uses the viewing surface, including forced rotation, and includes square videos.

Swipe vertically, use a wheel gesture, press Up/Down, or use the visible
previous/next buttons. Seek buttons jump between markers and otherwise move ten
seconds. Hold forward for faster playback or reverse for backward scrubbing;
Up/Down changes the held speed. Release, blur, and visibility changes restore
the previous rate and pause state. Menus, form inputs, and zoom take ownership
of their input. The guide inside TV lists the remaining shortcuts.

Scenes can start at resume, beginning, a sampled marker, or a sampled position.
Playback can use the full scene or a fixed/random duration window, with stop,
loop, or advance at completion. Marker ranges use the parent's normal scene
stream: explicit valid end, next strictly later marker, or scene end. The scene's
actual duration, stream list, and Apollo identity remain unchanged.

One saved **default quality for scenes and markers** is applied before each
item loads. A fixed resolution selects an advertised normal HLS transcode at
that resolution or below. If none is compatible, TV offers quality selection
or skipping; it does not silently load a higher quality or a direct/remux stream.
An explicit source selection lasts for the current item and preserves scene
time and playback state. It does not change the shared player's saved quality.
Transcodes are generated on demand by Stash; no generated video preview mode
is involved. Posters and timeline sprite images remain available.

TV starts muted and retains user audio/rate choices across item changes.
Playback menus include captions and supported device playback controls.
Fit/fill, zoom, rotation, left-handed actions, and hiding controls are available.
Settings, restore-controls, navigation, and exit stay reachable with chrome hidden.

Fullscreen targets the entire TV container. When unavailable or rejected,
immersive presentation fills the viewport with inline video and the TV UI.
TV never requests native video fullscreen, including on iOS Safari. Browser
bars may remain visible. Dialogs and menus use a portal inside the presentation
surface, and rotated slider and zoom gestures map to its coordinates.

## Actions and settings

Metadata links open the app's existing detail routes. Returning to TV restores
the recent feed, selection, and scalar playback state while cached. Rating,
organized state, and O-counter operations target the parent scene in both modes.
Tags target the scene or marker as appropriate, including marker primary tags.
Create markers through the shared editor or repeatable quick-create presets.
Deletion uses the shared confirmation and scene file/generated-file choices.

The action editor supports keyboard or pointer reorder, pinned entries,
one-level folders, moving actions into/out of folders, custom labels, curated
icons, quick tags, and quick markers. Settings and visibility remain at the
top level. Presets require real tag IDs. Only the versioned `tv` configuration
key is saved; local rotation is scoped to the backend URL in browser storage.
Failed saves retain the draft for retry, and invalid/future settings remain
recoverable until an explicit reset. The main settings search includes TV rows.

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
| TV-02–04, TV-25–26 | `core/tv/feed-query.ts`, `feed-state.ts`, `tv-session-state.ts` |
| TV-05–07, TV-09–11, TV-27 | Shared player controls/sources, `playback-policy.ts`, `player-quality.ts`, `marker-range.ts`, `tv-timeline.tsx`, `use-tv-inputs.ts` |
| TV-12–13, TV-23 | `use-tv-presentation.ts`, `tv-slider.tsx`, scoped overlay portals and zoom coordinates |
| TV-15–20, TV-24 | TV controls, playback menus, edit panel, mutation adapter, rail editor, and guide |
| TV-22 | `core/scene-activity.ts`, `scene-activity-effects.tsx`, explicit scopes in all three online surfaces |

Apollo owns entity data. The feed stores IDs, paging state, tombstones, and a
small playback snapshot; its constructor is pure and effects own requests.
There is one active page request, bounded refill bursts, one upcoming detail
prefetch, one stable player/video, and three presentation slots. Adjacent slots
use still images. Timelines subscribe to scalar player state, and playback ticks
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

Automated WebKit coverage is not a physical iPhone Safari test. Device-specific
casting, AirPlay, picture-in-picture, and iOS browser chrome still depend on the
device and browser capabilities. CRT effects, gamepads, generated video previews,
and executable JavaScript feed rules are intentionally excluded.
