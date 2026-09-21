import type { ApolloClient } from "@apollo/client";
import type { Client as WSClient } from "graphql-ws";
import {
  JobQueueDocument,
  JobsLifecycleSubscribeDocument,
  JobsProgressSubscribeDocument,
  JobStatus,
  JobStatusUpdateType,
  type JobsLifecycleSubscribeSubscription,
} from "./generated-graphql";

type JobEvent = JobsLifecycleSubscribeSubscription["jobsLifecycleSubscribe"];
export type QueueJob = JobEvent["job"];
type Entry = { job: QueueJob; removeAt?: number };
type ReceivedEvent = { event: JobEvent; receivedAt: number };

const finished = (job: QueueJob) =>
  job.status === JobStatus.Finished ||
  job.status === JobStatus.Failed ||
  job.status === JobStatus.Cancelled;

/** Live events provide progress; HTTP snapshots repair missed lifecycle events.
 * This observer belongs to the mounted queue, including all listeners/timers. */
export function observeJobQueue(
  client: ApolloClient,
  wsClient: Pick<WSClient, "terminate"> & {
    on(event: "connected", listener: () => void): () => void;
  },
  onChange: (jobs: QueueJob[]) => void,
): () => void {
  let disposed = false;
  let entries = new Map<string, Entry>();
  let expiryTimer: ReturnType<typeof setTimeout> | undefined;
  let refreshTimer: ReturnType<typeof setTimeout> | undefined;
  let restartPending = false;
  let request:
    | {
        controller: AbortController;
        timeout: ReturnType<typeof setTimeout>;
        events: Map<string, ReceivedEvent>;
      }
    | undefined;

  function publish() {
    if (disposed) return;
    clearTimeout(expiryTimer);
    const now = Date.now();
    let nextExpiry = Infinity;
    for (const [id, entry] of entries) {
      if (entry.removeAt === undefined) continue;
      if (entry.removeAt <= now) entries.delete(id);
      else nextExpiry = Math.min(nextExpiry, entry.removeAt);
    }
    onChange(Array.from(entries.values(), ({ job }) => job));
    if (Number.isFinite(nextExpiry))
      expiryTimer = setTimeout(publish, nextExpiry - now);
  }

  function apply({ event, receivedAt }: ReceivedEvent) {
    const previous = entries.get(event.job.id);
    if (event.type === JobStatusUpdateType.Update) {
      // The independent progress stream can deliver a late tick after REMOVE.
      if (!previous || finished(previous.job)) return;
    }
    entries.set(event.job.id, {
      job: event.job,
      removeAt: finished(event.job)
        ? (previous?.removeAt ?? receivedAt + 10_000)
        : undefined,
    });
  }

  function receive(event: JobEvent | undefined) {
    if (disposed || !event) return;
    const update = { event, receivedAt: Date.now() };
    if (request) {
      const previous = request.events.get(event.job.id);
      // Keep at most one event per job while HTTP is in flight. Preserve ADD
      // through subsequent progress, and never overwrite REMOVE with a tick.
      if (
        event.type !== JobStatusUpdateType.Update ||
        previous?.event.type !== JobStatusUpdateType.Remove
      ) {
        request.events.set(event.job.id, {
          ...update,
          event:
            event.type === JobStatusUpdateType.Update &&
            previous?.event.type === JobStatusUpdateType.Add
              ? { ...event, type: JobStatusUpdateType.Add }
              : event,
        });
      }
    }
    apply(update);
    publish();
  }

  function cancelRequest() {
    if (!request) return;
    const previous = request;
    request = undefined;
    clearTimeout(previous.timeout);
    previous.controller.abort();
  }

  function refresh(replacePending = false) {
    if (disposed || document.hidden || (request && !replacePending)) return;
    cancelRequest();
    const current = {
      controller: new AbortController(),
      timeout: setTimeout(cancelRequest, 15_000),
      events: new Map<string, ReceivedEvent>(),
    };
    request = current;
    void client
      .query({
        query: JobQueueDocument,
        fetchPolicy: "no-cache",
        context: {
          queryDeduplication: false,
          fetchOptions: { signal: current.controller.signal },
        },
      })
      .then(({ data, error }) => {
        if (disposed || request !== current || error || !data) return;
        const now = Date.now();
        const previous = entries;
        entries = new Map(
          (data.jobQueue ?? []).map((job) => [
            job.id,
            {
              job,
              removeAt: finished(job)
                ? (previous.get(job.id)?.removeAt ?? now + 10_000)
                : undefined,
            },
          ]),
        );
        // Only known terminal outcomes keep their short display window.
        // An absent running job must disappear after an authoritative snapshot.
        for (const [id, entry] of previous) {
          if (
            !entries.has(id) &&
            entry.removeAt !== undefined &&
            entry.removeAt > now
          )
            entries.set(id, entry);
        }
        // A delayed snapshot must not undo events received after it started.
        for (const update of current.events.values()) apply(update);
        publish();
      })
      .catch(() => {
        // A failed/offline request is not an empty queue. Retry on the next
        // foreground/reconnect event or periodic reconciliation.
      })
      .finally(() => {
        clearTimeout(current.timeout);
        if (request === current) request = undefined;
      });
  }

  function scheduleRefresh(restart = false) {
    if (disposed || document.hidden) return;
    restartPending ||= restart;
    if (refreshTimer !== undefined) return;
    // Safari may send visibilitychange, pageshow and focus together.
    refreshTimer = setTimeout(() => {
      refreshTimer = undefined;
      if (restartPending) wsClient.terminate();
      restartPending = false;
      publish();
      refresh(true);
    }, 100);
  }

  function visibilityChanged() {
    if (document.hidden) {
      cancelRequest();
      clearTimeout(refreshTimer);
      refreshTimer = undefined;
    } else scheduleRefresh(true);
  }
  const focused = () => scheduleRefresh();
  const online = () => scheduleRefresh(true);
  const pageShown = (event: PageTransitionEvent) =>
    scheduleRefresh(event.persisted);
  const stopConnected = wsClient.on("connected", focused);
  const lifecycle = client
    .subscribe({
      query: JobsLifecycleSubscribeDocument,
      fetchPolicy: "no-cache",
    })
    .subscribe({
      next: ({ data }) => receive(data?.jobsLifecycleSubscribe),
      error: focused,
    });
  const progress = client
    .subscribe({
      query: JobsProgressSubscribeDocument,
      fetchPolicy: "no-cache",
    })
    .subscribe({
      next: ({ data }) => receive(data?.jobsProgressSubscribe),
      error: focused,
    });
  document.addEventListener("visibilitychange", visibilityChanged);
  window.addEventListener("focus", focused);
  window.addEventListener("online", online);
  window.addEventListener("pageshow", pageShown);
  // Also repairs the small query/subscription startup gap and silent sockets.
  // No background polling, and no dependence on the queue already being nonempty.
  const pollTimer = setInterval(refresh, 30_000);
  refresh();

  return () => {
    disposed = true;
    cancelRequest();
    clearTimeout(expiryTimer);
    clearTimeout(refreshTimer);
    clearInterval(pollTimer);
    stopConnected();
    lifecycle.unsubscribe();
    progress.unsubscribe();
    document.removeEventListener("visibilitychange", visibilityChanged);
    window.removeEventListener("focus", focused);
    window.removeEventListener("online", online);
    window.removeEventListener("pageshow", pageShown);
  };
}
