import { expect, it, vi } from "vitest";
import { createArchiveActivityAPI } from "./activity-api";
import {
  jobActivityFilterSchema,
  runActivityFilterSchema,
} from "./activity-schema";
import {
  activityAttempt,
  activityIds,
  activityJob,
  activityJobDetail,
  activityRun,
  activityRunDetail,
} from "../../../tests/fixtures/archive-activity";

const endpoint = "https://example.test/stash/api/v3/archive/";
const jobs = jobActivityFilterSchema.parse({});
const runs = runActivityFilterSchema.parse({});
function client(body: unknown) {
  const transport = vi.fn<typeof fetch>(async () => Response.json(body));
  return { api: createArchiveActivityAPI(endpoint, transport), transport };
}

it("reads bounded history with the deployment prefix and application session", async () => {
  const { api, transport } = client([activityJob()]);
  expect(
    await api.jobs({ kind: "media.verify", state: "queued" }, 11),
  ).toHaveLength(1);
  const [address, options] = transport.mock.calls[0] ?? [];
  const url = new URL(String(address));
  expect(url.pathname).toBe("/stash/api/v3/archive/activity/jobs");
  expect(Object.fromEntries(url.searchParams)).toEqual({
    before: "11",
    limit: "25",
    kind: "media.verify",
    state: "queued",
  });
  expect(options).toMatchObject({
    method: "GET",
    cache: "no-store",
    credentials: "same-origin",
    redirect: "error",
  });
  expect(options?.body).toBeUndefined();
});

it("rejects duplicate, reversed, oversized and incorrectly filtered job pages", async () => {
  const first = activityJob();
  const older = { ...first, uuid: activityIds.other, sequence: 9 };
  expect(await client([first, older]).api.jobs(jobs)).toHaveLength(2);
  for (const rows of [
    [first, first],
    [older, first],
    [{ ...first, state: "running" }],
    Array.from({ length: 26 }, () => first),
  ])
    await expect(client(rows).api.jobs(jobs)).rejects.toThrow();
  await expect(
    client([first]).api.jobs(jobs, first.sequence),
  ).rejects.toThrow();
  await expect(
    client([first]).api.jobs({ ...jobs, state: "failed" }),
  ).rejects.toThrow();
  await expect(
    client([first]).api.jobs({ ...jobs, kind: "album.backfill" }),
  ).rejects.toThrow();
});

it("keeps source runs separate from jobs and validates their original collection filter", async () => {
  const row = activityRun();
  const { api, transport } = client([row]);
  expect(
    await api.runs({ state: "queued", collection: activityIds.collection }),
  ).toEqual([row]);
  const url = new URL(String(transport.mock.calls[0]?.[0]));
  expect(url.searchParams.get("collection")).toBe(activityIds.collection);
  expect(url.pathname).toBe("/stash/api/v3/archive/activity/runs");
  await expect(
    client([row]).api.runs({ ...runs, collection: activityIds.other }),
  ).rejects.toThrow();
  await expect(
    client([row]).api.runs({ ...runs, state: "deferred" }),
  ).rejects.toThrow();
  await expect(client([row, row]).api.runs(runs)).rejects.toThrow();
  await expect(
    client([{ ...row, state: "succeeded" }]).api.runs(runs),
  ).rejects.toThrow();
});

it("checks selected identities and does not misreport partial run coverage as completion", async () => {
  const job = activityJobDetail();
  expect(await client(job).api.job(job.summary.uuid)).toEqual(job);
  await expect(client(job).api.job(activityIds.other)).rejects.toThrow();
  const detail = activityRunDetail();
  detail.completed = [
    { since: "2026-10-05T12:00:00Z", until: "2026-10-05T13:00:00Z" },
  ];
  detail.summary.completed_windows = 1;
  expect(await client(detail).api.run(activityIds.run)).toEqual(detail);
  await expect(client(detail).api.run(activityIds.other)).rejects.toThrow();
  await expect(
    client({ ...detail, pending: [] }).api.run(activityIds.run),
  ).rejects.toThrow();
  await expect(
    client({
      ...detail,
      summary: { ...detail.summary, state: "succeeded" },
    }).api.run(activityIds.run),
  ).rejects.toThrow();
});

it("reads attempt history newest first and validates outcome and time semantics", async () => {
  const first = { ...activityAttempt(), number: 2 };
  const older = activityAttempt();
  expect(
    await client([first, older]).api.attempts("jobs", activityIds.job),
  ).toHaveLength(2);
  for (const rows of [
    [first, first],
    [older, first],
    [{ ...first, outcome: "running" }],
    [{ ...first, ended_at: "2026-10-01T12:00:00Z" }],
  ])
    await expect(
      client(rows).api.attempts("jobs", activityIds.job),
    ).rejects.toThrow();
  await expect(
    client([first]).api.attempts("jobs", activityIds.job, 2),
  ).rejects.toThrow();
  await expect(
    client([{ ...older, outcome: "deferred" }]).api.attempts(
      "jobs",
      activityIds.job,
    ),
  ).rejects.toThrow();
  expect(
    await client([{ ...older, outcome: "deferred" }]).api.attempts(
      "runs",
      activityIds.run,
    ),
  ).toHaveLength(1);
});

it("rejects invalid page inputs before transport and strips unrelated response data", async () => {
  const { api, transport } = client([]);
  for (const before of [-1, 1.5, Number.MAX_SAFE_INTEGER + 1])
    await expect(api.jobs(jobs, before)).rejects.toThrow();
  await expect(api.run("not-an-id")).rejects.toThrow();
  expect(transport).not.toHaveBeenCalled();
  const response = {
    ...activityJobDetail(),
    arguments: { settings: "private" },
    work_key: "private",
  };
  const result = await client(response).api.job(activityIds.job);
  expect(result).toEqual(activityJobDetail());
});
