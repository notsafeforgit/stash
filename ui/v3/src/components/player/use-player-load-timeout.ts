import { useEffect } from "react";
import { useCommittedRef } from "@/hooks/use-committed-ref";

// Cold transcodes need more time than the playback stall watchdog. A source
// that never becomes ready must still leave loading through recovery or Retry.
export const PLAYER_LOAD_TIMEOUT_MS = 30_000;

export function usePlayerLoadTimeout({
  load,
  pending,
  onTimeout,
}: {
  load: object;
  pending: boolean;
  onTimeout: () => void;
}) {
  const onTimeoutRef = useCommittedRef(onTimeout);
  // biome-ignore lint/correctness/useExhaustiveDependencies: Each source load receives its own deadline, including consecutive pending loads.
  useEffect(() => {
    if (!pending) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const schedule = () => {
      clearTimeout(timer);
      if (!document.hidden)
        timer = setTimeout(
          () => onTimeoutRef.current(),
          PLAYER_LOAD_TIMEOUT_MS,
        );
    };
    schedule();
    document.addEventListener("visibilitychange", schedule);
    return () => {
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", schedule);
    };
  }, [load, pending, onTimeoutRef]);
}
