import { useCallback, useEffect, useRef, type RefObject } from "react";
import { useCommittedRef } from "@/hooks/use-committed-ref";
import { useOverlayShortcutsBlocked } from "@/components/shortcut-provider";
import { useScenePlayerControls } from "@/components/player/scene-player-controls";
import type { TvRotation } from "@/core/tv/settings";
import { tvCoordinates } from "./use-tv-presentation";

export type TvInputCommand =
  | { type: "navigate"; direction: -1 | 1 }
  | { type: "seek"; direction: -1 | 1 }
  | {
      type: "action";
      action:
        | "delete"
        | "tags"
        | "fullscreen"
        | "info"
        | "completion"
        | "rotation"
        | "subtitles"
        | "visibility";
    }
  | { type: "exit-presentation" };

export function useTvInputs({
  surface,
  selectionKey,
  rotation,
  blocked,
  dispatch,
  drag,
  cancelDrag,
}: {
  surface: RefObject<HTMLDivElement | null>;
  selectionKey: string;
  rotation: TvRotation;
  blocked: boolean;
  dispatch: (command: TvInputCommand) => void;
  drag: (offset: number) => void;
  cancelDrag: () => void;
}) {
  const controls = useScenePlayerControls();
  const overlaysBlocked = useOverlayShortcutsBlocked();
  const latest = useCommittedRef({
    rotation,
    blocked,
    dispatch,
    drag,
    cancelDrag,
    selectionKey,
  });
  const hold = useRef<{
    direction: -1 | 1;
    originalRate: number;
    wasPaused: boolean;
    speed: number;
    key: string;
    started: boolean;
  } | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const reverseTimer = useRef<ReturnType<typeof setInterval> | undefined>(
    undefined,
  );
  const suppressClick = useRef(false);
  const endHold = useCallback(() => {
    clearTimeout(timer.current);
    clearInterval(reverseTimer.current);
    const current = hold.current;
    hold.current = null;
    if (current?.started) {
      suppressClick.current = true;
      // WebKit can finish a rate change using the prior playing state.
      // Restore pause first so releasing a hold cannot resume playback.
      if (current.wasPaused) controls.pause();
      controls.setRate(current.originalRate);
      if (
        !current.wasPaused &&
        current.direction === -1 &&
        current.key === latest.current.selectionKey
      )
        controls.play();
    }
  }, [controls]);
  const startHold = useCallback(
    (direction: -1 | 1) => {
      if (hold.current || latest.current.blocked || overlaysBlocked()) return;
      suppressClick.current = false;
      const state = controls.read();
      hold.current = {
        direction,
        originalRate: state.rate,
        wasPaused: state.paused,
        speed: 2,
        key: latest.current.selectionKey,
        started: false,
      };
      timer.current = setTimeout(() => {
        const current = hold.current;
        if (!current) return;
        current.started = true;
        if (direction === 1) {
          controls.setRate(current.speed);
          controls.play();
        } else {
          controls.pause();
          reverseTimer.current = setInterval(() => {
            const active = hold.current;
            if (!active || active.key !== latest.current.selectionKey) {
              endHold();
              return;
            }
            controls.seek(controls.read().position - active.speed * 0.2);
          }, 200);
        }
      }, 350);
    },
    [controls, endHold, overlaysBlocked],
  );
  const seekClick = useCallback((direction: -1 | 1) => {
    if (suppressClick.current) {
      suppressClick.current = false;
      return;
    }
    latest.current.dispatch({ type: "seek", direction });
  }, []);
  useEffect(() => {
    if (hold.current?.direction === -1 && hold.current.key !== selectionKey)
      endHold();
  }, [selectionKey, endHold]);
  useEffect(() => {
    const element = surface.current;
    if (!element) return;
    const ignored = (target: EventTarget | null) =>
      target instanceof Element &&
      !!target.closest(
        "button,a,input,textarea,select,[role=slider],[role=dialog],[role=menu],[contenteditable=true],[data-tv-interactive]",
      );
    const blockedNow = () => latest.current.blocked || overlaysBlocked();
    let pointer:
      | { id: number; x: number; y: number; moved: boolean }
      | undefined;
    const pointers = new Set<number>();
    let wheelLast = 0;
    let wheelTotal = 0;
    let wheelCommitted = false;
    const cancel = () => {
      pointer = undefined;
      pointers.clear();
      endHold();
      latest.current.cancelDrag();
    };
    const down = (event: PointerEvent) => {
      pointers.add(event.pointerId);
      if (pointers.size > 1) {
        pointer = undefined;
        latest.current.cancelDrag();
        return;
      }
      if (
        blockedNow() ||
        ignored(event.target) ||
        controls.read().zoomed ||
        event.button !== 0
      )
        return;
      pointer = {
        id: event.pointerId,
        x: event.clientX,
        y: event.clientY,
        moved: false,
      };
    };
    const move = (event: PointerEvent) => {
      if (
        !pointer ||
        event.pointerId !== pointer.id ||
        blockedNow() ||
        controls.read().zoomed ||
        event.defaultPrevented
      )
        return;
      const delta = tvCoordinates(
        event.clientX - pointer.x,
        event.clientY - pointer.y,
        latest.current.rotation,
      );
      if (!pointer.moved && Math.abs(delta.y) < 10) return;
      if (!pointer.moved && Math.abs(delta.x) > Math.abs(delta.y)) {
        pointer = undefined;
        return;
      }
      pointer.moved = true;
      element.setPointerCapture(event.pointerId);
      event.preventDefault();
      latest.current.drag(
        Math.max(
          -element.clientHeight * 0.8,
          Math.min(element.clientHeight * 0.8, delta.y),
        ),
      );
    };
    const up = (event: PointerEvent) => {
      pointers.delete(event.pointerId);
      if (!pointer || pointer.id !== event.pointerId) return;
      const current = pointer;
      pointer = undefined;
      if (element.hasPointerCapture(event.pointerId))
        element.releasePointerCapture(event.pointerId);
      const delta = tvCoordinates(
        event.clientX - current.x,
        event.clientY - current.y,
        latest.current.rotation,
      );
      if (current.moved) {
        event.preventDefault();
        if (Math.abs(delta.y) > 55)
          latest.current.dispatch({
            type: "navigate",
            direction: delta.y < 0 ? 1 : -1,
          });
        else latest.current.cancelDrag();
      }
    };
    const wheel = (event: WheelEvent) => {
      if (
        blockedNow() ||
        ignored(event.target) ||
        event.ctrlKey ||
        event.metaKey ||
        controls.read().zoomed ||
        event.defaultPrevented
      )
        return;
      const now = performance.now();
      if (now - wheelLast > 180) {
        wheelCommitted = false;
        wheelTotal = 0;
      }
      wheelLast = now;
      const delta = tvCoordinates(
        event.deltaX,
        event.deltaY,
        latest.current.rotation,
      );
      if (Math.abs(delta.y) < Math.abs(delta.x)) return;
      event.preventDefault();
      wheelTotal +=
        delta.y *
        (event.deltaMode === 1
          ? 16
          : event.deltaMode === 2
            ? element.clientHeight
            : 1);
      if (!wheelCommitted && Math.abs(wheelTotal) > 65) {
        wheelCommitted = true;
        latest.current.dispatch({
          type: "navigate",
          direction: wheelTotal > 0 ? 1 : -1,
        });
      }
    };
    const keyDown = (event: KeyboardEvent) => {
      if (
        event.defaultPrevented ||
        blockedNow() ||
        event.altKey ||
        event.ctrlKey ||
        event.metaKey ||
        event.shiftKey
      )
        return;
      const key = event.key.toLowerCase();
      if ((key === "arrowup" || key === "arrowdown") && hold.current?.started) {
        hold.current.speed = Math.max(
          0.25,
          Math.min(16, hold.current.speed * (key === "arrowup" ? 2 : 0.5)),
        );
        if (hold.current.direction === 1) controls.setRate(hold.current.speed);
        event.preventDefault();
        return;
      }
      if (ignored(event.target)) return;
      if (event.repeat) {
        if (key.startsWith("arrow")) event.preventDefault();
        return;
      }
      if (key === "arrowleft" || key === "arrowright")
        startHold(key === "arrowright" ? 1 : -1);
      else if (key === "arrowup" || key === "arrowdown")
        latest.current.dispatch({
          type: "navigate",
          direction: key === "arrowdown" ? 1 : -1,
        });
      else if (key === " ") controls.togglePaused();
      else if (key === "m") controls.toggleMuted();
      else if (key === "escape")
        latest.current.dispatch({ type: "exit-presentation" });
      else {
        const actions = {
          d: "delete",
          e: "tags",
          f: "fullscreen",
          i: "info",
          l: "completion",
          o: "rotation",
          s: "subtitles",
          h: "visibility",
        } as const;
        const matched = Object.entries(actions).find(
          ([shortcut]) => shortcut === key,
        );
        if (!matched) return;
        latest.current.dispatch({ type: "action", action: matched[1] });
      }
      event.preventDefault();
    };
    const keyUp = (event: KeyboardEvent) => {
      if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
      const active = hold.current;
      endHold();
      if (active && !active.started && !blockedNow())
        latest.current.dispatch({ type: "seek", direction: active.direction });
    };
    element.addEventListener("pointerdown", down);
    element.addEventListener("pointermove", move);
    element.addEventListener("pointerup", up);
    element.addEventListener("pointercancel", cancel);
    element.addEventListener("wheel", wheel, { passive: false });
    window.addEventListener("keydown", keyDown);
    window.addEventListener("keyup", keyUp);
    window.addEventListener("blur", cancel);
    document.addEventListener("visibilitychange", cancel);
    return () => {
      element.removeEventListener("pointerdown", down);
      element.removeEventListener("pointermove", move);
      element.removeEventListener("pointerup", up);
      element.removeEventListener("pointercancel", cancel);
      element.removeEventListener("wheel", wheel);
      window.removeEventListener("keydown", keyDown);
      window.removeEventListener("keyup", keyUp);
      window.removeEventListener("blur", cancel);
      document.removeEventListener("visibilitychange", cancel);
      cancel();
    };
  }, [surface, controls, overlaysBlocked, startHold, endHold]);
  return { startHold, endHold, seekClick };
}
