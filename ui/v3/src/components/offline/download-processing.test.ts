// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  checkDownloadProcessing,
  downloadProgressValue,
  watchDownloadProcessing,
  VIDEO_PROCESSING_FAILED,
  type DownloadProcessing,
} from "./download-processing";

vi.mock("./offline-scope", () => ({
  getOfflineScope: async () => ({
    deploymentURL: "https://example.com/stash/",
  }),
}));

const entry = { scene_id: "42", request_id: "this-attempt" };
const progress: DownloadProcessing = {
  request_id: entry.request_id,
  state: "processing",
  processed_seconds: 25,
  duration_seconds: 100,
};
const fetchMock = vi.fn();
const stopWatching: (() => void)[] = [];
beforeEach(() => {
  vi.useFakeTimers();
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => {
  stopWatching.splice(0).forEach((stop) => {
    stop();
  });
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

it("uses video time when bytes are unknown and reserves completion for local saving", () => {
  const input = { bytesDownloaded: 50, bytesTotal: null, processing: progress };
  expect(downloadProgressValue(input)).toEqual({
    kind: "processing",
    percent: 25,
  });
  expect(downloadProgressValue({ ...input, bytesTotal: 200 })).toEqual({
    kind: "transfer",
    percent: 25,
  });
  expect(downloadProgressValue({ ...input, bytesTotal: 50 })).toEqual({
    kind: "saving",
    percent: 99,
  });
  expect(
    downloadProgressValue({
      ...input,
      processing: { ...progress, processed_seconds: 120 },
    }),
  ).toEqual({ kind: "processing", percent: 99 });
  expect(
    downloadProgressValue({
      ...input,
      processing: { ...progress, state: "finished" },
    }),
  ).toEqual({ kind: "saving", percent: 99 });
  expect(
    downloadProgressValue({
      ...input,
      processing: { ...progress, duration_seconds: 0 },
    }),
  ).toEqual({ kind: "processing", percent: null });
  expect(downloadProgressValue({ ...input, processing: undefined })).toEqual({
    kind: "transfer",
    percent: null,
  });
});

it("polls one request at a time with the deployment prefix and stops at the terminal snapshot", async () => {
  let resolve: (value: Response) => void = () => {};
  fetchMock.mockImplementationOnce(
    () =>
      new Promise<Response>((done) => {
        resolve = done;
      }),
  );
  fetchMock.mockImplementation(async () =>
    Response.json({ ...progress, state: "finished" }),
  );
  const changed = vi.fn();
  stopWatching.push(watchDownloadProcessing(entry, changed));
  await vi.advanceTimersByTimeAsync(3000);
  expect(fetchMock).toHaveBeenCalledOnce();
  expect(String(fetchMock.mock.calls[0]?.[0])).toBe(
    "https://example.com/stash/scene/42/download/progress?request_id=this-attempt",
  );
  resolve(Response.json(progress));
  await vi.advanceTimersByTimeAsync(0);
  expect(changed).toHaveBeenLastCalledWith(progress);
  await vi.advanceTimersByTimeAsync(1000);
  expect(changed).toHaveBeenLastCalledWith({ ...progress, state: "finished" });
  await vi.advanceTimersByTimeAsync(10000);
  expect(fetchMock).toHaveBeenCalledTimes(2);
});

it("suppresses a response arriving after cancellation", async () => {
  let resolve: (value: Response) => void = () => {};
  fetchMock.mockImplementation(
    () =>
      new Promise<Response>((done) => {
        resolve = done;
      }),
  );
  const changed = vi.fn();
  const stop = watchDownloadProcessing(entry, changed);
  stopWatching.push(stop);
  await vi.advanceTimersByTimeAsync(0);
  const signal: AbortSignal = fetchMock.mock.calls[0]?.[1].signal;
  stop();
  expect(signal.aborted).toBe(true);
  resolve(Response.json(progress));
  await vi.advanceTimersByTimeAsync(10000);
  expect(changed).not.toHaveBeenCalled();
  expect(fetchMock).toHaveBeenCalledOnce();
});

it.each([
  () => new Response(null, { status: 404 }),
  () => new Response("<html>Older server</html>"),
  () => Response.json({ ...progress, request_id: "previous-attempt" }),
  () => Response.json({ ...progress, processed_seconds: -5 }),
])(
  "ignores unsupported or invalid telemetry without affecting the download",
  async (response) => {
    fetchMock.mockImplementation(async () => response());
    const changed = vi.fn();
    stopWatching.push(watchDownloadProcessing(entry, changed));
    await vi.advanceTimersByTimeAsync(15000);
    expect(changed).not.toHaveBeenCalled();
    expect(fetchMock).toHaveBeenCalledOnce();
    await expect(checkDownloadProcessing(entry)).resolves.toBeUndefined();
  },
);

it("recovers from an initially missing record and temporary network failure", async () => {
  fetchMock.mockRejectedValueOnce(new TypeError("network unavailable"));
  fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));
  fetchMock.mockImplementation(async () => Response.json(progress));
  const changed = vi.fn();
  stopWatching.push(watchDownloadProcessing(entry, changed));
  await vi.advanceTimersByTimeAsync(5000);
  expect(changed).toHaveBeenCalledWith(progress);
});

it("reports an encoder failure after EOF and honors cancellation", async () => {
  fetchMock.mockImplementation(async () =>
    Response.json({ ...progress, state: "failed" }),
  );
  await expect(checkDownloadProcessing(entry)).rejects.toThrow(
    VIDEO_PROCESSING_FAILED,
  );
  const abort = new AbortController();
  abort.abort();
  await expect(
    checkDownloadProcessing(entry, abort.signal),
  ).rejects.toMatchObject({ name: "AbortError" });
});
