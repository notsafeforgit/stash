import type { WebSocketRoute } from "@playwright/test";
import { z } from "zod";
import { expect, test } from "./test";
import {
  JobStatus,
  JobStatusUpdateType,
  type JobDataFragment,
} from "@/core/generated-graphql";

const messageSchema = z.object({
  type: z.string(),
  id: z.string().optional(),
  payload: z.object({ query: z.string() }).optional(),
});

test("resumes the actual task list and restores live progress after a missed completion", async ({
  page,
}) => {
  const original: JobDataFragment = {
    __typename: "Job",
    id: "1",
    status: JobStatus.Running,
    description: "Suspended generation",
    progress: 0.2,
    subTasks: [],
    error: null,
    addTime: "2026-09-21T00:00:00Z",
    startTime: null,
    endTime: null,
  };
  let queue: JobDataFragment[] | null = [original];
  let connections = 0;
  const streams = new Map<string, { socket: WebSocketRoute; id: string }>();
  await page.route("**/graphql", async (route) => {
    z.object({ operationName: z.literal("JobQueue") }).parse(
      route.request().postDataJSON(),
    );
    await route.fulfill({ json: { data: { jobQueue: queue } } });
  });
  await page.routeWebSocket("**/graphql", (socket) => {
    connections++;
    streams.clear();
    socket.onMessage((raw) => {
      const message = messageSchema.parse(JSON.parse(raw.toString()));
      if (message.type === "connection_init") {
        socket.send(JSON.stringify({ type: "connection_ack" }));
      } else if (
        message.type === "subscribe" &&
        message.id &&
        message.payload
      ) {
        const field = message.payload.query.includes("JobsLifecycleSubscribe")
          ? "jobsLifecycleSubscribe"
          : "jobsProgressSubscribe";
        streams.set(field, { socket, id: message.id });
      }
    });
  });
  function send(type: JobStatusUpdateType, job: JobDataFragment) {
    const field =
      type === JobStatusUpdateType.Update
        ? "jobsProgressSubscribe"
        : "jobsLifecycleSubscribe";
    const stream = streams.get(field);
    if (!stream) throw new Error("Missing task subscription");
    stream.socket.send(
      JSON.stringify({
        id: stream.id,
        type: "next",
        payload: {
          data: { [field]: { __typename: "JobStatusUpdate", type, job } },
        },
      }),
    );
  }
  await page.clock.install();
  await page.goto("/job-queue.html");
  await expect(page.getByText(original.description)).toBeVisible();
  await expect.poll(() => streams.size).toBe(2);
  const initialConnections = connections;
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", {
      configurable: true,
      value: true,
    });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  // The server finishes while the app cannot receive the lifecycle event.
  queue = null;
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", {
      configurable: true,
      value: false,
    });
    document.dispatchEvent(new Event("visibilitychange"));
    window.dispatchEvent(
      new PageTransitionEvent("pageshow", { persisted: true }),
    );
    window.dispatchEvent(new Event("focus"));
  });
  await expect(page.getByText("No tasks are currently running.")).toBeVisible();
  await expect.poll(() => connections).toBeGreaterThan(initialConnections);
  await expect.poll(() => streams.size).toBe(2);

  const next = { ...original, id: "2", description: "New generation" };
  queue = [next];
  send(JobStatusUpdateType.Add, next);
  await expect(page.getByText(next.description)).toBeVisible();
  send(JobStatusUpdateType.Update, { ...next, progress: 0.6 });
  await expect(page.getByRole("progressbar")).toHaveAttribute(
    "aria-valuenow",
    "60",
  );
  queue = null;
  send(JobStatusUpdateType.Remove, { ...next, status: JobStatus.Finished });
  await expect(page.getByRole("button", { name: "Stop job" })).toBeDisabled();
  await page.clock.fastForward(10_100);
  await expect(page.getByText("No tasks are currently running.")).toBeVisible();
});
