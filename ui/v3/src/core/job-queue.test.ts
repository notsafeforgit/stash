// @vitest-environment jsdom
import {
  ApolloClient,
  ApolloLink,
  InMemoryCache,
  Observable,
} from "@apollo/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  JobStatus,
  JobStatusUpdateType,
  type JobDataFragment,
  type JobsLifecycleSubscribeSubscription,
} from "./generated-graphql";
import { observeJobQueue, type QueueJob } from "./job-queue";

type JobEvent = JobsLifecycleSubscribeSubscription["jobsLifecycleSubscribe"];
const disposers: Array<() => void> = [];
let hidden = false;

beforeEach(() => {
  vi.useFakeTimers();
  hidden = false;
  vi.spyOn(document, "hidden", "get").mockImplementation(() => hidden);
});

afterEach(() => {
  for (const dispose of disposers.splice(0)) dispose();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

function job(id = "1", status = JobStatus.Running): JobDataFragment {
  return {
    __typename: "Job",
    id,
    status,
    description: `Task ${id}`,
    progress: 0.2,
    subTasks: [],
    error: status === JobStatus.Failed ? "Generation failed" : null,
    addTime: "2026-09-21T00:00:00Z",
    startTime: "2026-09-21T00:00:00Z",
    endTime: null,
  };
}

function fixture() {
  const requests: Array<{
    reply: (jobs: JobDataFragment[] | null) => void;
    fail: () => void;
    signal: AbortSignal;
  }> = [];
  const streams = new Map<string, (event: JobEvent) => void>();
  const connections = new Set<() => void>();
  const changes = vi.fn<(jobs: QueueJob[]) => void>();
  const wsClient = {
    on: (_event: "connected", listener: () => void) => {
      connections.add(listener);
      return () => connections.delete(listener);
    },
    terminate: vi.fn(),
  };
  const client = new ApolloClient({
    cache: new InMemoryCache(),
    link: new ApolloLink(
      (operation) =>
        new Observable((observer) => {
          if (operation.operationName === "JobQueue") {
            const signal: unknown = operation.getContext().fetchOptions?.signal;
            if (!(signal instanceof AbortSignal))
              throw new Error("Missing abort signal");
            requests.push({
              signal,
              reply: (jobs) => {
                observer.next({ data: { jobQueue: jobs } });
                observer.complete();
              },
              fail: () => observer.error(new Error("Offline")),
            });
            return;
          }
          const field =
            operation.operationName === "JobsLifecycleSubscribe"
              ? "jobsLifecycleSubscribe"
              : "jobsProgressSubscribe";
          streams.set(field, (event) =>
            observer.next({ data: { [field]: event } }),
          );
          return () => {
            streams.delete(field);
          };
        }),
    ),
  });
  const stop = observeJobQueue(client, wsClient, changes);
  disposers.push(() => {
    stop();
    client.stop();
  });
  return {
    requests,
    streams,
    connections,
    changes,
    wsClient,
    stop,
    jobs: () => changes.mock.lastCall?.[0] ?? [],
    reconnect: () => {
      for (const listener of connections) listener();
    },
    event: (type: JobStatusUpdateType, value = job()) => {
      const field =
        type === JobStatusUpdateType.Update
          ? "jobsProgressSubscribe"
          : "jobsLifecycleSubscribe";
      streams.get(field)?.({ __typename: "JobStatusUpdate", type, job: value });
    },
    reply: async (
      jobs: JobDataFragment[] | null,
      index = requests.length - 1,
    ) => {
      const request = requests[index];
      if (!request) throw new Error("Missing queue request");
      request.reply(jobs);
      await vi.advanceTimersByTimeAsync(0);
    },
  };
}

function visibility(value: boolean) {
  hidden = value;
  document.dispatchEvent(new Event("visibilitychange"));
}

it("drops a missed completion after Safari resumes and coalesces resume events", async () => {
  const f = fixture();
  await f.reply([job()]);
  visibility(true);
  await vi.advanceTimersByTimeAsync(60_000);
  expect(f.requests).toHaveLength(1);
  expect(f.jobs()[0]?.status).toBe(JobStatus.Running);
  visibility(false);
  window.dispatchEvent(
    new PageTransitionEvent("pageshow", { persisted: true }),
  );
  window.dispatchEvent(new Event("focus"));
  await vi.advanceTimersByTimeAsync(100);
  expect(f.wsClient.terminate).toHaveBeenCalledOnce();
  expect(f.requests).toHaveLength(2);
  await f.reply([]);
  expect(f.jobs()).toEqual([]);
});

it("reconciles a reconnected socket even when the page never became hidden", async () => {
  const f = fixture();
  await f.reply([job()]);
  f.reconnect();
  await vi.advanceTimersByTimeAsync(100);
  await f.reply(null);
  expect(f.jobs()).toEqual([]);
  expect(f.wsClient.terminate).not.toHaveBeenCalled();
});

it.each(["focus", "pageshow", "online"])(
  "refreshes current progress on %s",
  async (event) => {
    const f = fixture();
    await f.reply([job()]);
    window.dispatchEvent(
      event === "pageshow"
        ? new PageTransitionEvent(event, { persisted: true })
        : new Event(event),
    );
    await vi.advanceTimersByTimeAsync(100);
    await f.reply([{ ...job(), progress: 0.8 }]);
    expect(f.jobs()[0]?.progress).toBe(0.8);
    expect(f.wsClient.terminate).toHaveBeenCalledTimes(
      event === "focus" ? 0 : 1,
    );
  },
);

it("periodically repairs missed additions and removals, including repeated empty snapshots", async () => {
  const f = fixture();
  await f.reply([]);
  await vi.advanceTimersByTimeAsync(30_000);
  await f.reply([job()]);
  expect(f.jobs()).toHaveLength(1);
  await vi.advanceTimersByTimeAsync(30_000);
  await f.reply([]);
  expect(f.jobs()).toEqual([]);
  expect(f.requests).toHaveLength(3);
});

it("keeps newer additions and progress when an older snapshot arrives", async () => {
  const f = fixture();
  f.event(JobStatusUpdateType.Add, job("2"));
  f.event(JobStatusUpdateType.Update, { ...job("2"), progress: 0.8 });
  await f.reply([job()]);
  expect(f.jobs().map(({ id }) => id)).toEqual(["1", "2"]);
  expect(f.jobs()[1]?.progress).toBe(0.8);
});

it("does not resurrect a finished job from a late snapshot or progress tick", async () => {
  const f = fixture();
  await f.reply([job()]);
  f.reconnect();
  await vi.advanceTimersByTimeAsync(100);
  f.event(JobStatusUpdateType.Remove, job("1", JobStatus.Failed));
  f.event(JobStatusUpdateType.Update, job());
  await f.reply([job()]);
  expect(f.jobs()[0]?.status).toBe(JobStatus.Failed);
  expect(f.jobs()[0]?.error).toBe("Generation failed");
  await vi.advanceTimersByTimeAsync(10_000);
  expect(f.jobs()).toEqual([]);
  f.event(JobStatusUpdateType.Update, job());
  expect(f.jobs()).toEqual([]);
});

it("preserves terminal results only for their original ten-second window", async () => {
  const f = fixture();
  await f.reply([job()]);
  f.event(JobStatusUpdateType.Remove, job("1", JobStatus.Finished));
  const deadline = Date.now() + 10_000;
  await vi.advanceTimersByTimeAsync(2_000);
  f.reconnect();
  await vi.advanceTimersByTimeAsync(100);
  await f.reply([]);
  expect(f.jobs()[0]?.status).toBe(JobStatus.Finished);
  await vi.advanceTimersByTimeAsync(deadline - Date.now() - 1);
  expect(f.jobs()).toHaveLength(1);
  await vi.advanceTimersByTimeAsync(1);
  expect(f.jobs()).toEqual([]);
});

it("ignores pre-suspension requests even if their response arrives after recovery", async () => {
  const f = fixture();
  visibility(true);
  expect(f.requests[0]?.signal.aborted).toBe(true);
  visibility(false);
  await vi.advanceTimersByTimeAsync(100);
  await f.reply([]);
  await f.reply([job()], 0);
  expect(f.jobs()).toEqual([]);
});

it("preserves jobs during request failures and retries without a page reload", async () => {
  const f = fixture();
  await f.reply([job()]);
  f.reconnect();
  await vi.advanceTimersByTimeAsync(100);
  f.requests[1]?.fail();
  await vi.advanceTimersByTimeAsync(0);
  expect(f.jobs()[0]?.status).toBe(JobStatus.Running);
  await vi.advanceTimersByTimeAsync(30_000);
  await f.reply([]);
  expect(f.jobs()).toEqual([]);
});

it("times out stalled requests so reconciliation can continue", async () => {
  const f = fixture();
  await vi.advanceTimersByTimeAsync(15_000);
  expect(f.requests[0]?.signal.aborted).toBe(true);
  await vi.advanceTimersByTimeAsync(15_000);
  await f.reply([job("2")]);
  await f.reply([job()], 0);
  expect(f.jobs().map(({ id }) => id)).toEqual(["2"]);
});

it("cleans up subscriptions, requests, listeners, and terminal timers", async () => {
  const f = fixture();
  f.event(JobStatusUpdateType.Add);
  f.event(JobStatusUpdateType.Remove, job("1", JobStatus.Cancelled));
  window.dispatchEvent(new Event("focus"));
  const notifications = f.changes.mock.calls.length;
  f.stop();
  expect(f.connections.size).toBe(0);
  expect(f.streams.size).toBe(0);
  expect(f.requests[0]?.signal.aborted).toBe(true);
  window.dispatchEvent(new Event("online"));
  visibility(false);
  await f.reply([job()]);
  await vi.advanceTimersByTimeAsync(60_000);
  expect(f.requests).toHaveLength(1);
  expect(f.changes).toHaveBeenCalledTimes(notifications);
});
