import { useCallback, useEffect, useRef } from "react";

const LIGHTBOX_HISTORY_KEY = "stashV3Lightbox";

interface LightboxHistoryState {
  [LIGHTBOX_HISTORY_KEY]?: {
    id?: number;
  };
}

let nextLightboxHistoryId = 0;

function getLightboxHistoryId(state: unknown): number | undefined {
  if (!state || typeof state !== "object") return undefined;
  const lightboxState = (state as LightboxHistoryState)[LIGHTBOX_HISTORY_KEY];
  return typeof lightboxState?.id === "number" ? lightboxState.id : undefined;
}

/**
 * Adds one same-URL history entry while a lightbox is open. Browser Back and
 * lightbox dismissal both consume that entry, so Back closes the lightbox
 * instead of leaving the page and the close button does not leave a dead
 * history step behind.
 */
export function useLightboxHistory(open: boolean, onClose: () => void) {
  const activeIdRef = useRef<number | undefined>(undefined);
  const dismissingRef = useRef(false);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  useEffect(() => {
    if (!open) return;

    const id = ++nextLightboxHistoryId;
    activeIdRef.current = id;
    dismissingRef.current = false;
    const currentState =
      typeof window.history.state === "object" && window.history.state !== null
        ? window.history.state
        : {};
    window.history.pushState(
      { ...currentState, [LIGHTBOX_HISTORY_KEY]: { id } },
      "",
      window.location.href,
    );

    function handlePopState(event: PopStateEvent) {
      if (activeIdRef.current !== id) return;
      if (getLightboxHistoryId(event.state) === id) {
        dismissingRef.current = false;
        return;
      }

      activeIdRef.current = undefined;
      dismissingRef.current = false;
      onCloseRef.current();
    }

    window.addEventListener("popstate", handlePopState);
    return () => {
      window.removeEventListener("popstate", handlePopState);

      // Programmatic closure (for example deleting the final slide) can
      // unmount the lightbox without calling requestClose. Consume its entry
      // here as long as no navigation has already replaced it.
      if (
        activeIdRef.current === id &&
        getLightboxHistoryId(window.history.state) === id
      ) {
        activeIdRef.current = undefined;
        dismissingRef.current = true;
        window.history.back();
      }
    };
  }, [open]);

  return useCallback(() => {
    const id = activeIdRef.current;
    if (id !== undefined && getLightboxHistoryId(window.history.state) === id) {
      if (dismissingRef.current) return;
      dismissingRef.current = true;
      window.history.back();
      return;
    }

    activeIdRef.current = undefined;
    dismissingRef.current = false;
    onCloseRef.current();
  }, []);
}
