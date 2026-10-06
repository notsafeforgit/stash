import { z } from "zod";
import { accountUUIDSchema as uuid } from "./account-review-api";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";

const integer = z.number().int().positive().max(Number.MAX_SAFE_INTEGER);
const timestamp = z.string().datetime({ offset: true });
export const downloadTransferSchema = z
  .object({
    sequence: integer,
    attachment_uuid: uuid,
    producer_uuid: uuid,
    run_uuid: uuid,
    fence: integer,
    transfer_sequence: integer,
    capture_event_uuid: uuid,
    collection_uuid: uuid,
    collection_revision: integer,
    collection_label: z.string(),
    root_uuid: uuid,
    root_label: z.string(),
    state: z.enum([
      "downloading",
      "interrupted",
      "downloaded",
      "failed",
      "excluded",
      "skipped",
    ]),
    reported_state: z.enum([
      "started",
      "downloaded",
      "failed",
      "excluded",
      "skipped",
    ]),
    reason_code: z
      .enum([
        "download_failed",
        "postprocess_failed",
        "source_failure",
        "unsupported_media",
        "filter",
        "archive_entry_without_file",
        "existing_without_file",
      ])
      .optional(),
    start_event_uuid: uuid.optional(),
    terminal_event_uuid: uuid.optional(),
    started_at: timestamp.nullable(),
    finished_at: timestamp.nullable(),
    first_recorded_at: timestamp,
    last_recorded_at: timestamp,
    file_event_uuid: uuid.optional(),
    verification_job_uuid: uuid.optional(),
    verification_state: z
      .enum(["queued", "running", "succeeded", "failed", "cancelled"])
      .optional(),
  })
  .refine((row) => {
    if (
      Boolean(row.start_event_uuid) !== (row.started_at !== null) ||
      Boolean(row.terminal_event_uuid) !== (row.finished_at !== null)
    )
      return false;
    if (row.reported_state === "started")
      return (
        Boolean(row.start_event_uuid) &&
        !row.terminal_event_uuid &&
        (row.state === "downloading" || row.state === "interrupted") &&
        !row.file_event_uuid &&
        !row.verification_job_uuid &&
        !row.verification_state &&
        !row.reason_code
      );
    if (!row.terminal_event_uuid || row.state !== row.reported_state)
      return false;
    if (row.reported_state === "downloaded")
      return (
        Boolean(
          row.file_event_uuid &&
            row.verification_job_uuid &&
            row.verification_state,
        ) && !row.reason_code
      );
    if (
      row.file_event_uuid ||
      row.verification_job_uuid ||
      row.verification_state
    )
      return false;
    if (row.reported_state === "failed")
      return ["download_failed", "postprocess_failed", "source_failure"].some(
        (reason) => reason === row.reason_code,
      );
    if (row.reported_state === "excluded")
      return (
        row.reason_code === "unsupported_media" || row.reason_code === "filter"
      );
    return (
      row.reason_code === "archive_entry_without_file" ||
      row.reason_code === "existing_without_file"
    );
  });
export type DownloadTransfer = z.infer<typeof downloadTransferSchema>;
const pageSchema = z.object({
  attachment_uuid: uuid,
  checked_at: timestamp,
  transfers: z.array(downloadTransferSchema).max(25),
  next_before: integer.nullable(),
});
const statusSchema = z.object({
  checked_at: timestamp,
  attachments: z
    .array(
      z.object({
        attachment_uuid: uuid,
        latest: downloadTransferSchema.nullable(),
      }),
    )
    .min(1)
    .max(25),
});
export type DownloadStatus = z.infer<typeof statusSchema>;

export function createDownloadAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  const request = createArchiveRequest(endpoint, transport);
  return {
    endpoint,
    async history(attachment: string, before?: number, signal?: AbortSignal) {
      const query = new URLSearchParams({ limit: "25" });
      if (before !== undefined)
        query.set("before", String(integer.parse(before)));
      const page = await request(
        `attachments/${uuid.parse(attachment)}/download-transfers?${query}`,
        pageSchema,
        undefined,
        signal,
      );
      if (page.attachment_uuid !== attachment)
        throw new NativeArchiveError(0, "invalid_response");
      let previous = before ?? Number.POSITIVE_INFINITY;
      const transfers = new Set<string>();
      for (const row of page.transfers) {
        const key = `${row.producer_uuid}:${row.capture_event_uuid}`;
        if (
          row.attachment_uuid !== attachment ||
          row.sequence >= previous ||
          transfers.has(key)
        )
          throw new NativeArchiveError(0, "invalid_response");
        previous = row.sequence;
        transfers.add(key);
      }
      if (
        page.next_before !== null &&
        (page.transfers.length !== 25 ||
          page.next_before !== page.transfers.at(-1)?.sequence)
      )
        throw new NativeArchiveError(0, "invalid_response");
      return page;
    },
    async status(attachments: string[], signal?: AbortSignal) {
      z.array(uuid)
        .min(1)
        .max(25)
        .refine((ids) => new Set(ids).size === ids.length)
        .parse(attachments);
      const page = await request(
        "attachments/download-status",
        statusSchema,
        JSON.stringify({ attachments }),
        signal,
      );
      if (
        page.attachments.length !== attachments.length ||
        page.attachments.some(
          (row, index) =>
            row.attachment_uuid !== attachments[index] ||
            (row.latest !== null &&
              row.latest.attachment_uuid !== row.attachment_uuid),
        )
      )
        throw new NativeArchiveError(0, "invalid_response");
      return page;
    },
  };
}
export type DownloadAPI = ReturnType<typeof createDownloadAPI>;
