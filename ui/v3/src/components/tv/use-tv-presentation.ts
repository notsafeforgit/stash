import { useCallback, useEffect, useRef, useState } from "react";

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
  return { root, surface, portals, mode, toggle, exit, canFullscreen };
}
