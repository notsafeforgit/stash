import { useCallback, useEffect, useState } from "react";
import type {
  DownloadAPI,
  DownloadStatus,
} from "@/core/native-archive/download-api";

/** One request for up to 25 distinct attachments, only while its group is visible.
 * Never overlap polls or retain results across a changed endpoint/attachment set. */
export function useDownloadStatus(
  api: DownloadAPI,
  ids: string,
  active: boolean,
) {
  const [version, setVersion] = useState(0);
  const [state, setState] = useState<{
    key: string;
    data?: DownloadStatus;
    error?: unknown;
    busy: boolean;
  }>();
  const key = `${api.endpoint}:${ids}`;
  const reload = useCallback(() => setVersion((value) => value + 1), []);
  // biome-ignore lint/correctness/useExhaustiveDependencies: Version is the explicit refresh signal.
  useEffect(() => {
    if (!active || !ids) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    async function read() {
      setState((old) => ({
        key,
        data: old?.key === key ? old.data : undefined,
        busy: true,
      }));
      try {
        const data = await api.status(ids.split(","), controller.signal);
        if (!controller.signal.aborted) setState({ key, data, busy: false });
      } catch (error) {
        if (!controller.signal.aborted)
          setState((old) => ({
            key,
            data: old?.key === key ? old.data : undefined,
            error,
            busy: false,
          }));
      } finally {
        if (!controller.signal.aborted)
          timer = setTimeout(() => void read(), 15000);
      }
    }
    void read();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [api, key, ids, active, version]);
  return { ...(state?.key === key ? state : undefined), reload };
}

export function useVisibleDownloadGroup() {
  const [node, setNode] = useState<HTMLDivElement | null>(null);
  const [intersecting, setIntersecting] = useState(false);
  const [visible, setVisible] = useState(() => !document.hidden);
  useEffect(() => {
    const changed = () => setVisible(!document.hidden);
    document.addEventListener("visibilitychange", changed);
    return () => document.removeEventListener("visibilitychange", changed);
  }, []);
  useEffect(() => {
    if (!node) return;
    const observer = new IntersectionObserver(([entry]) =>
      setIntersecting(Boolean(entry?.isIntersecting)),
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [node]);
  return { setNode, active: visible && intersecting };
}
