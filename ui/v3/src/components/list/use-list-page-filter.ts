import { useState, useEffect, useRef, useMemo, useCallback } from "react";
import type { FilterMode } from "@/core/generated-graphql";
import { ListFilterModel } from "@/models/list-filter/filter";
import type { DisplayMode } from "@/models/list-filter/types";
import type { CardAspect } from "./card-aspect-context";
import type { View } from "./views";
import { useFilterState } from "./use-filter-state";

function useCardAspectPref(
  filterMode: string,
): [CardAspect, (a: CardAspect) => void] {
  const key = `list-card-aspect:${filterMode}`;
  const [aspect, setAspect] = useState<CardAspect>(() => {
    try {
      const raw = window.localStorage.getItem(key);
      if (raw === "portrait" || raw === "landscape" || raw === "auto")
        return raw;
    } catch {
      // ignore
    }
    return "auto";
  });

  useEffect(() => {
    try {
      window.localStorage.setItem(key, aspect);
    } catch {
      // ignore
    }
  }, [key, aspect]);

  return [aspect, setAspect];
}

function useMobileGridColumns(
  filterMode: string,
): [1 | 2, (cols: 1 | 2) => void] {
  const key = `list-mobile-grid-cols:${filterMode}`;
  const [cols, setCols] = useState<1 | 2>(() => {
    try {
      const raw = window.localStorage.getItem(key);
      return raw === "1" ? 1 : 2;
    } catch {
      return 2;
    }
  });

  useEffect(() => {
    try {
      window.localStorage.setItem(key, String(cols));
    } catch {
      // ignore
    }
  }, [key, cols]);

  return [cols, setCols];
}

// Per-view localStorage so each context (root scenes, performer
// scenes, tag scenes, …) tracks its own preference. The legacy
// per-filterMode keys are intentionally not migrated — at worst the
// user re-picks once.
function useZoomPref(scope: string): [number, (z: number) => void] {
  const key = `list-zoom:${scope}`;
  const [zoom, setZoomState] = useState<number>(() => {
    try {
      const raw = window.localStorage.getItem(key);
      if (raw !== null) {
        const parsed = parseInt(raw, 10);
        if (!Number.isNaN(parsed)) return Math.max(0, Math.min(4, parsed));
      }
    } catch {
      // ignore
    }
    return 1;
  });

  const setZoom = useCallback(
    (z: number) => {
      setZoomState(z);
      try {
        window.localStorage.setItem(key, String(z));
      } catch {
        // ignore
      }
    },
    [key],
  );

  return [zoom, setZoom];
}

// Per-view localStorage — see `useZoomPref` above.
function useDisplayModePref(
  scope: string,
  options: DisplayMode[],
): [DisplayMode, (m: DisplayMode) => void] {
  const key = `list-display-mode:${scope}`;
  const [mode, setModeState] = useState<DisplayMode>(() => {
    try {
      const raw = window.localStorage.getItem(key);
      if (raw !== null) {
        const parsed = parseInt(raw, 10) as DisplayMode;
        if (options.includes(parsed)) return parsed;
      }
    } catch {
      // ignore
    }
    const first = options[0];
    if (first === undefined)
      throw new Error("A list requires at least one display mode");
    return first;
  });

  const setMode = useCallback(
    (m: DisplayMode) => {
      setModeState(m);
      try {
        window.localStorage.setItem(key, String(m));
      } catch {
        // ignore
      }
    },
    [key],
  );

  return [mode, setMode];
}

export function useListPageFilter({
  filterMode,
  view,
  defaultSort,
  useURL,
  isActive,
  defaultFilter,
}: {
  filterMode: FilterMode;
  view?: View;
  defaultSort?: string;
  useURL?: boolean;
  isActive: boolean;
  defaultFilter?: ListFilterModel;
}) {
  // Display mode + zoom are UI preferences — not part of the URL or
  // filter predicate. Scoped per `view` (root-scenes vs
  // performer-scenes vs tag-scenes …) so each context can keep its own
  // layout. Falls back to `filterMode` when no view is supplied.
  const prefScope = view ?? filterMode;
  const emptyFilterForOptions = new ListFilterModel(filterMode);
  const [displayModePref, setDisplayModePref] = useDisplayModePref(
    prefScope,
    emptyFilterForOptions.options.displayModeOptions,
  );

  const [zoomPref, setZoomPref] = useZoomPref(prefScope);
  // `zoomPref` is React state, so it lags behind by a render. Rapid
  // clicks fire before the previous render has committed, so each
  // handler reads the same (stale) state and computes the same target.
  // `intendedZoomRef` mirrors the latest *intended* zoom, updated
  // synchronously inside `setFilter`, so back-to-back clicks compound
  // correctly even when React batches the updates.
  const intendedZoomRef = useRef(zoomPref);
  useEffect(() => {
    intendedZoomRef.current = zoomPref;
  }, [zoomPref]);

  const { filter: rawFilter, setFilter: rawSetFilter } = useFilterState({
    filterMode,
    view,
    defaultSort,
    // DetailTabs keeps visited panels mounted. Only the visible panel may
    // consume or rewrite the shared list params (`p`, `sortby`, etc.);
    // otherwise an inactive one-page list can clamp the active tab's `p=2`
    // straight back to page 1.
    useURL: (useURL ?? true) && isActive,
    defaultFilter,
    defaultDisplayMode: displayModePref,
  });

  // Apply display mode and zoom prefs into the live filter without going through URL.
  // Must be memoized — creating a new object every render would make useDebouncedValue
  // never settle, keeping isLoading permanently true.
  const filter = useMemo(() => {
    let f =
      rawFilter.displayMode === displayModePref
        ? rawFilter
        : rawFilter.setDisplayMode(displayModePref);
    if (f.zoomIndex !== zoomPref) {
      f = f.setZoom(zoomPref);
    }
    return f;
  }, [rawFilter, displayModePref, zoomPref]);

  // Wrap setFilter to intercept display mode / zoom changes and persist them.
  // Use `filter` (the memoized value with prefs applied) as the base for
  // functional updaters — callers always have `filter` in scope, not rawFilter.
  // Only call rawSetFilter when URL-relevant parameters actually change; zoom
  // and displayMode are UI-only prefs that don't belong in the URL, and routing
  // through rawSetFilter for those would trigger router.history.replace
  // unnecessarily, adding latency to instant UI actions like zoom.
  // EntityList reveals the committed layout through its empty paint surface;
  // preference updates stay synchronous and never capture page snapshots.
  const setFilter = useCallback(
    (f: ListFilterModel | ((prev: ListFilterModel) => ListFilterModel)) => {
      // For functional updaters: substitute the latest intended zoom into
      // `prev` so callers like the toolbar's zoomIn/zoomOut compute their
      // target from the most recent click rather than the stale render.
      const baseFilter =
        filter.zoomIndex !== intendedZoomRef.current
          ? filter.setZoom(intendedZoomRef.current)
          : filter;
      const next = typeof f === "function" ? f(baseFilter) : f;
      if (next.displayMode !== displayModePref) {
        setDisplayModePref(next.displayMode);
      }
      const zoomChanged = next.zoomIndex !== intendedZoomRef.current;
      const urlDiffers =
        next.makeQueryParameters() !== rawFilter.makeQueryParameters();
      const applyNonZoom = () => {
        if (urlDiffers) rawSetFilter(next);
      };

      if (zoomChanged) {
        // Update intended zoom synchronously so rapid follow-up clicks
        // see the new target before React commits.
        intendedZoomRef.current = next.zoomIndex;
        setZoomPref(next.zoomIndex);
      }
      applyNonZoom();
    },
    [
      rawFilter,
      filter,
      rawSetFilter,
      displayModePref,
      setDisplayModePref,
      setZoomPref,
    ],
  );

  const [mobileGridCols, setMobileGridCols] = useMobileGridColumns(filterMode);
  const [cardAspect, setCardAspect] = useCardAspectPref(filterMode);
  return {
    filter,
    setFilter,
    mobileGridCols,
    setMobileGridCols,
    cardAspect,
    setCardAspect,
  };
}
