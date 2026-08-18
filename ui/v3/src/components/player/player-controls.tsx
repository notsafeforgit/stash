/**
 * Controls overlay for `<ScenePlayer>`. Rendered inside `Container`
 * so the Video.js v10 hooks (`Player.usePlayer`) can read state and call
 * actions. Handles scene-time vs media-time math, hotkeys, touch overlay,
 * source/speed menus, markers, and the frozen-frame + reloading overlay
 * interplay.
 *
 * The `Player` factory is threaded in as a prop so this file doesn't create
 * its own store type; the parent `ScenePlayer` owns the single
 * `createPlayer(...)` call and passes its `CreatePlayerResult` down.
 */
import type React from "react";
import { useEffect, useCallback, useState, useRef } from "react";
import {
  CastButton,
  Controls,
  VolumeSlider,
  type CreatePlayerResult,
  type VideoPlayerStore,
} from "@videojs/react";
import {
  Play,
  Pause,
  Volume2,
  VolumeX,
  Maximize,
  Minimize,
  PictureInPicture,
  PictureInPicture2,
  RotateCcw,
  RotateCw,
  Cast,
  Repeat,
  SkipForward,
} from "lucide-react";
import { cn } from "src/lib/utils";
import { Button } from "src/components/ui/button";
import { Spinner } from "src/components/ui/spinner";
import type { PlayerSource } from "./player-utils";
import { PlayerMarkers } from "./player-markers";
import type { IMarker } from "./player-utils";
import { SpeedMenu, QualityMenu } from "./player-menus";
import { DOUBLE_TAP_MAX_MS } from "./video-frame-zoom";
import {
  exceedsTouchTapMovement,
  isCompletedTouchTap,
  shouldCancelTouchHoldOnMove,
  type TouchTapCandidate,
} from "./touch-tap";

type PlayerInstance = CreatePlayerResult<VideoPlayerStore>;

// Common transparent-on-video button style. Static-color callers compose
// `cn(OVERLAY_BTN, "text-white/80")`; dynamic-color callers (loop /
// auto-advance / cast) supply their own active-state class.
const OVERLAY_BTN =
  "shrink-0 bg-transparent hover:bg-transparent hover:text-white";

// ── Playback-mode button ──────────────────────────────────────────────────────
// Single tri-state cycle: normal → advance → loop → normal. Replaces the two
// separate Loop / Auto-advance toggles since the states are mutually
// exclusive in practice (the marker-range gate uses one or the other, never
// both). When the parent didn't wire `onNext` (standalone scene page) the
// `advance` step is skipped and the cycle collapses to normal ↔ loop.
//
// In normal (inactive) mode the button shows both candidate icons separated
// by a slash so the cycle's two destinations are visible at a glance. In an
// active mode it shows only the corresponding icon, in blue, so the current
// state reads cleanly without the cycle hint clutter.

function PlaybackModeButton({
  mode,
  canAdvance,
  onCycle,
}: {
  mode: "normal" | "loop" | "advance";
  canAdvance: boolean;
  onCycle: () => void;
}) {
  const label =
    mode === "advance"
      ? "Playback: auto-advance"
      : mode === "loop"
        ? "Playback: loop"
        : canAdvance
          ? "Playback: normal — click to cycle auto-advance / loop"
          : "Playback: normal — click to enable loop";
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      onClick={onCycle}
      aria-label={label}
      title={label}
      className={cn(
        OVERLAY_BTN,
        mode === "normal" ? "text-white/80" : "text-blue-400",
        // In normal mode + canAdvance, the content (icon + slash + icon)
        // is wider than `size-8`; let the button auto-expand so the
        // press/focus indicator wraps the visible glyphs.
        mode === "normal" && canAdvance && "w-auto min-w-8 px-1.5",
      )}
    >
      {mode === "advance" ? (
        <SkipForward size={15} />
      ) : mode === "loop" ? (
        <Repeat size={15} />
      ) : (
        <span className="inline-flex items-center gap-0.5 text-[0.7rem] leading-none">
          {canAdvance && <SkipForward size={12} />}
          {canAdvance && <span aria-hidden>/</span>}
          <Repeat size={12} />
        </span>
      )}
    </Button>
  );
}

// ── Duration formatting ────────────────────────────────────────────────────────

function formatDuration(secs: number): string {
  const t = !Number.isFinite(secs) || secs < 0 ? 0 : secs;
  const s = Math.floor(t);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (h > 0)
    return `${h}:${String(m).padStart(2, "0")}:${String(sec).padStart(2, "0")}`;
  return `${m}:${String(sec).padStart(2, "0")}`;
}

// ── Position slider ───────────────────────────────────────────────────────────
// Custom slider operating in scene time against `fileDuration`. v10's built-in
// `<TimeSlider>` binds to the media element's duration/currentTime and would
// break on live-transcoded fragmented-MP4 sources whose `media.duration` is
// Infinity.

interface PositionSliderProps {
  Player: PlayerInstance;
  offsetStart: number;
  fileDuration: number;
  markers: IMarker[];
  onSeek: (trueTime: number) => void;
  reloading: boolean;
  seekDisplayTarget: number | null;
  /**
   * When set, renders two draggable handles on the slider for editing a
   * marker's start / end. Drags emit `onChange` with the new value(s) in
   * scene time and never call `onSeek` — the underlying playhead is
   * untouched. A `null` boundary value hides that handle (e.g. while the
   * user hasn't picked an end yet for a single-point marker). Used by the
   * scene detail page's marker editor.
   */
  clipBoundsEdit?: ClipBoundsEdit;
}

export interface ClipBoundsEdit {
  start: number | null;
  end: number | null;
  onChange: (next: { start?: number | null; end?: number | null }) => void;
}

function PositionSlider({
  Player,
  offsetStart,
  fileDuration,
  markers,
  onSeek,
  reloading,
  seekDisplayTarget,
  clipBoundsEdit,
}: PositionSliderProps) {
  const mediaCurrentTime = Player.usePlayer((s) => s.currentTime);
  const mediaBuffered = Player.usePlayer((s) => s.buffered);
  const controlsVisible = Player.usePlayer((s) => s.controlsVisible);

  // `Math.max(0, …)` covers the brief reload window in clip-display mode:
  // for HLS / direct sources `offsetStart` is shifted negative by the
  // clip start (so `offsetStart + mediaCurrentTime` lands at clip-time 0
  // once Safari's currentTime reaches the clip start), but during reload
  // mediaCurrentTime is stale at 0 and the raw expression underflows.
  // The slider's `progress` is already clamped to [0, 1] downstream;
  // clamping here keeps any time-ish displays consistent.
  const trueTime = Math.max(
    0,
    seekDisplayTarget != null
      ? seekDisplayTarget
      : reloading
        ? offsetStart
        : offsetStart + mediaCurrentTime,
  );
  const bufferedEnd = Math.max(
    0,
    !reloading && mediaBuffered && mediaBuffered.length > 0
      ? offsetStart + mediaBuffered[mediaBuffered.length - 1][1]
      : offsetStart,
  );

  const trackRef = useRef<HTMLDivElement>(null);
  const [dragTime, setDragTime] = useState<number | null>(null);
  const draggingRef = useRef(false);

  const displayTime = dragTime ?? trueTime;
  const progress =
    fileDuration > 0 ? Math.max(0, Math.min(1, displayTime / fileDuration)) : 0;
  const bufferedProgress =
    fileDuration > 0 ? Math.max(0, Math.min(1, bufferedEnd / fileDuration)) : 0;

  function timeFromPointer(clientX: number): number {
    const track = trackRef.current;
    if (!track || fileDuration <= 0) return 0;
    const rect = track.getBoundingClientRect();
    const ratio = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width));
    return ratio * fileDuration;
  }

  // `stopPropagation` on every pointer event keeps a seek-drag from also
  // being interpreted as a horizontal swipe by an enclosing carousel
  // (e.g. YARL's pointer-swipe controller in the lightbox scene player).
  // Without it, the slightest horizontal movement during a drag jumps to
  // the next slide.
  function handlePointerDown(e: React.PointerEvent<HTMLDivElement>) {
    if (fileDuration <= 0) return;
    // When controls are hidden, the first tap should reveal them rather
    // than initiate a seek-drag. Without this gate the slider takes a
    // single tap to both reveal and seek (it stays interactive through
    // the parent's `opacity-0` since opacity doesn't disable pointer
    // events), while the rest of the controls follow the standard
    // mobile two-tap pattern (first tap reveals, second tap interacts)
    // because the user can't precisely hit a button they can't see.
    // Bailing here without `stopPropagation` lets the native pointer
    // listener `@videojs/core`'s controls feature attaches on the
    // container fire its `setActive` path and bring the bar back.
    if (!controlsVisible) return;
    e.stopPropagation();
    e.currentTarget.setPointerCapture(e.pointerId);
    draggingRef.current = true;
    setDragTime(timeFromPointer(e.clientX));
  }

  function handlePointerMove(e: React.PointerEvent<HTMLDivElement>) {
    if (!draggingRef.current) return;
    e.stopPropagation();
    setDragTime(timeFromPointer(e.clientX));
  }

  function handlePointerUp(e: React.PointerEvent<HTMLDivElement>) {
    if (!draggingRef.current) return;
    e.stopPropagation();
    draggingRef.current = false;
    try {
      e.currentTarget.releasePointerCapture(e.pointerId);
    } catch {
      /* ignore — pointer may have already been released */
    }
    const t = timeFromPointer(e.clientX);
    setDragTime(null);
    onSeek(t);
  }

  function handlePointerCancel() {
    draggingRef.current = false;
    setDragTime(null);
  }

  return (
    // `items-end` pins the bar to the bottom of the container; the
    // height is the hit area, sized so the slack lives entirely *above*
    // the bar (the buttons row sits just below via `gap-1` on
    // `Controls.Group`). Fine pointers (mouse) get a tight `h-3`; coarse
    // pointers (touch) bump up to `h-5` for a comfortable tap target.
    <div
      ref={trackRef}
      onPointerDown={handlePointerDown}
      onPointerMove={handlePointerMove}
      onPointerUp={handlePointerUp}
      onPointerCancel={handlePointerCancel}
      className="group relative flex items-end w-full min-w-[4em] h-3 pointer-coarse:h-5 touch-none cursor-pointer"
    >
      <div className="relative h-1 w-full bg-white/25 rounded-sm group-hover:h-1.5 transition-all">
        <div
          className="absolute inset-y-0 left-0 bg-white/40 rounded-sm"
          style={{ width: `${bufferedProgress * 100}%` }}
        />
        <div
          className="absolute inset-y-0 left-0 bg-white rounded-sm"
          style={{ width: `${progress * 100}%` }}
        />
        {/* Markers float in their own band above the bar so they don't
            visually merge with the playback line. The 2px gap between the
            wrapper's bottom and the bar's top matches the gap between
            successive marker layers (LAYER_HEIGHT_PX - marker height in
            player-markers.tsx). pointer-events-none on the wrapper lets
            the user scrub through gaps between markers; individual
            markers re-enable pointer events for tooltip hover. */}
        <div className="absolute left-0 right-0 bottom-[calc(100%+2px)] pointer-events-none">
          <PlayerMarkers markers={markers} duration={fileDuration} />
        </div>
        <div
          className="absolute top-1/2 -translate-y-1/2 -translate-x-1/2 w-3 h-3 bg-white rounded-full opacity-0 group-hover:opacity-100 transition-opacity"
          style={{ left: `${progress * 100}%` }}
        />
        {clipBoundsEdit && fileDuration > 0 && (
          <>
            {clipBoundsEdit.start != null && (
              <ClipBoundHandle
                trackRef={trackRef}
                boundary="start"
                time={clipBoundsEdit.start}
                fileDuration={fileDuration}
                onDrag={(t) => clipBoundsEdit.onChange({ start: t })}
              />
            )}
            {clipBoundsEdit.end != null && (
              <ClipBoundHandle
                trackRef={trackRef}
                boundary="end"
                time={clipBoundsEdit.end}
                fileDuration={fileDuration}
                onDrag={(t) => clipBoundsEdit.onChange({ end: t })}
              />
            )}
          </>
        )}
      </div>
    </div>
  );
}

// Marker-edit handle on the position slider. Dragged in scene time;
// `stopPropagation` keeps `PositionSlider`'s seek-drag from also firing.
// `setPointerCapture` is required so the pointer keeps tracking the handle
// even when the user drags past the slider's bounding box. Hit area
// (`w-5 h-7`) is intentionally larger than the visual flag so touch
// targets meet WCAG; the inner divs are positioned to render the visible
// flag inside that area.
function ClipBoundHandle({
  trackRef,
  boundary,
  time,
  fileDuration,
  onDrag,
}: {
  trackRef: React.RefObject<HTMLDivElement | null>;
  boundary: "start" | "end";
  time: number;
  fileDuration: number;
  onDrag: (t: number) => void;
}) {
  const draggingRef = useRef(false);
  const progress =
    fileDuration > 0 ? Math.max(0, Math.min(1, time / fileDuration)) : 0;

  function timeFromPointer(clientX: number): number {
    const track = trackRef.current;
    if (!track || fileDuration <= 0) return 0;
    const rect = track.getBoundingClientRect();
    const ratio = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width));
    return ratio * fileDuration;
  }

  function handlePointerDown(e: React.PointerEvent<HTMLDivElement>) {
    e.stopPropagation();
    e.preventDefault();
    e.currentTarget.setPointerCapture(e.pointerId);
    draggingRef.current = true;
  }
  function handlePointerMove(e: React.PointerEvent<HTMLDivElement>) {
    if (!draggingRef.current) return;
    e.stopPropagation();
    onDrag(timeFromPointer(e.clientX));
  }
  function handlePointerUp(e: React.PointerEvent<HTMLDivElement>) {
    if (!draggingRef.current) return;
    draggingRef.current = false;
    e.stopPropagation();
    try {
      e.currentTarget.releasePointerCapture(e.pointerId);
    } catch {
      /* released already */
    }
  }
  function handlePointerCancel() {
    draggingRef.current = false;
  }

  return (
    <div
      onPointerDown={handlePointerDown}
      onPointerMove={handlePointerMove}
      onPointerUp={handlePointerUp}
      onPointerCancel={handlePointerCancel}
      // `select-none` + `[-webkit-touch-callout:none]`: a long-press on the
      // handle on iOS Safari otherwise triggers text-selection / the
      // callout menu before our pointer-drag completes, which both hijacks
      // the gesture and surfaces a "Copy / Look Up" affordance over the
      // player. `touch-none` already disables panning/zooming gestures
      // here, but iOS treats text selection as a separate concern.
      className="absolute top-1/2 -translate-y-1/2 -translate-x-1/2 w-5 h-7 cursor-ew-resize touch-none select-none [-webkit-touch-callout:none] z-10"
      style={{ left: `${progress * 100}%` }}
      data-clip-bound={boundary}
      title={`${boundary === "start" ? "Start" : "End"}: ${formatDurationMs(time)}`}
    >
      <div className="absolute inset-y-0 left-1/2 -translate-x-1/2 w-[2px] bg-amber-400" />
      <div
        className={cn(
          "absolute top-1/2 -translate-y-1/2 h-3 w-2 rounded-sm bg-amber-400 ring-1 ring-black/40",
          boundary === "start"
            ? "left-1/2 -translate-x-full"
            : "left-1/2 translate-x-0",
        )}
      />
    </div>
  );
}

// Like `formatDuration` above but always includes thousandths — the marker
// editor needs millisecond precision in handle tooltips so the user can
// read out the exact captured time.
function formatDurationMs(secs: number): string {
  const t = !Number.isFinite(secs) || secs < 0 ? 0 : secs;
  const wholeMs = Math.round(t * 1000);
  const ms = wholeMs % 1000;
  const totalSec = Math.floor(wholeMs / 1000);
  const h = Math.floor(totalSec / 3600);
  const m = Math.floor((totalSec % 3600) / 60);
  const s = totalSec % 60;
  const base =
    h > 0
      ? `${h}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`
      : `${m}:${String(s).padStart(2, "0")}`;
  return `${base}.${String(ms).padStart(3, "0")}`;
}

// ── Relative-seek helpers ─────────────────────────────────────────────────────
// `useRelSeek` exists because we seek in *scene time* (via the parent's
// `onSeek`), not media time — and on live-transcode sources scene time is
// `offsetStart + media.currentTime`. Both the control-bar `RelSeekButton`
// and the touch-overlay buttons need this math; the hook centralises the
// `currentTime` subscription so callers just invoke `seekRel(±N)`.

function useRelSeek(
  Player: PlayerInstance,
  offsetStart: number,
  fileDuration: number,
  onSeek: (trueTime: number) => void,
): (seconds: number) => void {
  const mediaCurrentTime = Player.usePlayer((s) => s.currentTime);
  return (seconds: number) => {
    const trueTime = offsetStart + mediaCurrentTime;
    const max = fileDuration > 0 ? fileDuration : Infinity;
    onSeek(Math.max(0, Math.min(trueTime + seconds, max)));
  };
}

// ── Seek button ───────────────────────────────────────────────────────────────

interface RelSeekButtonProps {
  Player: PlayerInstance;
  seconds: number;
  children: React.ReactNode;
  className?: string;
  offsetStart: number;
  fileDuration: number;
  onSeek: (trueTime: number) => void;
}

function RelSeekButton({
  Player,
  seconds,
  children,
  className,
  offsetStart,
  fileDuration,
  onSeek,
}: RelSeekButtonProps) {
  const seekRel = useRelSeek(Player, offsetStart, fileDuration, onSeek);

  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      className={cn("bg-transparent hover:bg-transparent", className)}
      onClick={() => seekRel(seconds)}
    >
      {children}
    </Button>
  );
}

// ── Hotkeys ────────────────────────────────────────────────────────────────────

function useHotkeys({
  Player,
  offsetStart,
  fileDuration,
  frameRate,
  onSeek,
  onToggleFullscreen,
  onTogglePaused,
  seekDisplayTarget,
  disableSeekArrows,
}: {
  Player: PlayerInstance;
  offsetStart: number;
  fileDuration: number;
  frameRate: number;
  onSeek: (t: number) => void;
  onToggleFullscreen: () => void;
  /** Resolved play/pause toggle — already wraps the marker replay-from-stop
   *  case where applicable, so the spacebar shortcut shares the same path
   *  as the play button. */
  onTogglePaused: () => void;
  seekDisplayTarget: number | null;
  disableSeekArrows?: boolean;
}) {
  const store = Player.usePlayer();
  const mediaCurrentTime = Player.usePlayer((s) => s.currentTime);
  const volume = Player.usePlayer((s) => s.volume);

  const trueTime =
    seekDisplayTarget != null
      ? seekDisplayTarget
      : offsetStart + mediaCurrentTime;
  const max = fileDuration > 0 ? fileDuration : Infinity;
  const frameStep = 1 / (frameRate > 0 ? frameRate : 30);

  const handler = useCallback(
    (e: KeyboardEvent) => {
      const target = e.target as HTMLElement;
      if (
        target.tagName === "INPUT" ||
        target.tagName === "TEXTAREA" ||
        target.isContentEditable
      )
        return;

      const key = e.key.toLowerCase();
      const hasModifier = e.altKey || e.ctrlKey || e.metaKey || e.shiftKey;

      const seekBy = (step: number) =>
        onSeek(Math.max(0, Math.min(trueTime + step, max)));
      const adjustVolume = (delta: number) =>
        store.setVolume(Math.max(0, Math.min(1, volume + delta)));
      const frameStepAndPause = (direction: 1 | -1) => {
        store.pause();
        seekBy(direction * frameStep);
      };

      if (key === "arrowleft" || key === "arrowright") {
        if (disableSeekArrows) return;
        const sign = key === "arrowright" ? 1 : -1;
        const step = e.shiftKey ? 5 : e.ctrlKey || e.altKey ? 60 : 10;
        seekBy(sign * step);
        e.preventDefault();
        return;
      }

      if (hasModifier) return;

      let handled = true;
      switch (key) {
        case " ":
        case "enter":
        case "k":
          onTogglePaused();
          break;

        case "j":
          seekBy(-10);
          break;
        case "l":
          seekBy(10);
          break;

        case ",":
          frameStepAndPause(-1);
          break;
        case ".":
          frameStepAndPause(1);
          break;

        case "m":
          store.toggleMuted();
          break;
        case "f":
          onToggleFullscreen();
          break;
        case "arrowup":
          adjustVolume(+0.1);
          break;
        case "arrowdown":
          adjustVolume(-0.1);
          break;

        default:
          handled = false;
      }
      if (handled) e.preventDefault();
    },
    [
      store,
      trueTime,
      max,
      volume,
      onSeek,
      onToggleFullscreen,
      onTogglePaused,
      frameStep,
      disableSeekArrows,
    ],
  );

  useEffect(() => {
    // Capture phase, not bubble: a player button (e.g. the +10s seek
    // button in the control bar) that happens to hold keyboard focus
    // would otherwise have its keydown processed first, and Safari
    // queues that button's synthetic spacebar→click activation before
    // the bubble-phase handler here gets a chance to call
    // `preventDefault`. Capture flips the order — our handler runs as
    // the event travels down to the target, so the `preventDefault`
    // that the switch below issues for handled keys lands before the
    // browser commits to the activation, suppressing the spurious
    // button click cleanly without touching focus.
    window.addEventListener("keydown", handler, { capture: true });
    return () =>
      window.removeEventListener("keydown", handler, { capture: true });
  }, [handler]);
}

// ── Touch overlay ─────────────────────────────────────────────────────────────

const TOUCH_BTN_CLASS =
  "rounded-full bg-transparent text-white hover:bg-transparent hover:text-white";

function TouchOverlay({
  Player,
  offsetStart,
  fileDuration,
  onSeek,
  onTogglePaused,
}: {
  Player: PlayerInstance;
  offsetStart: number;
  fileDuration: number;
  onSeek: (t: number) => void;
  onTogglePaused: () => void;
}) {
  const paused = Player.usePlayer((s) => s.paused);
  const started = Player.usePlayer((s) => s.started);
  const seekRel = useRelSeek(Player, offsetStart, fileDuration, onSeek);

  if (!started) return null;

  return (
    <>
      <Button
        type="button"
        variant="ghost"
        size="icon-xl"
        onClick={() => seekRel(-10)}
        aria-label="Skip back 10 seconds"
        className={TOUCH_BTN_CLASS}
      >
        <RotateCcw />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon-xl"
        onClick={onTogglePaused}
        aria-label={paused ? "Play" : "Pause"}
        className={TOUCH_BTN_CLASS}
      >
        {paused ? <Play fill="white" /> : <Pause fill="white" />}
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon-xl"
        onClick={() => seekRel(10)}
        aria-label="Skip forward 10 seconds"
        className={TOUCH_BTN_CLASS}
      >
        <RotateCw />
      </Button>
    </>
  );
}

// ── Time display ──────────────────────────────────────────────────────────────

function TimeDisplay({
  Player,
  offsetStart,
  fileDuration,
  reloading,
  seekDisplayTarget,
}: {
  Player: PlayerInstance;
  offsetStart: number;
  fileDuration: number;
  reloading: boolean;
  seekDisplayTarget: number | null;
}) {
  const mediaCurrentTime = Player.usePlayer((s) => s.currentTime);
  // See PositionSlider for the `Math.max(0, …)` rationale — the same
  // negative-time underflow during the reload window in clip-display mode
  // applies here.
  const trueTime = Math.max(
    0,
    seekDisplayTarget != null
      ? seekDisplayTarget
      : reloading
        ? offsetStart
        : offsetStart + mediaCurrentTime,
  );

  return (
    <div className="flex items-center gap-1 text-white/80 text-xs tabular-nums shrink-0 px-1">
      <span className="text-white">{formatDuration(trueTime)}</span>
      <span className="text-white/50">/</span>
      {fileDuration > 0 && <span>{formatDuration(fileDuration)}</span>}
    </div>
  );
}

// ── Control bar ────────────────────────────────────────────────────────────────

interface ControlBarProps {
  Player: PlayerInstance;
  sources: PlayerSource[];
  activeSource: PlayerSource | null;
  onSourceChange: (source: PlayerSource) => void;
  sourceResolution?: string;
  markers: IMarker[];
  fileDuration: number;
  offsetStart: number;
  onSeek: (t: number) => void;
  reloading: boolean;
  seekDisplayTarget: number | null;
  isFullscreen: boolean;
  onToggleFullscreen: () => void;
  /** Hide the in-bar fullscreen button on hover-capable devices — set
   *  when an external surface (e.g. the lightbox) owns fullscreen and
   *  surfaces its own affordance. Touch devices still get the in-bar
   *  button (the YARL toolbar's 16px icon is hard to find on phones).
   *  The `f` hotkey still works because `onToggleFullscreen` itself
   *  delegates to the override. */
  hideFullscreenButton: boolean;
  /** Tri-state playback mode. The cycle button toggles loop / auto-advance /
   *  normal; mutual exclusion is enforced by the parent. */
  playbackMode: "normal" | "loop" | "advance";
  /** Whether the cycle should pass through the `advance` state — `false`
   *  on the standalone scene page where no `onNext` is wired. */
  canAdvance: boolean;
  onCyclePlaybackMode: () => void;
  onTogglePaused: () => void;
  /** Notifies the parent when any popup menu (speed/quality) opens or
   *  closes. Used so the parent can suppress its click-anywhere
   *  play/pause toggle for the click that dismisses the menu — Base UI's
   *  internal backdrop sits below YARL's lightbox portal so the close-
   *  click bubbles through to the player otherwise. */
  onMenuOpenChange?: (open: boolean) => void;
  clipBoundsEdit?: ClipBoundsEdit;
}

function ControlBar({
  Player,
  sources,
  activeSource,
  onSourceChange,
  sourceResolution,
  markers,
  fileDuration,
  offsetStart,
  onSeek,
  reloading,
  seekDisplayTarget,
  isFullscreen,
  onToggleFullscreen,
  hideFullscreenButton,
  playbackMode,
  canAdvance,
  onCyclePlaybackMode,
  onTogglePaused,
  onMenuOpenChange,
  clipBoundsEdit,
}: ControlBarProps) {
  const paused = Player.usePlayer((s) => s.paused);
  const muted = Player.usePlayer((s) => s.muted);
  const pip = Player.usePlayer((s) => s.pip);
  const canPip = Player.usePlayer((s) => s.pipAvailability === "available");
  const store = Player.usePlayer();

  return (
    // `mt-auto` pins the bar to the bottom of `<Controls.Root>`'s
    // flex-col regardless of which siblings render in the row above.
    // Without it, a brief render where neither the touch-overlay nor
    // the click-to-play div claims the flex-1 slot — e.g. the moment
    // `started` flips to true on first playback while React hasn't
    // yet painted the touch-overlay group — causes the bar to sit
    // adjacent to the top of the player area before snapping to the
    // bottom on the next frame.
    //
    // `relative z-10` lifts the bar above the now-`absolute inset-0`
    // touch-overlay and click-to-play siblings so its buttons stay
    // tappable; without an explicit z-index those positioned siblings
    // would paint over this static-positioned bar (per CSS stacking
    // rules: positioned-with-z-auto > non-positioned).
    <Controls.Group className="relative z-10 mt-auto flex flex-col gap-1 px-2 py-1 w-full bg-gradient-to-t from-black/70 to-transparent">
      <PositionSlider
        Player={Player}
        offsetStart={offsetStart}
        fileDuration={fileDuration}
        markers={markers}
        onSeek={onSeek}
        reloading={reloading}
        seekDisplayTarget={seekDisplayTarget}
        clipBoundsEdit={clipBoundsEdit}
      />

      <div className="flex items-center justify-between gap-1 w-full">
        <div className="flex items-center gap-1">
          <Button
            type="button"
            variant="ghost"
            size="icon"
            onClick={onTogglePaused}
            className={cn(OVERLAY_BTN, "text-white/80")}
            aria-label={paused ? "Play" : "Pause"}
          >
            {paused ? (
              <Play size={16} fill="currentColor" />
            ) : (
              <Pause size={16} fill="currentColor" />
            )}
          </Button>

          <RelSeekButton
            Player={Player}
            seconds={-10}
            className="hidden text-white/80 hover:text-white lg:flex"
            offsetStart={offsetStart}
            fileDuration={fileDuration}
            onSeek={onSeek}
          >
            <RotateCcw size={14} />
          </RelSeekButton>

          <RelSeekButton
            Player={Player}
            seconds={10}
            className="hidden text-white/80 hover:text-white lg:flex"
            offsetStart={offsetStart}
            fileDuration={fileDuration}
            onSeek={onSeek}
          >
            <RotateCw size={14} />
          </RelSeekButton>

          <Button
            type="button"
            variant="ghost"
            size="icon"
            onClick={() => store.toggleMuted()}
            className={cn(OVERLAY_BTN, "text-white/80")}
            aria-label={muted ? "Unmute" : "Mute"}
          >
            {muted ? <VolumeX size={16} /> : <Volume2 size={16} />}
          </Button>

          <VolumeSlider.Root className="hidden lg:relative lg:flex items-center w-20 h-8 group touch-none">
            <VolumeSlider.Track className="absolute inset-x-0 top-1/2 -translate-y-1/2 h-1 bg-white/30 rounded-full group-hover:h-1.5 transition-all" />
            <VolumeSlider.Fill className="absolute left-0 top-1/2 -translate-y-1/2 h-1 w-[var(--media-slider-fill)] bg-white rounded-full group-hover:h-1.5 transition-all" />
            <VolumeSlider.Thumb className="absolute top-1/2 -translate-y-1/2 -translate-x-1/2 left-[var(--media-slider-fill)] w-3 h-3 bg-white rounded-full opacity-0 group-hover:opacity-100 transition-opacity" />
          </VolumeSlider.Root>

          <TimeDisplay
            Player={Player}
            offsetStart={offsetStart}
            fileDuration={fileDuration}
            reloading={reloading}
            seekDisplayTarget={seekDisplayTarget}
          />
        </div>

        <div className="flex items-center gap-1">
          <PlaybackModeButton
            mode={playbackMode}
            canAdvance={canAdvance}
            onCycle={onCyclePlaybackMode}
          />

          <SpeedMenu Player={Player} onOpenChange={onMenuOpenChange} />

          <QualityMenu
            sources={sources}
            activeSource={activeSource}
            onSourceChange={onSourceChange}
            sourceResolution={sourceResolution}
            onOpenChange={onMenuOpenChange}
          />

          {canPip && (
            <Button
              type="button"
              variant="ghost"
              size="icon"
              onClick={() => store.togglePictureInPicture()}
              className={cn(OVERLAY_BTN, "text-white/80")}
              aria-label={
                pip ? "Exit picture-in-picture" : "Enter picture-in-picture"
              }
            >
              {pip ? (
                <PictureInPicture2 size={15} />
              ) : (
                <PictureInPicture size={15} />
              )}
            </Button>
          )}

          <CastButton
            className={(state) =>
              cn(
                OVERLAY_BTN,
                state.connection === "connected"
                  ? "text-blue-400"
                  : "text-white/80",
              )
            }
            render={
              <Button type="button" variant="ghost" size="icon">
                <Cast size={15} />
              </Button>
            }
          />

          <Button
            type="button"
            variant="ghost"
            size="icon"
            onClick={onToggleFullscreen}
            aria-label={isFullscreen ? "Exit fullscreen" : "Enter fullscreen"}
            // Hide on hover-capable devices when an external surface
            // (e.g. lightbox) owns fullscreen — its toolbar already
            // exposes the affordance there. Touch devices keep the
            // in-bar button so users aren't stuck hunting for the
            // 16px YARL toolbar icon at the top-right corner.
            className={cn(
              OVERLAY_BTN,
              "text-white/80",
              hideFullscreenButton && "[@media(hover:hover)]:hidden",
            )}
          >
            {isFullscreen ? <Minimize size={15} /> : <Maximize size={15} />}
          </Button>
        </div>
      </div>
    </Controls.Group>
  );
}

// ── Main export ────────────────────────────────────────────────────────────────

export interface PlayerControlsProps {
  Player: PlayerInstance;
  sources: PlayerSource[];
  activeSource: PlayerSource | null;
  onSourceChange: (source: PlayerSource) => void;
  sourceResolution?: string;
  markers: IMarker[];
  fileDuration?: number;
  frameRate?: number;
  offsetStart: number;
  onSeek: (trueTime: number) => void;
  disableSeekArrows?: boolean;
  hasEverStarted?: boolean;
  reloading?: boolean;
  seekDisplayTarget?: number | null;
  fullscreenContainerRef?: React.RefObject<HTMLElement | null>;
  /** Tri-state playback mode (normal / loop / advance). The advance step is
   *  available only when `canAdvance` is true. */
  playbackMode: "normal" | "loop" | "advance";
  canAdvance: boolean;
  onCyclePlaybackMode: () => void;
  /** Override for the play/pause toggle wired into every play button and
   *  the spacebar hotkey. Defaults to `store.togglePaused()`. The marker
   *  lightbox supplies a wrapper that intercepts an unpause-after-clip-end
   *  to also seek the playhead back to the clip start synchronously
   *  (Safari blocks programmatic `play()` that crosses an async boundary,
   *  so the seek + play has to happen inside the click-handler call
   *  stack). */
  onTogglePaused?: () => void;
  /** Notifies the parent that the user explicitly initiated playback
   *  outside the `onTogglePaused` path (the pre-start big-play-button
   *  click calls `store.play()` directly). The scene player feeds this
   *  into the play-delay gate so a quick click right after the lightbox
   *  opens isn't swallowed as suppressed autoplay. */
  onUserPlaybackGesture?: () => void;
  /** When set, replaces the player's built-in fullscreen logic and
   *  hides the in-bar fullscreen button on hover-capable devices.
   *  Touch devices keep the in-bar button (the alternative — typically
   *  YARL's 16px toolbar icon — is hard to find on phones). The `f`
   *  hotkey calls this instead. Used by the lightbox so fullscreen
   *  captures the entire slide UI (title overlay, swipe nav, toolbar)
   *  rather than just the video.
   *
   *  Return `false` to signal "couldn't handle it, please fall back" —
   *  used when the external surface's fullscreen is unavailable on the
   *  current device (e.g. YARL's Fullscreen plugin is disabled on iOS
   *  Safari pre-16.4 because Element.requestFullscreen isn't supported).
   *  In that case the player's native fullscreen path (container request,
   *  with v10 store fallback to webkitEnterFullscreen on the video) runs
   *  instead. Any other return — including `void` — counts as handled. */
  onToggleFullscreenOverride?: () => boolean | undefined;
  /** When set, the position slider grows two draggable handles for
   *  start / end editing. Drags emit `onChange` with new scene-time
   *  values; the playhead is left alone. Used by the scene detail page's
   *  marker editor. */
  clipBoundsEdit?: ClipBoundsEdit;
  /** When provided, this ref's `.current` is populated with a function
   *  that cancels any pending tap-to-toggle action on the touch overlay.
   *  `VideoFrameZoom` calls it on double-tap recognition so a double-tap-
   *  zoom doesn't flicker the controls / surrounding lightbox overlay
   *  between the first tap and the second.
   */
  cancelPendingTapToggleRef?: React.MutableRefObject<(() => void) | null>;
  /** When provided, this ref's `.current` is populated with a function
   *  that cancels any in-flight long-press 2×-speed hold and restores
   *  the previous playback rate. `VideoFrameZoom` calls it once a gesture
   *  is recognized as not-a-still-tap (pinch start, pan activation, or
   *  double-tap), so the long-press timer / 2× speed doesn't linger
   *  through a deliberate zoom or pan.
   */
  cancelPendingHoldRef?: React.MutableRefObject<(() => void) | null>;
  /** Keeps the independent speed indicator and zoom gesture ownership in sync
   *  with the temporary long-press playback rate. `null` means inactive. */
  onTemporaryPlaybackRateChange: (rate: number | null) => void;
}

export function PlayerControls({
  Player,
  sources,
  activeSource,
  onSourceChange,
  sourceResolution,
  markers,
  fileDuration,
  frameRate,
  offsetStart,
  onSeek,
  disableSeekArrows,
  hasEverStarted,
  reloading: reloadingProp,
  seekDisplayTarget = null,
  fullscreenContainerRef,
  playbackMode,
  canAdvance,
  onCyclePlaybackMode,
  onTogglePaused,
  onUserPlaybackGesture,
  onToggleFullscreenOverride,
  clipBoundsEdit,
  cancelPendingTapToggleRef,
  cancelPendingHoldRef,
  onTemporaryPlaybackRateChange,
}: PlayerControlsProps) {
  const duration = fileDuration ?? 0;
  const store = Player.usePlayer();
  // Default to the raw store toggle when the parent doesn't override.
  // The marker lightbox passes its own to intercept replay-from-stop.
  const togglePaused = onTogglePaused ?? (() => store.togglePaused());

  // Native fullscreen on the outer wrapper survives provider key-remounts
  // (quality switch, seek past buffer); v10's store-level fullscreen is used
  // as a fallback when the container can't be fullscreened (iOS → video
  // element fullscreen).
  const storeFullscreen = Player.usePlayer((s) => s.fullscreen);
  const [containerFullscreen, setContainerFullscreen] = useState(false);
  useEffect(() => {
    function onChange() {
      const el = fullscreenContainerRef?.current ?? null;
      setContainerFullscreen(!!el && document.fullscreenElement === el);
    }
    document.addEventListener("fullscreenchange", onChange);
    return () => document.removeEventListener("fullscreenchange", onChange);
  }, [fullscreenContainerRef]);
  const isFullscreen = containerFullscreen || storeFullscreen;

  const toggleFullscreen = useCallback(() => {
    // External override (e.g. lightbox YARL fullscreen) gets first
    // crack. The in-bar button (visible on touch, hidden on hover-
    // capable when an override is set) and the `f` hotkey both route
    // through here. The override returns `false` when it can't handle
    // the request (e.g. YARL's Fullscreen plugin is disabled on iOS
    // Safari pre-16.4 because Element.requestFullscreen isn't
    // supported); in that case fall through to the native path so
    // mobile users still get fullscreen via webkitEnterFullscreen on
    // the video element.
    if (onToggleFullscreenOverride && onToggleFullscreenOverride() !== false) {
      return;
    }
    const el = fullscreenContainerRef?.current;
    // Touch devices (iOS Safari especially): prefer
    // `webkitEnterFullscreen` on the video element directly.
    // Element.requestFullscreen on a `<div>` exists since iOS 16.4
    // but silently rejects in many configurations (and v10's
    // store-level requestFullscreen tries the container first too,
    // hitting the same failure). webkitEnterFullscreen on the video
    // has worked reliably since iOS 5; it's effectively the only
    // fullscreen path that consistently works on iPhone Safari.
    // Gated on `pointer: coarse` so desktop Safari (where
    // requestFullscreen on the player container is preferred so the
    // controls overlay survives in fullscreen) still uses the
    // container path.
    type IOSVideo = HTMLVideoElement & {
      webkitEnterFullscreen?: () => void;
      webkitExitFullscreen?: () => void;
      webkitDisplayingFullscreen?: boolean;
    };
    const isTouch = window.matchMedia("(pointer: coarse)").matches;
    const video = isTouch
      ? (el?.querySelector("video") as IOSVideo | null)
      : null;
    if (video && typeof video.webkitEnterFullscreen === "function") {
      if (video.webkitDisplayingFullscreen) {
        video.webkitExitFullscreen?.();
      } else {
        video.webkitEnterFullscreen();
      }
      return;
    }
    if (!el || typeof el.requestFullscreen !== "function") {
      void store.toggleFullscreen();
      return;
    }
    if (document.fullscreenElement === el) {
      document.exitFullscreen().catch(() => {});
    } else {
      el.requestFullscreen().catch(() => {});
    }
  }, [onToggleFullscreenOverride, fullscreenContainerRef, store]);

  useHotkeys({
    Player,
    offsetStart,
    fileDuration: duration,
    frameRate: frameRate ?? 0,
    onSeek,
    onToggleFullscreen: toggleFullscreen,
    onTogglePaused: togglePaused,
    seekDisplayTarget,
    disableSeekArrows,
  });
  const controlsVisible = Player.usePlayer((s) => s.controlsVisible);
  const started = Player.usePlayer((s) => s.started);
  const seeking = Player.usePlayer((s) => s.seeking);
  const muted = Player.usePlayer((s) => s.muted);
  const [pendingPlay, setPendingPlay] = useState(false);

  // Hold `pendingPlay` (and therefore the pre-start spinner) until the
  // initial seek-to-target completes. videojs/store flips `started` true
  // as soon as the seek begins, but the painted frame is still the
  // pre-seek frame on Safari — clearing `pendingPlay` on `started` alone
  // hid the spinner before `PlayerPoster` could release (which gates on
  // `!seeking`), leaving the user staring at a stale poster with no
  // loading indicator while the seek finished.
  useEffect(() => {
    if (started && !seeking) {
      setPendingPlay(false);
    }
  }, [started, seeking]);

  const reloading = !!reloadingProp;

  const [controlsGrace, setControlsGrace] = useState(hasEverStarted ?? false);
  useEffect(() => {
    if (!controlsGrace || !started) return;
    const t = setTimeout(() => setControlsGrace(false), 800);
    return () => clearTimeout(t);
  }, [controlsGrace, started]);

  // Track popup menu (speed/quality) open state. Used for two things:
  //   1. Suppress the click-anywhere play/pause toggle while a menu is
  //      open and for a short grace period after it closes — Base UI's
  //      internal backdrop absorbs the dismiss-click normally, but inside
  //      YARL's lightbox portal the click leaks through to the player.
  //   2. Pin the control bar visible while a menu is open, so the user
  //      can move the pointer off the player onto the menu without the
  //      bar fading out from under their selection.
  // State (not just a ref) so the opacity calculation re-renders when
  // a menu opens or closes.
  const [menuOpenCount, setMenuOpenCount] = useState(0);
  const menuClosedAtRef = useRef(0);
  const handleMenuOpenChange = useCallback((open: boolean) => {
    if (open) {
      setMenuOpenCount((c) => c + 1);
    } else {
      setMenuOpenCount((c) => Math.max(0, c - 1));
      menuClosedAtRef.current = Date.now();
    }
  }, []);
  const menuOpen = menuOpenCount > 0;
  const isMenuActive = useCallback(
    () => menuOpen || Date.now() - menuClosedAtRef.current < 300,
    [menuOpen],
  );

  // Deferred tap-to-toggle for the coarse-pointer touch overlay. The
  // empty-area tap path stops vjs's container-level pointerup from
  // running its setActive (see `onPointerUpCapture` below), then schedules
  // a toggle DOUBLE_TAP_MAX_MS after `click`. `VideoFrameZoom` calls into
  // `cancelPendingTapToggleRef.current` when it recognizes a double-tap,
  // which clears the pending toggle so the controls / lightbox overlay
  // don't flash between the first tap and the zoom-on-second-tap. The
  // delay is the same constant `VideoFrameZoom` uses for double-tap
  // recognition — they must match so the cancellation window covers the
  // full recognition window. Imported (rather than redeclared) so the two
  // can't drift.
  const tapToggleTimerRef = useRef<number | null>(null);
  const controlsVisibleAtPointerDownRef = useRef(false);
  const activeTouchPointerIdRef = useRef<number | null>(null);
  const touchTapCandidateRef = useRef<
    (TouchTapCandidate & { pointerId: number }) | null
  >(null);
  const completedTouchTapRef = useRef(false);
  const clearPendingTapToggle = useCallback(() => {
    if (tapToggleTimerRef.current != null) {
      window.clearTimeout(tapToggleTimerRef.current);
      tapToggleTimerRef.current = null;
    }
  }, []);
  useEffect(() => {
    if (!cancelPendingTapToggleRef) return;
    cancelPendingTapToggleRef.current = clearPendingTapToggle;
    return () => {
      if (cancelPendingTapToggleRef.current === clearPendingTapToggle) {
        cancelPendingTapToggleRef.current = null;
      }
    };
  }, [cancelPendingTapToggleRef, clearPendingTapToggle]);
  useEffect(() => clearPendingTapToggle, [clearPendingTapToggle]);

  // Long-press-to-2×-speed on the coarse-pointer touch overlay. Single-
  // finger touch held on the overlay's empty space for `HOLD_PRESS_MS`
  // bumps playbackRate to `HOLD_PRESS_RATE` until release. While pending,
  // normal touch slop is allowed; once active, the finger owns the gesture
  // and movement no longer cancels it. Restored on touchend / cancel.
  //
  // Coordinated with `VideoFrameZoom`'s gesture recognizer via
  // `cancelPendingHoldRef`: pinch / pan-active / double-tap all cancel
  // the hold so a deliberate zoom or pan doesn't also speed up the
  // video. The pinch case is special — `VideoFrameZoom` stops the second
  // finger's touchstart from reaching our React handler, so the callback
  // is the only way we learn about it.
  //
  // `holdFiredRef` is set when the timer fires and read by `onClick`
  // below so the synthesized click on release doesn't also trigger the
  // deferred controls toggle. Always reset at the next touchstart, in
  // case a gesture aborted without firing onClick (e.g. preventDefault
  // on touchmove from `VideoFrameZoom` suppresses the click).
  const HOLD_PRESS_MS = 500;
  const HOLD_PRESS_RATE = 2;
  const holdTimerRef = useRef<number | null>(null);
  const holdOriginalRateRef = useRef<number | null>(null);
  const holdStartPosRef = useRef<{ x: number; y: number } | null>(null);
  const holdFiredRef = useRef(false);
  const cancelHold = useCallback(() => {
    if (holdTimerRef.current != null) {
      window.clearTimeout(holdTimerRef.current);
      holdTimerRef.current = null;
    }
    if (holdOriginalRateRef.current != null) {
      store.setPlaybackRate(holdOriginalRateRef.current);
      holdOriginalRateRef.current = null;
    }
    holdStartPosRef.current = null;
    onTemporaryPlaybackRateChange(null);
  }, [store, onTemporaryPlaybackRateChange]);
  useEffect(() => {
    if (!cancelPendingHoldRef) return;
    cancelPendingHoldRef.current = cancelHold;
    return () => {
      if (cancelPendingHoldRef.current === cancelHold) {
        cancelPendingHoldRef.current = null;
      }
    };
  }, [cancelPendingHoldRef, cancelHold]);
  useEffect(() => cancelHold, [cancelHold]);

  return (
    <Controls.Root
      className={cn(
        "absolute inset-0 flex flex-col transition-opacity duration-300",
        started && !controlsVisible && !controlsGrace && !menuOpen
          ? "opacity-0"
          : "opacity-100",
      )}
    >
      {started && (
        // `absolute inset-0` so the seek/play row centers against the
        // *whole* player area instead of just the slice above the
        // ControlBar — that's what brings the three touch buttons in
        // line with the YARL lightbox prev/next buttons (which sit at
        // viewport-center). Combined with the lightbox bypass of the
        // mobile-cap (`[data-scene-player][data-fill]`) so the player
        // truly fills the viewport, the centerlines match exactly.
        <Controls.Group
          className="[@media(pointer:coarse)]:flex hidden absolute inset-0 items-center justify-around"
          onPointerDownCapture={(e) => {
            controlsVisibleAtPointerDownRef.current = controlsVisible;
            completedTouchTapRef.current = e.pointerType !== "touch";
            activeTouchPointerIdRef.current =
              e.pointerType === "touch" ? e.pointerId : null;
            touchTapCandidateRef.current =
              e.pointerType === "touch" && e.target === e.currentTarget
                ? {
                    pointerId: e.pointerId,
                    startedAt: e.timeStamp,
                    startX: e.clientX,
                    startY: e.clientY,
                    moved: false,
                  }
                : null;
          }}
          onPointerMove={(e) => {
            if (
              e.pointerType !== "touch" ||
              e.pointerId !== activeTouchPointerIdRef.current
            ) {
              return;
            }

            const candidate = touchTapCandidateRef.current;
            if (
              candidate &&
              !candidate.moved &&
              exceedsTouchTapMovement(candidate, e.clientX, e.clientY)
            ) {
              candidate.moved = true;
            }
          }}
          onTouchStart={(e) => {
            // Reset cross-gesture flag from any prior hold; the click
            // handler clears it on consume but movement-aborted gestures
            // (preventDefault from `VideoFrameZoom`'s active pan) never
            // synthesize a click to consume it.
            holdFiredRef.current = false;
            // Long-press only on empty-area taps (target===currentTarget)
            // so taps on the inner play / skip buttons keep their normal
            // tap-to-activate semantics. Multi-touch (pinch) cancels any
            // pending hold immediately. The lightbox's `VideoFrameZoom`
            // also stops the second finger's touchstart from reaching
            // here at all, so the parallel `onActiveGesture` callback is
            // what fires `cancelHold` in that path.
            if (e.target !== e.currentTarget) {
              cancelHold();
              return;
            }
            if (e.touches.length !== 1) {
              cancelHold();
              return;
            }
            const t = e.touches[0];
            cancelHold();
            holdStartPosRef.current = { x: t.clientX, y: t.clientY };
            holdTimerRef.current = window.setTimeout(() => {
              holdTimerRef.current = null;
              holdOriginalRateRef.current = store.state.playbackRate ?? 1;
              store.setPlaybackRate(HOLD_PRESS_RATE);
              holdFiredRef.current = true;
              onTemporaryPlaybackRateChange(HOLD_PRESS_RATE);
              // The hold consumes the gesture; suppress the deferred tap-
              // to-toggle that the eventual `click` (after release) would
              // otherwise schedule. The flag in `onClick` is a safety net,
              // but clearing now avoids any race where the toggle is
              // already scheduled when the click finally fires.
              clearPendingTapToggle();
            }, HOLD_PRESS_MS);
          }}
          onTouchMove={(e) => {
            const start = holdStartPosRef.current;
            if (!start) return;
            const touch = e.touches.length === 1 ? e.touches[0] : null;
            if (
              shouldCancelTouchHoldOnMove(
                start,
                touch ? { x: touch.clientX, y: touch.clientY } : null,
                e.touches.length,
                holdOriginalRateRef.current != null,
              )
            ) {
              cancelHold();
            }
          }}
          onTouchEnd={() => {
            cancelHold();
          }}
          onTouchCancel={() => {
            cancelHold();
            holdFiredRef.current = false;
          }}
          onPointerUpCapture={(e) => {
            if (
              e.pointerType === "touch" &&
              e.pointerId === activeTouchPointerIdRef.current
            ) {
              const candidate = touchTapCandidateRef.current;
              completedTouchTapRef.current =
                e.target === e.currentTarget &&
                candidate?.pointerId === e.pointerId &&
                isCompletedTouchTap(
                  candidate,
                  e.timeStamp,
                  e.clientX,
                  e.clientY,
                );
              activeTouchPointerIdRef.current = null;
              touchTapCandidateRef.current = null;
            }

            // Suppress vjs's container-level pointerup → setActive for
            // taps on the overlay's empty space, so a double-tap-zoom
            // doesn't flash controls between the first tap's pointerup
            // and `VideoFrameZoom` recognising the second tap. The toggle
            // is performed by our own deferred timer in `onClick` below,
            // which `VideoFrameZoom` cancels on double-tap. Only stop
            // empty-area taps — taps on the inner buttons (play / skip)
            // still need vjs's setActive to reset the auto-hide idle
            // timer. Limited to touch pointers; mouse taps run vjs's
            // normal activity-tracking path.
            if (e.target !== e.currentTarget) return;
            if (e.pointerType !== "touch") return;
            e.stopPropagation();
          }}
          onPointerCancelCapture={(e) => {
            if (
              e.pointerType !== "touch" ||
              e.pointerId !== activeTouchPointerIdRef.current
            ) {
              return;
            }
            activeTouchPointerIdRef.current = null;
            touchTapCandidateRef.current = null;
            completedTouchTapRef.current = false;
          }}
          onClickCapture={(e) => {
            // The Quality / Speed menus use `modal={false}`, so the tap
            // that dismisses the menu also lands on whatever sits
            // underneath — on mobile that's this overlay (with the
            // big play/pause and seek buttons inside TouchOverlay).
            // Without this guard, dismissing the menu also toggles
            // playback or seeks. Stopping propagation in the capture
            // phase prevents the inner button onClicks from firing as
            // well as our own onClick below. The 300ms grace inside
            // `isMenuActive()` covers the gap between Base UI's
            // outside-click handler firing on pointerdown and React's
            // click event arriving here.
            if (isMenuActive()) {
              e.stopPropagation();
              e.preventDefault();
            }
          }}
          onClick={(e) => {
            // Only react to taps on the empty space between the touch
            // overlay's big buttons; the buttons themselves stop the
            // event by virtue of being descendants — `e.target ===
            // e.currentTarget` matches only when the tap landed on the
            // group itself.
            if (e.target !== e.currentTarget) return;
            // The synthesized click after releasing a long-press should
            // not also toggle controls — the hold already consumed the
            // gesture. Consume and clear the flag here so a follow-up
            // single tap (new gesture) still toggles normally.
            if (holdFiredRef.current) {
              holdFiredRef.current = false;
              completedTouchTapRef.current = false;
              return;
            }
            // Only a short, stationary press followed by release is a tap.
            // A hold or drag is consumed and must be followed by a fresh tap
            // before controls can be revealed.
            if (!completedTouchTapRef.current) return;
            completedTouchTapRef.current = false;
            const wasVisible = controlsVisibleAtPointerDownRef.current;
            clearPendingTapToggle();
            tapToggleTimerRef.current = window.setTimeout(() => {
              tapToggleTimerRef.current = null;
              // `wasVisible` captured at pointerdown is the pre-tap
              // state — vjs's setActive was suppressed above, so the
              // store's current `controlsVisible` still matches. Toggle
              // flips it: hidden → setActive (reveal), visible →
              // setInactive (dismiss).
              if (!wasVisible && muted) {
                // Tap-to-unmute on the reveal tap: clears an autoplay-
                // fallback mute alongside the controls reveal. One-way —
                // re-muting is via the explicit mute button in the bar.
                store.toggleMuted();
              }
              store.toggleControls();
            }, DOUBLE_TAP_MAX_MS);
          }}
        >
          <TouchOverlay
            Player={Player}
            offsetStart={offsetStart}
            fileDuration={duration}
            onSeek={onSeek}
            onTogglePaused={togglePaused}
          />
        </Controls.Group>
      )}
      {(() => {
        // `pendingPlay` keeps us in pre-start so the spinner above the
        // Play button stays mounted until the initial seek finishes —
        // see the `pendingPlay` effect above for the rationale.
        const mode: "pre-start" | "reloading" | "playing" =
          pendingPlay || (!started && !hasEverStarted)
            ? "pre-start"
            : reloading
              ? "reloading"
              : "playing";

        return (
          // `absolute inset-0` (rather than `flex-1` in the flex-col)
          // so the pre-start play button / spinner sit at the player's
          // true vertical center — aligning with the touch-overlay
          // buttons (also absolute inset-0) and the YARL lightbox
          // prev/next arrows. Without this, the play button centers in
          // the slice above the ControlBar and is offset upward.
          <div
            className={cn(
              "absolute inset-0 flex items-center justify-center pointer-events-auto",
              mode === "pre-start" && "cursor-pointer",
              mode === "playing" &&
                "[@media(pointer:fine)]:cursor-pointer [@media(pointer:coarse)]:hidden",
            )}
            onClick={() => {
              if (mode === "reloading") return;
              if (isMenuActive()) return;
              if (mode === "pre-start") {
                onUserPlaybackGesture?.();
                setPendingPlay(true);
                void store.play();
              } else {
                togglePaused();
              }
            }}
          >
            {mode === "pre-start" &&
              (pendingPlay ? (
                <Spinner className="size-12 text-white pointer-events-none" />
              ) : (
                <div className="flex items-center justify-center w-20 h-20 rounded-full bg-black/40 text-white pointer-events-none">
                  <Play size={40} fill="white" />
                </div>
              ))}
          </div>
        );
      })()}

      <ControlBar
        Player={Player}
        sources={sources}
        activeSource={activeSource}
        onSourceChange={onSourceChange}
        sourceResolution={sourceResolution}
        markers={markers}
        fileDuration={duration}
        offsetStart={offsetStart}
        onSeek={onSeek}
        reloading={reloading}
        seekDisplayTarget={seekDisplayTarget}
        isFullscreen={isFullscreen}
        onToggleFullscreen={toggleFullscreen}
        hideFullscreenButton={!!onToggleFullscreenOverride}
        playbackMode={playbackMode}
        canAdvance={canAdvance}
        onCyclePlaybackMode={onCyclePlaybackMode}
        onTogglePaused={togglePaused}
        onMenuOpenChange={handleMenuOpenChange}
        clipBoundsEdit={clipBoundsEdit}
      />
    </Controls.Root>
  );
}
