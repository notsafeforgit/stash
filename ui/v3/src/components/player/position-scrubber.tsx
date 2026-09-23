import type React from "react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useMsg } from "@/hooks/message";
import { useCommittedRef } from "@/hooks/use-committed-ref";
import { cn } from "@/lib/utils";
import type { PlaybackRange } from "@/core/marker-range";

export interface ClipBoundsEdit {
  start: number | null;
  end: number | null;
  onChange: (next: { start?: number | null; end?: number | null }) => void;
}

const DWELL_MS = 650;
const PRECISION_DWELL_MS = 1200;
const DWELL_SLOP = 4;
const MIN_WINDOW_SECONDS = 1;

interface TouchScrub {
  point: { x: number; y: number };
  dwellOrigin: { x: number; y: number };
  ratio: number;
  range: PlaybackRange;
}

/** The video scrubber shared by standard controls and TV. All times are in
 * the caller's display coordinates; seeking into a scene stays at its boundary.
 * Pointer drags can preview buffered frames and commit once, including when rotated. */
export function PositionScrubber({
  value,
  duration,
  bufferedRanges = [],
  bufferedOffset = 0,
  disabled = false,
  direction = "right",
  markers,
  clipBoundsEdit,
  onSeek,
  onSeekPreview,
  onScrubChange,
  onPreviewChange,
}: {
  value: number;
  duration: number;
  bufferedRanges?: readonly PlaybackRange[];
  /** Translate buffered scene times into this segment's display coordinates. */
  bufferedOffset?: number;
  disabled?: boolean;
  direction?: "right" | "down" | "up";
  markers?: ReactNode;
  clipBoundsEdit?: ClipBoundsEdit;
  onSeek: (time: number) => void;
  /** Preview a drag position; null cancels and restores the pre-drag position. */
  onSeekPreview?: (time: number | null) => void;
  onScrubChange?: (time: number | null) => void;
  onPreviewChange?: (visible: boolean) => void;
}) {
  const msg = useMsg();
  const trackRef = useRef<HTMLDivElement>(null);
  const pointer = useRef<number | null>(null);
  const origin = useRef({ x: 0, y: 0 });
  const previewing = useRef(false);
  const touchScrub = useRef<TouchScrub | null>(null);
  const dwellTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const callbacks = useCommittedRef({
    onSeekPreview,
    onScrubChange,
    onPreviewChange,
  });
  useEffect(
    () => () => {
      if (dwellTimer.current !== null) clearTimeout(dwellTimer.current);
      touchScrub.current = null;
      if (previewing.current) callbacks.current.onSeekPreview?.(null);
      if (pointer.current !== null) {
        callbacks.current.onScrubChange?.(null);
        callbacks.current.onPreviewChange?.(false);
      }
    },
    [],
  );
  const [dragTime, setDragTime] = useState<number | null>(null);
  const [precisionRange, setPrecisionRange] = useState<PlaybackRange | null>(
    null,
  );
  const unavailable = disabled || duration <= 0;
  const clamp = (time: number) => Math.max(0, Math.min(duration, time));
  const displayTime = clamp(dragTime ?? value);
  const range = precisionRange ?? { start: 0, end: duration };
  const span = range.end - range.start;
  const progress = span > 0 ? (displayTime - range.start) / span : 0;
  const change = (time: number | null) => {
    setDragTime(time);
    onScrubChange?.(time);
  };
  const pointerRatio = (event: React.PointerEvent<HTMLDivElement>) => {
    const rect = event.currentTarget.getBoundingClientRect();
    const ratio =
      direction === "right"
        ? (event.clientX - rect.left) / Math.max(1, rect.width)
        : (direction === "down"
            ? event.clientY - rect.top
            : rect.bottom - event.clientY) / Math.max(1, rect.height);
    return Math.max(0, Math.min(1, ratio));
  };
  const position = (event: React.PointerEvent<HTMLDivElement>) => {
    const visibleRange = touchScrub.current?.range ?? {
      start: 0,
      end: duration,
    };
    return clamp(
      visibleRange.start +
        pointerRatio(event) * (visibleRange.end - visibleRange.start),
    );
  };
  const clearDwell = () => {
    if (dwellTimer.current !== null) clearTimeout(dwellTimer.current);
    dwellTimer.current = null;
  };
  const scheduleDwell = () => {
    clearDwell();
    const touch = touchScrub.current;
    if (!touch || touch.range.end - touch.range.start <= MIN_WINDOW_SECONDS)
      return;
    touch.dwellOrigin = touch.point;
    const zoomed = touch.range.end - touch.range.start < duration;
    dwellTimer.current = setTimeout(
      () => {
        if (touchScrub.current !== touch) return;
        const previousSpan = touch.range.end - touch.range.start;
        // Long scenes enter a useful minute-wide window on the first dwell.
        // Further dwells refine it down to a second, including short marker clips.
        const nextSpan = Math.max(
          MIN_WINDOW_SECONDS,
          Math.min(60, previousSpan / 4),
        );
        const time = touch.range.start + touch.ratio * previousSpan;
        const start = time - touch.ratio * nextSpan;
        touch.range = { start, end: start + nextSpan };
        setPrecisionRange(touch.range);
        // A stationary hold also starts the buffered preview pause. Zooming
        // changes only the scale: the time under the finger stays anchored.
        previewing.current = true;
        callbacks.current.onSeekPreview?.(time);
        navigator.vibrate?.(10);
        scheduleDwell();
      },
      zoomed ? PRECISION_DWELL_MS : DWELL_MS,
    );
  };
  const finish = () => {
    clearDwell();
    touchScrub.current = null;
    setPrecisionRange(null);
    pointer.current = null;
    previewing.current = false;
    change(null);
    onPreviewChange?.(false);
  };
  const cancel = () => {
    if (previewing.current) onSeekPreview?.(null);
    finish();
  };
  return (
    // Center the desktop hit area on the thin bar to allow misses on either side.
    // Touch keeps its bottom-aligned bar and expanding precision timeline.
    // Stop drag events before they reach the lightbox carousel / TV gestures.
    <div
      ref={trackRef}
      role="slider"
      tabIndex={unavailable ? -1 : 0}
      aria-label={msg("media_player.position", "Playback position")}
      aria-valuemin={0}
      aria-valuemax={duration}
      aria-valuenow={displayTime}
      aria-valuetext={`${precisionRange ? formatDurationMs(displayTime) : formatTime(displayTime)} / ${formatTime(duration)}`}
      aria-disabled={unavailable}
      aria-orientation={direction === "right" ? "horizontal" : "vertical"}
      data-position-scrubber
      data-dragging={dragTime !== null || undefined}
      data-precision={precisionRange ? true : undefined}
      className={cn(
        "group/scrubber relative flex h-8 w-full min-w-[4em] cursor-pointer select-none items-center pointer-coarse:h-5 pointer-coarse:items-end [-webkit-touch-callout:none]",
        precisionRange && "h-16 pointer-coarse:h-16",
      )}
      // Override the unlayered page-zoom guard; a Tailwind touch-none utility
      // loses to it and lets native scrolling cancel this pointer drag.
      style={{ touchAction: "none" }}
      onFocus={() => onPreviewChange?.(true)}
      onBlur={() => {
        if (pointer.current !== null) cancel();
        onPreviewChange?.(false);
      }}
      onPointerDown={(event) => {
        // Hidden standard controls keep the first tap free to reveal them.
        if (unavailable || event.button !== 0 || pointer.current !== null)
          return;
        event.stopPropagation();
        event.preventDefault();
        pointer.current = event.pointerId;
        origin.current = { x: event.clientX, y: event.clientY };
        event.currentTarget.focus({ preventScroll: true });
        event.currentTarget.setPointerCapture(event.pointerId);
        onPreviewChange?.(true);
        change(position(event));
        if (event.pointerType === "touch") {
          touchScrub.current = {
            point: { x: event.clientX, y: event.clientY },
            dwellOrigin: { x: event.clientX, y: event.clientY },
            ratio: pointerRatio(event),
            range: { start: 0, end: duration },
          };
          scheduleDwell();
        }
      }}
      onPointerMove={(event) => {
        if (pointer.current !== event.pointerId) return;
        event.stopPropagation();
        const next = position(event);
        change(next);
        const touch = touchScrub.current;
        if (touch) {
          touch.point = { x: event.clientX, y: event.clientY };
          touch.ratio = pointerRatio(event);
          const zoom = duration / (touch.range.end - touch.range.start);
          // Allow less jitter as the timeline magnifies: 4 px at full scale,
          // 2 px at 4x, and 1 px from 16x onward. Fine adjustments must reset
          // the next zoom instead of quickly cascading to maximum precision.
          const slop = Math.max(1, DWELL_SLOP / Math.sqrt(zoom));
          // Compare against the dwell origin, not the previous event, so
          // slow deliberate movement cannot masquerade as finger jitter.
          if (
            Math.hypot(
              event.clientX - touch.dwellOrigin.x,
              event.clientY - touch.dwellOrigin.y,
            ) >= slop
          ) {
            scheduleDwell();
          }
        }
        // A tap retains uninterrupted playback. Only an actual drag starts
        // the temporary preview pause, after allowing for touch jitter.
        previewing.current ||=
          Math.hypot(
            event.clientX - origin.current.x,
            event.clientY - origin.current.y,
          ) >= 3;
        if (previewing.current) onSeekPreview?.(next);
      }}
      onPointerUp={(event) => {
        if (pointer.current !== event.pointerId) return;
        event.stopPropagation();
        const next = position(event);
        finish();
        if (event.currentTarget.hasPointerCapture(event.pointerId))
          event.currentTarget.releasePointerCapture(event.pointerId);
        onSeek(next);
      }}
      onPointerCancel={(event) => {
        if (pointer.current !== event.pointerId) return;
        event.stopPropagation();
        cancel();
      }}
      onLostPointerCapture={() => {
        if (pointer.current !== null) cancel();
      }}
      onContextMenu={(event) => {
        if (pointer.current !== null) event.preventDefault();
      }}
      // Claim seek keys before the player's native keyboard listener can
      // interpret them as playback or lightbox navigation shortcuts.
      onKeyDownCapture={(event) => {
        if (unavailable) return;
        const step = event.shiftKey ? 10 : 5;
        let next: number;
        switch (event.key) {
          case "Escape":
            if (pointer.current === null) return;
            event.preventDefault();
            event.stopPropagation();
            cancel();
            return;
          case "ArrowRight":
          case "ArrowUp":
            next = displayTime + step;
            break;
          case "ArrowLeft":
          case "ArrowDown":
            next = displayTime - step;
            break;
          case "PageUp":
            next = displayTime + 10;
            break;
          case "PageDown":
            next = displayTime - 10;
            break;
          case "Home":
            next = 0;
            break;
          case "End":
            next = duration;
            break;
          default:
            return;
        }
        event.preventDefault();
        event.stopPropagation();
        onSeek(clamp(next));
      }}
    >
      {precisionRange && (
        <>
          <div
            data-position-scrubber-precision
            aria-hidden="true"
            className="pointer-events-none absolute inset-x-0 bottom-16 flex justify-center text-xs text-foreground"
          >
            <span className="rounded bg-background/90 px-2 py-1 tabular-nums">
              {msg("media_player.fine_seeking", "Fine seeking")} ·{" "}
              {formatDurationMs(displayTime)} · {Math.round(duration / span)}×
            </span>
          </div>
          <div
            className="pointer-events-none absolute inset-x-0 bottom-3 flex justify-between"
            aria-hidden="true"
          >
            {Array.from({ length: 11 }, (_, index) => (
              <span
                key={index}
                className={cn(
                  "w-px bg-white/60",
                  index % 5 === 0 ? "h-2" : "h-1",
                )}
              />
            ))}
          </div>
        </>
      )}
      <div
        data-position-scrubber-track
        className="relative h-1 w-full rounded-sm bg-white/25 transition-[height] group-hover/scrubber:h-1.5 group-focus-visible/scrubber:h-1.5 group-data-[dragging]/scrubber:h-1.5"
        style={{ clipPath: precisionRange ? "inset(-100vh 0)" : undefined }}
      >
        {duration > 0 &&
          bufferedRanges.map(({ start, end }) => {
            const left = Math.max(range.start, clamp(start + bufferedOffset));
            const right = Math.min(range.end, clamp(end + bufferedOffset));
            if (right <= left) return null;
            return (
              <div
                key={start}
                data-position-scrubber-buffer
                className="absolute inset-y-0 rounded-sm bg-white/40"
                style={{
                  left: `${((left - range.start) / span) * 100}%`,
                  width: `${((right - left) / span) * 100}%`,
                }}
              />
            );
          })}
        <div
          data-position-scrubber-progress
          className="absolute inset-y-0 left-0 rounded-sm bg-white"
          style={{ width: `${progress * 100}%` }}
        />
        <div
          className="pointer-events-none absolute bottom-[calc(100%+2px)] transition-[left,width] duration-150 motion-reduce:transition-none"
          style={{
            left: span > 0 ? `${(-range.start / span) * 100}%` : "0%",
            width: span > 0 ? `${(duration / span) * 100}%` : "100%",
          }}
        >
          {markers}
        </div>
        <div
          data-position-scrubber-thumb
          className="absolute top-1/2 size-3 -translate-x-1/2 -translate-y-1/2 rounded-full bg-white opacity-0 transition-opacity group-hover/scrubber:opacity-100 group-focus-visible/scrubber:opacity-100 group-data-[dragging]/scrubber:opacity-100"
          style={{ left: `${progress * 100}%` }}
        />
        {clipBoundsEdit && duration > 0 && (
          <>
            {clipBoundsEdit.start != null && (
              <ClipBoundHandle
                trackRef={trackRef}
                boundary="start"
                time={clipBoundsEdit.start}
                range={range}
                onDrag={(t) => clipBoundsEdit.onChange({ start: t })}
              />
            )}
            {clipBoundsEdit.end != null && (
              <ClipBoundHandle
                trackRef={trackRef}
                boundary="end"
                time={clipBoundsEdit.end}
                range={range}
                onDrag={(t) => clipBoundsEdit.onChange({ end: t })}
              />
            )}
          </>
        )}
      </div>
    </div>
  );
}

function formatTime(seconds: number): string {
  const value = Math.max(0, Math.floor(seconds));
  const hours = Math.floor(value / 3600);
  return `${hours ? `${hours}:` : ""}${String(Math.floor(value / 60) % 60).padStart(hours ? 2 : 1, "0")}:${String(value % 60).padStart(2, "0")}`;
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
  range,
  onDrag,
}: {
  trackRef: React.RefObject<HTMLDivElement | null>;
  boundary: "start" | "end";
  time: number;
  range: PlaybackRange;
  onDrag: (t: number) => void;
}) {
  const draggingRef = useRef(false);
  const span = range.end - range.start;
  const progress = span > 0 ? (time - range.start) / span : 0;

  function timeFromPointer(clientX: number): number {
    const track = trackRef.current;
    if (!track || span <= 0) return 0;
    const rect = track.getBoundingClientRect();
    const ratio = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width));
    return range.start + ratio * span;
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
      // player. `touchAction` already disables panning/zooming gestures
      // here, but iOS treats text selection as a separate concern.
      className="absolute top-1/2 -translate-y-1/2 -translate-x-1/2 w-5 h-7 cursor-ew-resize select-none [-webkit-touch-callout:none] z-10"
      style={{ left: `${progress * 100}%`, touchAction: "none" }}
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
