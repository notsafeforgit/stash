import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useRouter } from "@tanstack/react-router";
import { ListFilterModel } from "src/models/list-filter/filter";
import {
  cloneFilterASTNode,
  type FilterASTGroupNode,
} from "src/models/list-filter/filter-ast";
import type { DisplayMode } from "src/models/list-filter/types";
import type { View } from "src/components/list/views";
import { usePrevious } from "src/hooks/state";
import type * as GQL from "src/core/generated-graphql";
import { useConfigurationContextOptional } from "src/hooks/config";

// ── Helpers ────────────────────────────────────────────────────────────────

function locationEquals(
  prev: { pathname: string; searchStr: string } | undefined,
  curr: { pathname: string; searchStr: string },
) {
  return (
    prev !== undefined &&
    prev.pathname === curr.pathname &&
    prev.searchStr === curr.searchStr
  );
}

function hasSearchParams(searchStr: string) {
  return searchStr.length > 0 && searchStr !== "?";
}

// The set of URL params managed exclusively by the filter. Non-filter params
// (e.g. `tab`) are preserved when updating the URL so they survive filter changes.
const FILTER_PARAMS = [
  "q",
  "c",
  "fa",
  "sortby",
  "sortdir",
  "perPage",
  "p",
] as const;

/**
 * Merge new filter params into the current URL search string, preserving any
 * non-filter params (e.g. `tab`). Old filter params are always removed so
 * stale values don't accumulate.
 */
function mergeFilterParams(
  currentSearchStr: string,
  newFilterParams: string,
): string {
  const current = new URLSearchParams(currentSearchStr);
  for (const key of FILTER_PARAMS) {
    current.delete(key);
  }
  if (newFilterParams) {
    const incoming = new URLSearchParams(newFilterParams);
    for (const [key, value] of incoming.entries()) {
      current.append(key, value);
    }
  }
  return current.toString();
}

// ── useDefaultFilter ────────────────────────────────────────────────────────

/**
 * Returns the default filter for a given view, with saved settings (sort, page
 * size, criteria) applied on top of `baseFilter`. When no saved default exists
 * the base filter is returned unchanged.
 *
 * Pass `propDefaultFilter ?? emptyFilter` as `baseFilter` so that embedded tabs
 * (which carry a locked criterion in `lockedFilterAst`) preserve that lock when
 * a saved default is loaded.
 */
function useDefaultFilter(baseFilter: ListFilterModel, view?: View) {
  const ctx = useConfigurationContextOptional();
  const defaultFilters = ctx?.configuration?.ui?.defaultFilters;

  const defaultFilter = useMemo(() => {
    if (view && defaultFilters?.[view]) {
      const savedFilter = defaultFilters[view]!;
      const newFilter = baseFilter.clone();
      newFilter.currentPage = 1;
      try {
        newFilter.configureFromSavedFilter(savedFilter);
      } catch (err) {
        console.log(err);
      }
      newFilter.randomSeed = -1;
      return newFilter;
    }
  }, [view, defaultFilters, baseFilter]);

  return defaultFilter ?? baseFilter;
}

// ── useFilterURL ────────────────────────────────────────────────────────────

function useFilterURL(
  filter: ListFilterModel,
  setFilterState: React.Dispatch<React.SetStateAction<ListFilterModel>>,
  options?: {
    defaultFilter?: ListFilterModel;
    active?: boolean;
  },
) {
  const { defaultFilter, active = true } = options ?? {};

  const router = useRouter();
  const location = useLocation();
  const prevLocation = usePrevious(location);

  // Capture the pathname on mount so we can ignore location changes that
  // are actually navigations away from this list page (e.g. clicking a card
  // navigates to /scenes/123 — we must not react to that location change or
  // we'll call router.history.replace on the detail-page URL, wiping state).
  const homePathnameRef = useRef(location.pathname);

  // Keep a ref to the current filter so updateFilter and the sync effect can
  // read the latest value without listing `filter` as a dependency (which
  // would cause infinite re-render loops when the effect sets the filter).
  const filterRef = useRef(filter);
  filterRef.current = filter;

  const updateFilter = useCallback(
    (value: ListFilterModel | ((prev: ListFilterModel) => ListFilterModel)) => {
      const prevFilter = filterRef.current;
      const newFilter = typeof value === "function" ? value(prevFilter) : value;
      setFilterState(newFilter);
      if (active) {
        const newParams = newFilter.makeQueryParameters();
        const merged = mergeFilterParams(location.searchStr, newParams);
        const hash = location.hash ? `#${location.hash}` : "";
        const newHref = merged
          ? `${location.pathname}?${merged}${hash}`
          : `${location.pathname}${hash}`;
        // Pagination is a discrete user action ("Next page" click), so push
        // a new history entry so the back button can return to the prior
        // page. Filter / search / sort edits stay on `replace` because they
        // commonly fire many times per interaction (typing in a search box,
        // dragging a slider) and would otherwise spam the history stack.
        const stripPageParam = (qp: string) =>
          qp
            .split("&")
            .filter((p) => p && !p.startsWith("p="))
            .join("&");
        const isPageOnlyChange =
          newFilter.currentPage !== prevFilter.currentPage &&
          stripPageParam(newFilter.makeQueryParameters()) ===
            stripPageParam(prevFilter.makeQueryParameters());
        if (isPageOnlyChange) {
          router.history.push(newHref);
        } else {
          router.history.replace(newHref);
        }
      }
    },
    [router, active, setFilterState, location],
  );

  // Sync filter when URL changes externally (back/forward navigation)
  useEffect(() => {
    if (!active || locationEquals(prevLocation, location)) return;
    // If the pathname changed away from our list page (e.g. navigating to a
    // detail page), do nothing — this component is about to unmount and any
    // router.history.replace call here would corrupt the detail page's URL/state.
    if (location.pathname !== homePathnameRef.current) return;
    // On first mount prevLocation is undefined — initialFilter already applied
    // the URL params. Only normalize the URL if the filter dropped invalid
    // params (e.g. a sort key not supported by this filter mode).
    if (prevLocation === undefined) {
      if (hasSearchParams(location.searchStr)) {
        const currentParams = filterRef.current.makeQueryParameters();
        const merged = mergeFilterParams(location.searchStr, currentParams);
        const expectedSearchStr = merged ? `?${merged}` : "";
        if (expectedSearchStr !== location.searchStr) {
          const hash = location.hash ? `#${location.hash}` : "";
          router.history.replace(
            merged
              ? `${location.pathname}?${merged}${hash}`
              : `${location.pathname}${hash}`,
          );
        }
      }
      return;
    }

    // No search params → reset to default filter
    if (!hasSearchParams(location.searchStr)) {
      if (defaultFilter) updateFilter(defaultFilter.clone());
      return;
    }

    // Compute the new filter via the live ref rather than passing an
    // updater function to setFilterState. Updater functions run inside
    // React's render phase when the queued update is processed; calling
    // `router.history.replace(...)` from there triggers TanStack Router's
    // Transitioner setState mid-render and produces the
    //   "Cannot update a component (Transitioner) while rendering ..."
    // warning. Doing the URL normalisation here (effect body) keeps the
    // setState side effect outside any other component's render.
    const prevFilter = filterRef.current;
    const newFilter = prevFilter.empty();
    // `empty()` drops the locked filter (it's part of page identity, not URL
    // state — embedded list pages like /performers/$id set it once via
    // defaultFilter and it never round-trips through query params). Carry it
    // forward here so this rebuild path doesn't strip it.
    if (prevFilter.lockedFilterAst) {
      newFilter.lockedFilterAst = cloneFilterASTNode(
        prevFilter.lockedFilterAst,
      ) as FilterASTGroupNode;
    }
    newFilter.configureFromQueryString(location.searchStr);

    if (newFilter.makeQueryParameters() === prevFilter.makeQueryParameters()) {
      return;
    }

    // If random seed caused params to differ, reflect that in URL.
    const newParams = newFilter.makeQueryParameters();
    const merged = mergeFilterParams(location.searchStr, newParams);
    const expectedSearchStr = merged ? `?${merged}` : "";
    if (expectedSearchStr !== location.searchStr) {
      const hash = location.hash ? `#${location.hash}` : "";
      router.history.replace(
        merged
          ? `${location.pathname}?${merged}${hash}`
          : `${location.pathname}${hash}`,
      );
    }
    setFilterState(newFilter);
  }, [
    active,
    prevLocation,
    location,
    defaultFilter,
    setFilterState,
    updateFilter,
    router,
  ]);

  return { setFilter: updateFilter };
}

// ── useFilterState ──────────────────────────────────────────────────────────

export interface IFilterStateHook {
  filterMode: GQL.FilterMode;
  defaultSort?: string;
  view?: View;
  /** Whether to sync filter state to the URL (default: true). */
  useURL?: boolean;
  defaultFilter?: ListFilterModel;
  /** Override the initial display mode (e.g. from a localStorage preference). */
  defaultDisplayMode?: DisplayMode;
}

export function useFilterState(props: IFilterStateHook) {
  const {
    filterMode,
    defaultSort,
    view,
    useURL,
    defaultFilter: propDefaultFilter,
    defaultDisplayMode,
  } = props;
  const useURLActive = useURL ?? true;

  const location = useLocation();

  const ctx = useConfigurationContextOptional();
  const config = ctx?.configuration;

  const emptyFilter = useMemo(
    () =>
      new ListFilterModel(filterMode, config, {
        defaultSortBy: defaultSort,
      }),
    [config, filterMode, defaultSort],
  );

  // Use propDefaultFilter (which may carry a locked criterion) as the base
  // so that saved default settings are applied on top of the lock, not on a
  // blank filter that loses the lock.
  const defaultFilterFromConfig = useDefaultFilter(
    propDefaultFilter ?? emptyFilter,
    view,
  );

  // Compute the initial filter exactly once on mount. Using a ref rather than
  // useMemo ensures this is not recomputed when config loads asynchronously —
  // we don't want config loading to blow away a URL-derived filter the user
  // already has (useState ignores subsequent initialState values anyway).
  const initialFilterRef = useRef<ListFilterModel | null>(null);
  if (initialFilterRef.current === null) {
    const base = defaultFilterFromConfig.clone();
    if (useURLActive && hasSearchParams(location.searchStr)) {
      base.configureFromQueryString(location.searchStr);
    }
    // Apply display mode preference (localStorage) — takes effect only when
    // the URL doesn't override it (URL no longer carries disp, so always applies).
    if (defaultDisplayMode !== undefined) {
      base.displayMode = defaultDisplayMode;
    }
    initialFilterRef.current = base;
  }
  const initialFilter = initialFilterRef.current;

  const [filter, setFilterState] = useState<ListFilterModel>(initialFilter);

  const { setFilter } = useFilterURL(filter, setFilterState, {
    defaultFilter: defaultFilterFromConfig,
    active: useURLActive,
  });

  return { filter, setFilter };
}
