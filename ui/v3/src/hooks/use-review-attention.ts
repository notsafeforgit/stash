import { useEffect, useSyncExternalStore } from "react";
import {
  createReviewQueueAPI,
  type ReviewQueueAPI,
} from "@/core/native-archive/review-queue-api";
import { useSourceTracking } from "@/hooks/use-source-tracking";

// The app shell owns one poller (useReviewAttentionPolling) and every badge
// reads its count. Isolated component views render badges without polling.
// The summary is a cheap count of new decisions; failures keep the last known
// count rather than hiding a badge.
const pollInterval = 5 * 60_000;
const focusThrottle = 30_000;

let api: ReviewQueueAPI | undefined;
let sources = 0;
let users = 0;
let lastRequest = 0;
let controller: AbortController | undefined;
let timer: ReturnType<typeof setInterval> | undefined;
const listeners = new Set<() => void>();

function publish(value: number) {
  if (value === sources) return;
  sources = value;
  for (const listener of listeners) listener();
}

/** Re-reads the count now, e.g. after enabling or dismissing a held source. */
export function refreshReviewAttention() {
  if (users === 0) return;
  controller?.abort();
  const current = new AbortController();
  controller = current;
  lastRequest = Date.now();
  api ??= createReviewQueueAPI();
  api.summary(current.signal).then(
    (summary) => {
      if (controller === current) publish(summary.sources);
    },
    () => {},
  );
}

function onVisible() {
  if (
    document.visibilityState === "visible" &&
    Date.now() - lastRequest >= focusThrottle
  )
    refreshReviewAttention();
}

function start() {
  users += 1;
  if (users > 1) return;
  refreshReviewAttention();
  timer = setInterval(() => {
    if (document.visibilityState === "visible") refreshReviewAttention();
  }, pollInterval);
  document.addEventListener("visibilitychange", onVisible);
  window.addEventListener("focus", onVisible);
}

function stop() {
  users -= 1;
  if (users > 0) return;
  clearInterval(timer);
  timer = undefined;
  controller?.abort();
  controller = undefined;
  document.removeEventListener("visibilitychange", onVisible);
  window.removeEventListener("focus", onVisible);
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** Polls the review summary while source tracking is active. */
export function useReviewAttentionPolling() {
  const enabled = useSourceTracking();
  useEffect(() => {
    if (!enabled) return;
    start();
    return stop;
  }, [enabled]);
}

/** Held sources awaiting a decision; zero while source tracking is off. */
export function useReviewAttention() {
  const enabled = useSourceTracking();
  const count = useSyncExternalStore(
    subscribe,
    () => sources,
    () => 0,
  );
  return enabled ? count : 0;
}
