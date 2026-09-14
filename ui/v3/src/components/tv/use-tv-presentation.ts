import { useCallback, useEffect, useRef, useState } from "react";
import type { TvRotation } from "@/core/tv/settings";
import { presentationCoordinates } from "@/core/presentation-coordinates";

export function tvCoordinates(
  x: number,
  y: number,
  rotation: TvRotation,
): { x: number; y: number } {
  return presentationCoordinates(
    x,
    y,
    rotation === "clockwise" ? 90 : rotation === "counterclockwise" ? -90 : 0,
  );
}

export function useTvPresentation() {
  const root = useRef<HTMLDivElement>(null);
  const surface = useRef<HTMLDivElement>(null);
  const portals = useRef<HTMLDivElement>(null);
  const [mode, setMode] = useState<"normal" | "fullscreen">("normal");
  const [rejected, setRejected] = useState(false);
  const canFullscreen =
    !rejected &&
    document.fullscreenEnabled === true &&
    typeof document.documentElement.requestFullscreen === "function";
  const requestGeneration = useRef(0);
  const exit = useCallback(async () => {
    requestGeneration.current++;
    if (document.fullscreenElement === root.current)
      await document.exitFullscreen().catch(() => {});
    setMode("normal");
  }, []);
  const toggle = useCallback((): true => {
    if (mode !== "normal") {
      void exit();
      return true;
    }
    const element = root.current;
    const generation = ++requestGeneration.current;
    if (
      element &&
      canFullscreen &&
      typeof element.requestFullscreen === "function"
    ) {
      void element
        .requestFullscreen()
        .then(() => {
          if (
            generation !== requestGeneration.current &&
            document.fullscreenElement === element
          )
            return document.exitFullscreen();
        })
        .catch(() => {
          if (generation === requestGeneration.current) setRejected(true);
        });
    }
    return true;
  }, [mode, exit, canFullscreen]);
  useEffect(() => {
    const element = root.current;
    const change = () =>
      setMode(document.fullscreenElement === element ? "fullscreen" : "normal");
    document.addEventListener("fullscreenchange", change);
    return () => {
      requestGeneration.current++;
      document.removeEventListener("fullscreenchange", change);
      if (document.fullscreenElement === element)
        void document.exitFullscreen().catch(() => {});
    };
  }, []);
  // A transformed presentation establishes the fixed containing block for its
  // portals. Size from this viewport only; never rotate body or patch events.
  useEffect(() => {
    const element = root.current;
    if (!element) return;
    const update = () => {
      element.style.setProperty("--tv-width", `${element.clientWidth}px`);
      element.style.setProperty("--tv-height", `${element.clientHeight}px`);
    };
    const observer = new ResizeObserver(update);
    observer.observe(element);
    update();
    return () => observer.disconnect();
  }, []);
  return { root, surface, portals, mode, toggle, exit, canFullscreen };
}
