import { useEffect, useLayoutEffect, useState } from "react";
import { useApolloClient } from "@apollo/client/react";
import type { TvFeedItem } from "@/core/tv/feed-state";
import type { TvSettings } from "@/core/tv/settings";
import { TvPreparedItem } from "./tv-prepared-item";

export function tvMediaWindow(
  items: readonly TvFeedItem[],
  selected: number,
  count: TvSettings["preloadCount"],
) {
  const radius = (count - 1) / 2;
  return items.slice(Math.max(0, selected - radius), selected + radius + 1);
}

export function useTvMediaWindow({
  items,
  selected,
  settings,
  seed,
  identity,
  leaving,
  ready,
}: {
  items: readonly TvFeedItem[];
  selected: number;
  settings: TvSettings;
  seed: number;
  identity: string;
  leaving: boolean;
  ready: boolean;
}) {
  const client = useApolloClient();
  const desired = leaving
    ? []
    : tvMediaWindow(items, selected, settings.preloadCount);
  const configuration = JSON.stringify([
    identity,
    seed,
    settings.defaultQuality,
    settings.start,
    settings.window,
  ]);
  const keys = desired.map((item) => `${item.key}/${item.sceneId}`).join(",");
  const create = (previous: readonly TvPreparedItem[] = []) => ({
    configuration,
    keys,
    entries: desired.map(
      (item) =>
        previous.find(
          (entry) =>
            entry.item.key === item.key && entry.item.sceneId === item.sceneId,
        ) ?? new TvPreparedItem(item, client, settings, seed),
    ),
  });
  const [state, setWindow] = useState(create);
  if (state.configuration !== configuration || state.keys !== keys)
    setWindow(
      create(state.configuration === configuration ? state.entries : []),
    );
  const active = state.entries.find(
    (entry) => entry.item.key === items[selected]?.key,
  );

  useLayoutEffect(() => {
    const releases = state.entries.map((entry) => entry.retain());
    active?.activate();
    return () => {
      for (const release of releases) release();
    };
  }, [state.entries, active]);

  useEffect(() => {
    let hidden = false;
    const cancel = () => {
      for (const entry of state.entries) entry.cancelPreparation();
    };
    const prepare = () => {
      if (hidden || document.hidden || !ready) return;
      // The active item always has first use of the decoder/GPU. Only after
      // it is ready do neighbouring items start their bounded startup fetch.
      for (const entry of state.entries) if (entry !== active) entry.prepare();
    };
    const visibility = () => {
      if (document.hidden) cancel();
      else prepare();
    };
    const pagehide = () => {
      hidden = true;
      cancel();
    };
    const pageshow = () => {
      hidden = false;
      prepare();
    };
    prepare();
    document.addEventListener("visibilitychange", visibility);
    window.addEventListener("pagehide", pagehide);
    window.addEventListener("pageshow", pageshow);
    return () => {
      document.removeEventListener("visibilitychange", visibility);
      window.removeEventListener("pagehide", pagehide);
      window.removeEventListener("pageshow", pageshow);
    };
  }, [state.entries, active, ready]);
  return active?.session;
}
