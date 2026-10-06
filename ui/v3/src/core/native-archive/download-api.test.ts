import { expect, it, vi } from "vitest";
import { createDownloadAPI, type DownloadTransfer } from "./download-api";
import {
  downloadTransfer,
  downloadTime,
  emptyDownloadStatus,
} from "../../../tests/fixtures/downloads";
import { albumUUID } from "../../../tests/fixtures/source-albums";

const endpoint = "https://example.test/stash/api/v3/archive/";
const attachment = albumUUID(100);
function page(transfers = [downloadTransfer()]) {
  return {
    attachment_uuid: attachment,
    checked_at: downloadTime,
    transfers,
    next_before: null as number | null,
  };
}
function client(response: unknown) {
  const transport = vi.fn<typeof fetch>(async () => Response.json(response));
  return { api: createDownloadAPI(endpoint, transport), transport };
}

it("reads grouped transfers with application authentication and a bounded receipt cursor", async () => {
  const response = page();
  const { api, transport } = client(response);
  expect(await api.history(attachment, 200)).toEqual(response);
  expect(String(transport.mock.calls[0]?.[0])).toBe(
    `${endpoint}attachments/${attachment}/download-transfers?limit=25&before=200`,
  );
  expect(transport.mock.calls[0]?.[1]).toMatchObject({
    method: "GET",
    credentials: "same-origin",
    cache: "no-store",
    redirect: "error",
  });
  expect(response.transfers[0]?.verification_state).toBe("queued");
  for (const cursor of [0, -1, 1.5, Number.MAX_SAFE_INTEGER + 1]) {
    const invalid = client(response);
    await expect(invalid.api.history(attachment, cursor)).rejects.toThrow();
    expect(invalid.transport).not.toHaveBeenCalled();
  }
});

it("retains late reports and terminal-only transfers without inventing start times", async () => {
  const transfer = downloadTransfer();
  transfer.started_at = null;
  delete transfer.start_event_uuid;
  const response = page([transfer]);
  expect(
    (await client(response).api.history(attachment)).transfers[0]?.started_at,
  ).toBeNull();
  transfer.started_at = "2026-10-06T11:06:00Z";
  transfer.start_event_uuid = albumUUID(6100);
  // Worker clock order is not pagination order, even when start arrives last.
  expect((await client(response).api.history(attachment)).transfers).toEqual([
    transfer,
  ]);
});

it("rejects cross-attachment data, duplicated transfer phases and non-descending anchors", async () => {
  const base = page();
  for (const response of [
    { ...base, attachment_uuid: albumUUID(999) },
    page([{ ...downloadTransfer(), attachment_uuid: albumUUID(999) }]),
    page([downloadTransfer(), downloadTransfer()]),
    page([downloadTransfer(100), downloadTransfer(101)]),
    page([downloadTransfer(100), { ...downloadTransfer(100), sequence: 99 }]),
    { ...base, next_before: 100 },
    {
      ...base,
      transfers: Array.from({ length: 26 }, (_, index) =>
        downloadTransfer(100 - index),
      ),
    },
  ])
    await expect(client(response).api.history(attachment)).rejects.toThrow();
  await expect(client(base).api.history(attachment, 100)).rejects.toThrow();
  const continued = page(
    Array.from({ length: 25 }, (_, index) => downloadTransfer(100 - index)),
  );
  continued.next_before = 76;
  expect((await client(continued).api.history(attachment)).next_before).toBe(
    76,
  );
  continued.next_before = 75;
  await expect(client(continued).api.history(attachment)).rejects.toThrow();
});

it("keeps failure, interruption, exclusion and missing-file skips separate from downloaded files", async () => {
  const started = downloadTransfer();
  started.state = "interrupted";
  started.reported_state = "started";
  started.finished_at = null;
  delete started.terminal_event_uuid;
  delete started.file_event_uuid;
  delete started.verification_job_uuid;
  delete started.verification_state;
  expect(
    (await client(page([started])).api.history(attachment)).transfers[0]?.state,
  ).toBe("interrupted");
  for (const [state, reason] of [
    ["failed", "source_failure"],
    ["excluded", "unsupported_media"],
    ["skipped", "archive_entry_without_file"],
  ] as const) {
    const row: DownloadTransfer = {
      ...started,
      state,
      reported_state: state,
      reason_code: reason,
      finished_at: downloadTime,
      terminal_event_uuid: albumUUID(999),
    };
    expect(
      (await client(page([row])).api.history(attachment)).transfers[0]?.state,
    ).toBe(state);
    await expect(
      client(page([{ ...row, file_event_uuid: albumUUID(444) }])).api.history(
        attachment,
      ),
    ).rejects.toThrow();
  }
  for (const row of [
    { ...started, state: "downloaded" },
    { ...started, verification_state: "succeeded" },
    { ...downloadTransfer(), verification_job_uuid: undefined },
    { ...downloadTransfer(), verification_state: undefined },
    { ...downloadTransfer(), started_at: null },
    { ...downloadTransfer(), reason_code: "filter" },
    { ...downloadTransfer(), state: "available" },
  ])
    await expect(
      client(page([row as DownloadTransfer])).api.history(attachment),
    ).rejects.toThrow();
});

it("batches only distinct requested attachments and validates every returned association", async () => {
  const ids = [attachment, albumUUID(101)];
  const response = emptyDownloadStatus(ids);
  response.attachments[0]!.latest = downloadTransfer();
  const { api, transport } = client(response);
  expect(await api.status(ids)).toEqual(response);
  expect(transport.mock.calls[0]?.[1]).toMatchObject({
    method: "POST",
    body: JSON.stringify({ attachments: ids }),
    cache: "no-store",
    credentials: "same-origin",
  });
  for (const invalidIds of [
    [],
    [attachment, attachment],
    Array.from({ length: 26 }, (_, index) => albumUUID(index)),
    ["invalid"],
  ]) {
    const invalid = client(response);
    await expect(invalid.api.status(invalidIds)).rejects.toThrow();
    expect(invalid.transport).not.toHaveBeenCalled();
  }
  for (const invalid of [
    emptyDownloadStatus([...ids].reverse()),
    emptyDownloadStatus([attachment]),
    {
      ...response,
      attachments: [
        {
          attachment_uuid: attachment,
          latest: downloadTransfer(100, albumUUID(101)),
        },
        response.attachments[1],
      ],
    },
  ])
    await expect(client(invalid).api.status(ids)).rejects.toThrow();
});
