import { z } from "zod";
import { accountUUIDSchema as uuid } from "./account-review-api";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";
import {
  activityAttemptSchema,
  jobActivityFilterSchema,
  jobActivitySchema,
  jobActivityDetailSchema,
  runActivityDetailSchema,
  runActivityFilterSchema,
  runActivitySchema,
  type JobActivityFilter,
  type RunActivityFilter,
} from "./activity-schema";

const cursorSchema = z
  .number()
  .int()
  .nonnegative()
  .max(Number.MAX_SAFE_INTEGER);
function descending<T>(
  rows: T[],
  before: number,
  sequence: (row: T) => number,
) {
  let previous = before || Number.POSITIVE_INFINITY;
  for (const row of rows) {
    const current = sequence(row);
    if (current >= previous)
      throw new NativeArchiveError(0, "invalid_response");
    previous = current;
  }
}

export function createArchiveActivityAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  const request = createArchiveRequest(endpoint, transport);
  const pageLimit = 25;
  function page(before: number) {
    cursorSchema.parse(before);
    return new URLSearchParams({
      before: String(before),
      limit: String(pageLimit),
    });
  }
  async function jobs(
    filter: JobActivityFilter,
    before = 0,
    signal?: AbortSignal,
  ) {
    const checked = jobActivityFilterSchema.parse(filter);
    const query = page(before);
    for (const [key, value] of Object.entries(checked))
      if (value) query.set(key, value);
    const rows = await request(
      `activity/jobs?${query}`,
      z.array(jobActivitySchema).max(pageLimit),
      undefined,
      signal,
    );
    descending(rows, before, (row) => row.sequence);
    if (
      new Set(rows.map((row) => row.uuid)).size !== rows.length ||
      rows.some(
        (row) =>
          (checked.kind && row.kind !== checked.kind) ||
          (checked.state && row.state !== checked.state),
      )
    )
      throw new NativeArchiveError(0, "invalid_response");
    return rows;
  }
  async function runs(
    filter: RunActivityFilter,
    before = 0,
    signal?: AbortSignal,
  ) {
    const checked = runActivityFilterSchema.parse(filter);
    const query = page(before);
    for (const [key, value] of Object.entries(checked))
      if (value) query.set(key, value);
    const rows = await request(
      `activity/runs?${query}`,
      z.array(runActivitySchema).max(pageLimit),
      undefined,
      signal,
    );
    descending(rows, before, (row) => row.sequence);
    if (
      new Set(rows.map((row) => row.uuid)).size !== rows.length ||
      rows.some(
        (row) =>
          (checked.collection && row.collection_uuid !== checked.collection) ||
          (checked.state && row.state !== checked.state),
      )
    )
      throw new NativeArchiveError(0, "invalid_response");
    return rows;
  }
  async function job(id: string, signal?: AbortSignal) {
    const result = await request(
      `activity/jobs/${uuid.parse(id)}`,
      jobActivityDetailSchema,
      undefined,
      signal,
    );
    if (result.summary.uuid !== id)
      throw new NativeArchiveError(0, "invalid_response");
    return result;
  }
  async function run(id: string, signal?: AbortSignal) {
    const result = await request(
      `activity/runs/${uuid.parse(id)}`,
      runActivityDetailSchema,
      undefined,
      signal,
    );
    if (result.summary.uuid !== id)
      throw new NativeArchiveError(0, "invalid_response");
    return result;
  }
  async function attempts(
    type: "jobs" | "runs",
    id: string,
    before = 0,
    signal?: AbortSignal,
  ) {
    z.enum(["jobs", "runs"]).parse(type);
    const rows = await request(
      `activity/${type}/${uuid.parse(id)}/attempts?${page(before)}`,
      z.array(activityAttemptSchema).max(pageLimit),
      undefined,
      signal,
    );
    descending(rows, before, (row) => row.number);
    if (
      rows.some((row) =>
        type === "jobs" ? row.outcome === "deferred" : row.outcome === "failed",
      )
    )
      throw new NativeArchiveError(0, "invalid_response");
    return rows;
  }
  return { endpoint, pageLimit, jobs, job, runs, run, attempts };
}

export type ArchiveActivityAPI = ReturnType<typeof createArchiveActivityAPI>;
