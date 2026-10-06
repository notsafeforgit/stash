// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  createDownloadAPI,
  type DownloadAPI,
} from "@/core/native-archive/download-api";
import { emptyDownloadStatus } from "../../../../tests/fixtures/downloads";
import { albumUUID } from "../../../../tests/fixtures/source-albums";
import { useDownloadStatus, useVisibleDownloadGroup } from "./download-read";

let root: Root, container: HTMLDivElement;
const ids = [albumUUID(100), albumUUID(101)].join(",");
function Fixture({
  api,
  active = true,
  scope = ids,
}: {
  api: DownloadAPI;
  active?: boolean;
  scope?: string;
}) {
  const result = useDownloadStatus(api, scope, active);
  return (
    <div
      data-result={JSON.stringify(result.data)}
      data-busy={result.busy}
      data-error={result.error !== undefined}
    />
  );
}
beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});
afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it("stops hidden groups and never overlaps a slow poll", async () => {
  const transport = vi.fn<typeof fetch>();
  const api = createDownloadAPI(
    "https://example.test/api/v3/archive/",
    transport,
  );
  transport.mockResolvedValueOnce(
    Response.json(emptyDownloadStatus(ids.split(","))),
  );
  await act(async () => root.render(<Fixture api={api} active={false} />));
  expect(transport).not.toHaveBeenCalled();
  await act(async () => root.render(<Fixture api={api} />));
  expect(transport).toHaveBeenCalledOnce();
  let finish!: (response: Response) => void;
  transport.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  await act(async () => vi.advanceTimersByTimeAsync(15000));
  expect(transport).toHaveBeenCalledTimes(2);
  await act(async () => vi.advanceTimersByTimeAsync(60000));
  expect(transport).toHaveBeenCalledTimes(2);
  await act(async () => root.render(<Fixture api={api} active={false} />));
  expect(transport.mock.calls[1]?.[1]?.signal?.aborted).toBe(true);
  await act(async () =>
    finish(
      Response.json({
        ...emptyDownloadStatus(ids.split(",")),
        checked_at: "2026-10-06T18:00:00Z",
      }),
    ),
  );
  expect(container.textContent).toBe("");
  expect(
    container.firstElementChild?.getAttribute("data-result"),
  ).not.toContain("18:00");
  await act(async () => vi.advanceTimersByTimeAsync(60000));
  expect(transport).toHaveBeenCalledTimes(2);
});

it("keeps the last check after failure and ignores late responses for a replaced attachment set", async () => {
  const transport = vi
    .fn<typeof fetch>()
    .mockResolvedValueOnce(Response.json(emptyDownloadStatus(ids.split(","))));
  const api = createDownloadAPI(
    "https://example.test/api/v3/archive/",
    transport,
  );
  await act(async () => root.render(<Fixture api={api} />));
  transport.mockRejectedValueOnce(new Error("offline"));
  await act(async () => vi.advanceTimersByTimeAsync(15000));
  expect(container.firstElementChild?.getAttribute("data-error")).toBe("true");
  expect(container.firstElementChild?.getAttribute("data-result")).toContain(
    albumUUID(100),
  );
  let finish!: (response: Response) => void;
  transport.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  await act(async () => vi.advanceTimersByTimeAsync(15000));
  const next = albumUUID(102);
  transport.mockResolvedValueOnce(Response.json(emptyDownloadStatus([next])));
  await act(async () => root.render(<Fixture api={api} scope={next} />));
  expect(transport.mock.calls[2]?.[1]?.signal?.aborted).toBe(true);
  await act(async () =>
    finish(Response.json(emptyDownloadStatus(ids.split(",")))),
  );
  expect(
    JSON.parse(
      container.firstElementChild!.getAttribute("data-result")!,
    ).attachments.map(
      (row: { attachment_uuid: string }) => row.attachment_uuid,
    ),
  ).toEqual([next]);
});

it("suspends polling when the browser is hidden or the card group leaves the viewport", async () => {
  let observed: ((visible: boolean) => void) | undefined;
  const disconnected = vi.fn();
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(callback: IntersectionObserverCallback) {
        observed = (visible) =>
          callback(
            [{ isIntersecting: visible } as IntersectionObserverEntry],
            this as unknown as IntersectionObserver,
          );
      }
      observe() {}
      disconnect = disconnected;
    },
  );
  let hidden = false;
  vi.spyOn(document, "hidden", "get").mockImplementation(() => hidden);
  const transport = vi.fn<typeof fetch>(async () =>
    Response.json(emptyDownloadStatus(ids.split(","))),
  );
  const api = createDownloadAPI(
    "https://example.test/api/v3/archive/",
    transport,
  );
  function VisibleFixture() {
    const { setNode, active } = useVisibleDownloadGroup();
    useDownloadStatus(api, ids, active);
    return <div ref={setNode} />;
  }
  await act(async () => root.render(<VisibleFixture />));
  expect(transport).not.toHaveBeenCalled();
  await act(async () => observed?.(true));
  expect(transport).toHaveBeenCalledOnce();
  hidden = true;
  await act(async () => document.dispatchEvent(new Event("visibilitychange")));
  await act(async () => vi.advanceTimersByTimeAsync(60000));
  expect(transport).toHaveBeenCalledOnce();
  hidden = false;
  await act(async () => document.dispatchEvent(new Event("visibilitychange")));
  expect(transport).toHaveBeenCalledTimes(2);
  await act(async () => observed?.(false));
  await act(async () => vi.advanceTimersByTimeAsync(60000));
  expect(transport).toHaveBeenCalledTimes(2);
  await act(async () => root.render(null));
  expect(disconnected).toHaveBeenCalled();
});
