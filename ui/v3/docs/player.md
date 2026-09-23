# Player architecture

The shared player serves scene detail, scene lightboxes, TV, offline files, and
scoped guest playback. This guide owns its media lifetime and transition
contracts. Start with the [system overview](../../../docs/ARCHITECTURE.md#playback-and-downloads)
for transport boundaries and the [frontend architecture](architecture.md) for
state ownership. Paths in the frontend module table are relative to
[`src/components/player/`](../src/components/player/).

## Module boundaries

[scene-player.tsx](../src/components/player/scene-player.tsx) owns the stable player shell.
[use-scene-player-sources.tsx](../src/components/player/use-scene-player-sources.tsx)
coordinates selected sources and pending resumes.

TV uses this same shell through the semantic commands and scalar subscriptions
in `scene-player-controls.tsx`. Automatic activity for scene detail, online
scene lightbox, and TV is owned by `scene-activity-effects.tsx` and the
per-client coordinator in `core/scene-activity.ts`. Do not add route-owned
automatic activity mutations. Explicit marker, offline, and disabled scopes
exclude server accounting. See [TV mode](tv-mode.md) for feed ownership,
quality policy, inline presentation, and the full feature map.
The Video.js packages are pinned together in [package.json](../package.json).
[pnpm-workspace.yaml](../pnpm-workspace.yaml) contains a version-scoped hls.js
override; reassess it with the next adapter upgrade instead of downgrading the
engine implicitly.

| Module | Responsibility |
| --- | --- |
| [scene-video.tsx](../src/components/player/scene-video.tsx) | One native video element; typed direct/HLS source configuration |
| [scene-player-sources.ts](../src/components/player/scene-player-sources.ts) | Source eligibility, quality preferences, initial resume |
| [scene-player-transitions.ts](../src/components/player/scene-player-transitions.ts) | Pure seek/restart decisions and resume plans |
| [scene-player-source-url.ts](../src/components/player/scene-player-source-url.ts) | Stream URLs, clip bounds, fragments, reload nonce |
| [hls.ts](../src/components/player/hls.ts) | HLS timeline policy and engine helpers |
| [buffered-seek-preview.ts](../src/components/player/buffered-seek-preview.ts) | Coalesced, buffered frame previews during a scrub drag |
| [use-player-transition-feedback.tsx](../src/components/player/use-player-transition-feedback.tsx) | Freeze frame, loading feedback, seek readiness |
| [player-transcode-session.ts](../src/components/player/player-transcode-session.ts), [use-player-transcode-session.ts](../src/components/player/use-player-transcode-session.ts) | Own scoped HLS leases across playback and pause; TV retains nearby leases until window eviction or exit |
| [prepare-player-source.ts](../src/components/player/prepare-player-source.ts) | Cancellable, bounded startup fetching for TV's nearby direct/HLS sources |
| [use-player-recovery.ts](../src/components/player/use-player-recovery.ts) | Native fullscreen seeking and stalled-playback recovery |
| [use-player-load-timeout.ts](../src/components/player/use-player-load-timeout.ts) | Deadline for a source that never becomes ready, including failed recovery loads |
| [use-player-loop.ts](../src/components/player/use-player-loop.ts) | Media-clock loop deadline, cancelled by pause, seek and source changes |

## Media lifetime and selection

The scene lightbox uses [scene-carousel.tsx](../src/components/lightbox/scene-carousel.tsx),
a YARL carousel module with three keyed posters and one persistent player above
the track. YARL's controller still
owns pointer/wheel navigation, drag offsets and swipe animations. The decoded
incoming poster stays mounted while the outgoing player keeps its visual position
and source until the actual animation finishes. Only then does the player move
to the center and load the selected scene; interrupted swipes cannot load obsolete
selections. The same player/store/video survives scenes, markers and loading
sentinels; closing the lightbox disposes that session. No media DOM is moved
between slides. Only the initial entrance uses a fixed autoplay delay, and a late
EOF from an outgoing scene cannot advance the new selection.

`playbackKey` identifies the selected scene/marker independently of media ownership.
Selection changes reset source preferences, clip offsets, poster/started latches,
zoom and recovery state before the new playback becomes active. Quality changes
within that selection retain their existing playhead/resume behavior. Pending
queries, missing scenes and OPFS reads suspend the retained player, clear its
source and release the outgoing transcode. Late query results are checked against
the selected scene. Offline resume writes flush before the shared playhead changes.

Audio and playback rate belong to the persistent media element. A held 2× gesture
keeps its touch target through automatic advance and restores the prior rate on
release. Animation deadlines and asynchronous seeks belong to their playback/load;
obsolete work cannot resume or mute a later scene. Deferred freeze-frame JPEG
exports are also cancelled when cleared or superseded. Loop mode restarts on the
existing media just before the boundary (at most 5 ms or a quarter frame early),
reducing the interruption from native EOF and the application's restart path.
The deadline rechecks media time and respects pause, seeking, buffering, rate
changes and visibility. Native EOF remains the fallback when timers run late.
Buffered loops do not capture a frame or enter user-seek/loading feedback; a tiny
HLS timestamp gap at zero resolves to the first buffered sample, while an evicted
opening segment still uses source recovery. One clip-range effect owns marker
completion and EOF fallback, while auto-advance fires once. The explicit WebKit `canplay`
resume remains necessary. Chromium/WebKit fixtures exercise the actual lightbox
and its source machinery, but physical iPhone autoplay permission and MMS still
need device testing.

## Recovery and seeking

Recovery observes presented video frames separately from the audio clock.
Source changes, recovery and Retry retain the native video element along with
the player root and controls. A source reload has a 30-second readiness deadline,
reset after returning from the background. It gets one source retry, then stops
HLS loading and presents Retry. The deadline also covers a resume seek whose
completion event never arrives. Recovery is a fallback; it does not establish
that the underlying repeated-seek failure is resolved on physical iOS.

Audible Direct loops are not guaranteed to be seamless on Safari. A
[plain-video diagnostic on macOS 26](https://github.com/notsafeforgit/stash/actions/runs/35467017314)
measured roughly 380–400 ms between the first and next advancing frame with
audio, versus 50–70 ms muted; physical iPhone feedback also reported smoother
muted loops. Native `loop`, early seeking, `fastSeek`, a small positive seek
target and temporary muting did not reliably remove the audible delay.
[Routing through Web Audio](https://github.com/notsafeforgit/stash/actions/runs/35467312555)
also retained it. Linux browser checks do not establish this Apple media behavior.

Freeze-frame canvases start with a minimal backing buffer. Texture allocation
and synchronous GPU readback wait for playable video data, a paint and idle time
(a timer after paint where idle callbacks are unavailable). An early capture
prepares the buffer on demand. Warm-up is cancelled on departure, and an already
prepared buffer is reused without clearing a captured frame. Page mounting and
initial scrolling do not depend on this optional warm-up.

Transition plans consume plain buffered/seekable state and return an in-place
seek, engine restart, or source reload. Browser effects apply the plan; keep DOM
operations out of the planner so it remains testable without a media element.

`PositionScrubber` separates its draft position from committed seeks. A drag
temporarily pauses playback and previews decoded frames only inside the native
buffered ranges, rechecking for eviction before each write. Preview seeks are
coalesced and serialized. Classic MSE hls.js fragment loading is suspended until
the drag ends; Safari MMS and native HLS retain their browser-owned loading
policy. Release uses the normal seek policy and restores the previous playback
intent; cancellation restores the original position too. A simple tap does not
pause. The independent draft position also supports generated sprite previews
without seeking into unbuffered media.
Native seek recovery ignores an active preview, including recovery queued before
the drag started, so buffer changes during a hold cannot commit or reload it.
Releasing at a preview target still being decoded reuses the pending native seek;
it does not issue a second seek to the same time.

Touch scrubbing supports precision seeking in scene detail, both lightboxes and
TV (including rotated controls). A 650 ms dwell within a 4 px radius zooms the
timeline around the time under the finger. Further zooms require a 1200 ms pause;
the movement allowance tightens to 2 px at 4× and 1 px from 16× onward, so fine
adjustments restart the dwell. Each new window anchors its dwell at the current
finger position. Each dwell narrows the visible range
fourfold, capped at 60 seconds on entry and one second at maximum precision.
Markers and buffered ranges follow the same window; a fine-seeking readout shows
milliseconds above the thumb. The standard playback clock and TV clock follow
the same draft position during a drag, including while a frame is decoding or
the target is not buffered. Release commits once and restores the full timeline;
cancellation or a scene/marker change disposes the gesture. Mouse and keyboard input retain
the full scale. Zoom steps request a short vibration where supported; Safari on
iOS has no standard vibration API, so its feedback is visual.

Explicit play and pause commands share user intent across scene detail,
lightboxes, TV and OS media controls. Ordinary pause/resume delegates to native
playback without adjusting media time or observing frames to correct the browser's
clock. Play remains in the input gesture and handles cancellation by a newer
pause or seek. Marker replay and temporary scrub pauses retain their own seek
behavior. Browser tests verify pause/resume does not introduce seeks or reloads.

## Playback invariants

- Scene time is absolute. A clip's media time is relative to its segment-aligned
  origin; convert only at the media seek boundary.
- The player root and native video element survive source changes, including
  direct/HLS engine switches. `SceneVideo` configures Video.js’s packaged
  `HlsJsVideo` through a typed source, with an explicit MIME type for direct files.
  Retain playhead, paused state and playback rate through the pending-resume path,
  including WebKit’s explicit resume after `canplay`.
- Muted autoplay fallback is only for `NotAllowedError` on a current request.
  Source-change aborts and obsolete requests must not change the audio state.
- Buffered seeks stay in place. Distant desktop HLS seeks can flush the engine;
  iOS ManagedMediaSource and clipped playlists use source reloads.
- Every forced reload changes the URL, even a repeated target at zero.
- Clip URL bounds stay fixed through quality changes so earlier portions remain
  reachable. Retain freeze-frame masking and native fullscreen behavior.
- Temporary press-and-hold speed belongs to the current player. Release restores
  its previous rate while the media is attached. Scene/marker navigation in the
  lightbox retains the gesture with the player. If the owning viewer unmounts
  mid-hold, cleanup must clear the gesture without sending playback commands to
  a detached store.
- Touch scene lightboxes pass their history-aware dismissal callback into the
  player. Close occupies the right end of the playback row and remains subtly
  visible and tappable when playback controls fade. It has one stable 44px target;
  the fading controls and gradient are separate, so Close has no invisible or
  inert ancestor. Close captures pointer and focus activity before Video.js's
  native container listeners can reveal controls and consume Safari's first tap;
  its normal click remains the sole dismissal handler for touch and keyboard.
  Hidden playback controls, including the central play and skip buttons, remain
  inert and let taps through to the gesture surface. A single tap reveals controls
  without changing playback or audio; a rapid double tap zooms without revealing
  them. Visible buttons accept every tap immediately, while double taps on the
  surrounding video or control-bar gaps still zoom.
  Time and available PiP/Cast controls sit above a full-width timeline, preserving
  scrubbing space and direct speed, quality, playback-mode, fullscreen, and Close
  access with 44px targets.
  Mobile slides fill the viewport; control padding respects the home indicator
  and landscape display cutouts.
  Pending/error slides provide bottom dismissal until a player is available.
  Desktop retains the lightbox toolbar and Escape behavior. The timeline has a
  centered 32px hit area, allowing clicks above and below the visible track.
  Visible desktop control-bar gaps absorb stray clicks without toggling playback.

## Platform integrations

`PlatformMediaEffects` binds the active Video.js store to Media Session and
screen wake locks. OS seeks use the same offset/clip-aware seek callback as the
player timeline. The most recently started player owns metadata and actions;
paused previews and stale cleanup cannot replace a newer owner. Session state
clears on suspension/unmount. Wake locks cover visible, local playback only,
release on pause/end/PiP/casting/hidden state, and handle late requests and browser
refusal without interrupting playback. Visibility return can reacquire a lock.
AirPlay uses Video.js 10's `AirPlayButton` and built-in HLS AirPlay bridge; PiP
uses its existing feature store. Unavailable controls stay hidden, and local
blob media disables remote playback. Receivers must be able to fetch the source.

## Backend streaming boundary

[SceneStreamsV3](../../../internal/api/resolver_model_scene_v3.go) supplies the
v3 source catalogue. The legacy `sceneStreams` field has its own
[compatibility adapter](../../../internal/manager/scene_stream_legacy_compat.go).
[Scene routes](../../../internal/api/routes_scene.go) gate the v3 HLS endpoint
families on `enable-v3-ui`; GraphQL schema availability alone does not mean those
HTTP endpoints are enabled.

[pkg/ffmpeg/stream_v3_segmented.go](../../../pkg/ffmpeg/stream_v3_segmented.go)
owns the v3 stream variants and segment serving;
[stream_segmented.go](../../../pkg/ffmpeg/stream_segmented.go) constructs the
FFmpeg pipeline. Full-scene playlists expose scene time, while clipped playlists
are relative to the segment-aligned clip origin. Media-sequence numbers identify
segments; they are not a browser playback-time offset.

[stream_v3_session.go](../../../pkg/ffmpeg/stream_v3_session.go) isolates encoder
sessions between players, markers, and tabs. Released sessions reject late
segment requests so an aborted fetch cannot resurrect an evicted encoder.
Client transcode leases are separate from the reload nonce used to force a
source URL change.

Segments from different FFmpeg runs can share a playback session's cache.
Per-track PTS normalization, nonnegative encoder timestamps, frame-count
keyframes, and the child MP4 muxer's scene-time offset must remain compatible
across restarts. [The backend HLS constraints](../../../CLAUDE.md#frontend-uiv3)
record the encoder details and previous iOS regression.
[stream_v3_timestamps_test.go](../../../pkg/ffmpeg/stream_v3_timestamps_test.go)
checks fragment decode timestamps and decoded frames across restarted runs.
Run the relevant media tests when changing this pipeline, and verify physical
iOS playback separately from Linux Chromium/WebKit fixtures.
