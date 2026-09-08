import React, { useCallback, useState } from "react";
import { useLocation, useRouter } from "@tanstack/react-router";
import { useMediaQuery } from "src/utils/screen";
import type { View } from "src/components/list/views";
import { useInterfacePreferences } from "@/hooks/interface-preferences";

// ── SidebarStateContext ───────────────────────────────────────────────────────

type SidebarSectionStates = Record<string, boolean>;
declare module "@tanstack/react-router" {
  interface HistoryState {
    sectionOpen?: SidebarSectionStates;
  }
}

interface SidebarStateContext {
  sectionOpen: SidebarSectionStates;
  setSectionOpen: (section: string, open: boolean) => void;
}

export const SidebarStateContext =
  React.createContext<SidebarStateContext | null>(null);

// ── Constants ─────────────────────────────────────────────────────────────────

const MOBILE_QUERY = "only screen and (max-width: 767px)";

// ── useListSidebar ────────────────────────────────────────────────────────────

export function useListSidebar(view?: View) {
  const isMobileSidebar = useMediaQuery(MOBILE_QUERY);
  const router = useRouter();
  const location = useLocation();

  // ── localStorage: persist showSidebar per view ──────────────────────────────
  const [interfaceData, setInterfaceData] = useInterfacePreferences();
  // Mobile sheets start closed and opening them is transient. Desktop visibility
  // follows the shared preference, including changes from other mounted views.
  const [transientOpen, setTransientOpen] = useState(!view && !isMobileSidebar);
  const showSidebar =
    isMobileSidebar || !view
      ? transientOpen
      : (interfaceData.viewConfig[view]?.showSidebar ?? false);

  const setShowSidebar = useCallback(
    (show: boolean) => {
      if (isMobileSidebar || !view) {
        setTransientOpen(show);
        return;
      }
      setInterfaceData((prev) => ({
        ...prev,
        viewConfig: {
          ...prev.viewConfig,
          [view]: { ...prev.viewConfig[view], showSidebar: show },
        },
      }));
    },
    [isMobileSidebar, view, setInterfaceData],
  );

  // ── Router state: persist sectionOpen ─────────────────────────────────────
  const locationState = location.state;
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
