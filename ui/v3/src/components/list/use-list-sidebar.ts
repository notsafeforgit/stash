import React, { useCallback, useMemo, useState } from "react";
import { useLocation, useRouter } from "@tanstack/react-router";
import { useMediaQuery } from "src/utils/screen";
import type { View } from "src/components/list/views";
import {
  type ViewConfig,
  readInterfaceConfig,
  useInterfaceLocalForage,
} from "src/hooks/local-forage";

// ── SidebarStateContext ───────────────────────────────────────────────────────

type SidebarSectionStates = Record<string, boolean>;

interface SidebarStateContext {
  sectionOpen: SidebarSectionStates;
  setSectionOpen: (section: string, open: boolean) => void;
}

export const SidebarStateContext =
  React.createContext<SidebarStateContext | null>(null);

// ── Constants ─────────────────────────────────────────────────────────────────

const MOBILE_QUERY = "only screen and (max-width: 767px)";

function defaultShowSidebar() {
  return !window.matchMedia(MOBILE_QUERY).matches;
}

// ── useListSidebar ────────────────────────────────────────────────────────────

export function useListSidebar(view?: View) {
  const isMobileSidebar = useMediaQuery(MOBILE_QUERY);
  const router = useRouter();
  const location = useLocation();

  // ── localStorage: persist showSidebar per view ──────────────────────────────
  const [interfaceData, setInterfaceLocalForage] = useInterfaceLocalForage();

  const _viewConfig: ViewConfig = useMemo(
    () => (view ? (interfaceData?.viewConfig?.[view] ?? {}) : {}),
    [view, interfaceData],
  );

  const [showSidebar, setShowSidebarState] = useState<boolean>(() => {
    if (!view) return defaultShowSidebar();
    const stored = readInterfaceConfig();
    return !!stored?.viewConfig?.[view]?.showSidebar && defaultShowSidebar();
  });

  const setShowSidebar = useCallback(
    (show: boolean) => {
      setShowSidebarState(show);
      if (view === undefined) return;
      setInterfaceLocalForage((prev) => ({
        ...prev,
        viewConfig: {
          ...prev.viewConfig,
          [view]: { ...(prev.viewConfig?.[view] ?? {}), showSidebar: show },
        },
      }));
    },
    [view, setInterfaceLocalForage],
  );

  // ── Router state: persist sectionOpen ─────────────────────────────────────
  const locationState = location.state as
    | { sectionOpen?: SidebarSectionStates }
    | undefined;
  const [sectionOpen, setSectionOpenState] = useState<SidebarSectionStates>(
    locationState?.sectionOpen ?? {},
  );

  const setSectionOpen = useCallback(
    (section: string, open: boolean) => {
      const next = { ...sectionOpen, [section]: open };
      setSectionOpenState(next);
      if (view === undefined) return;
      router.history.replace(router.history.location.href, {
        ...router.history.location.state,
        sectionOpen: next,
      });
    },
    [sectionOpen, view, router],
  );

  // ── Open / close ──────────────────────────────────────────────────────────
  // Both BottomSheet (mobile) and RightDrawer (desktop) are Vaul-backed and
  // manage their own close animations, so we just toggle the state directly.

  const closeFilterSidebar = useCallback(() => {
    setShowSidebar(false);
  }, [setShowSidebar]);

  const openFilterSidebar = useCallback(() => {
    setShowSidebar(true);
  }, [setShowSidebar]);

  return {
    showSidebar,
    sectionOpen,
    setSectionOpen,
    isMobileSidebar,
    closeFilterSidebar,
    openFilterSidebar,
  };
}

export type IListSidebarState = ReturnType<typeof useListSidebar>;
