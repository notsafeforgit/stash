import { useEffect, useState } from "react";

/** Refresh visible activity after the preceding read finishes, including an
 * empty queue that can receive new work. Errors wait for an explicit retry. */
export function useActivityPolling(
  enabled: boolean,
  busy: boolean,
  failed: boolean,
  refresh: () => void,
) {
  const [visible, setVisible] = useState(() => !document.hidden);
  useEffect(() => {
    if (!enabled) return;
    const changed = () => {
      setVisible(!document.hidden);
      if (!document.hidden && !busy && !failed) refresh();
    };
    document.addEventListener("visibilitychange", changed);
    return () => document.removeEventListener("visibilitychange", changed);
  }, [enabled, busy, failed, refresh]);
  useEffect(() => {
    if (!enabled || !visible || busy || failed) return;
    const timer = setTimeout(refresh, 10_000);
    return () => clearTimeout(timer);
  }, [enabled, visible, busy, failed, refresh]);
}
