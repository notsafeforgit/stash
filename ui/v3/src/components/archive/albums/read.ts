import { useCallback, useEffect, useRef, useState } from "react";
import { NativeArchiveError } from "@/core/native-archive/client";

export type AlbumReadPage<T, C, H> = {
  signature: string;
  items: T[];
  next: C | null;
  header: H;
};

export function useAlbumPages<T, C, H>(
  load: (
    cursor: C | undefined,
    signal: AbortSignal,
  ) => Promise<AlbumReadPage<T, C, H>>,
) {
  const [data, setData] = useState<AlbumReadPage<T, C, H>>();
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState(true);
  const [version, setVersion] = useState(0);
  const pending = useRef<AbortController | null>(null);
  const reload = useCallback(() => setVersion((value) => value + 1), []);
  // biome-ignore lint/correctness/useExhaustiveDependencies: A reload replaces all pages with one current read.
  useEffect(() => {
    const controller = new AbortController();
    setBusy(true);
    setData(undefined);
    setError(undefined);
    void load(undefined, controller.signal)
      .then((page) => {
        if (!controller.signal.aborted) setData(page);
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setError(error);
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => {
      controller.abort();
      pending.current?.abort();
      pending.current = null;
    };
  }, [load, version]);
  async function more() {
    if (!data || data.next === null || busy || pending.current) return;
    const controller = new AbortController();
    pending.current = controller;
    setBusy(true);
    setError(undefined);
    try {
      const page = await load(data.next, controller.signal);
      if (controller.signal.aborted) return;
      if (page.signature !== data.signature)
        throw new NativeArchiveError(409, "album_changed");
      setData({ ...page, items: [...data.items, ...page.items] });
    } catch (error) {
      if (!controller.signal.aborted) setError(error);
    } finally {
      if (!controller.signal.aborted) setBusy(false);
      if (pending.current === controller) pending.current = null;
    }
  }
  return {
    data,
    busy,
    error,
    more,
    reload,
  };
}
