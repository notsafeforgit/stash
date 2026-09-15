// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { usePlayerTranscodeSession } from "./use-player-transcode-session";

const source = "https://stash.test/scene/1/stream.master.m3u8?resolution=LOW";
const fetchMock = vi.fn<typeof fetch>();
const beacon = vi.fn<Navigator["sendBeacon"]>();
let container: HTMLDivElement;
let root: Root;

function Session({ src = source }: { src?: string }) {
  usePlayerTranscodeSession("1", src);
  return (
    <div>
      {/* biome-ignore lint/a11y/useMediaCaption: no source; tests transcode ownership only. */}
      <video />
    </div>
  );
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("fetch", fetchMock);
  fetchMock.mockReset().mockResolvedValue(new Response(null, { status: 204 }));
  beacon.mockReset().mockReturnValue(true);
  Object.defineProperty(navigator, "sendBeacon", {
    configurable: true,
    value: beacon,
  });
  vi.spyOn(document, "hidden", "get").mockReturnValue(false);
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});

afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

it.each([
  false,
  true,
])("keeps the transcode lease alive beyond the idle timeout with paused=%s", async (paused) => {
  vi.spyOn(HTMLMediaElement.prototype, "paused", "get").mockReturnValue(paused);
  let lastContact = Date.now();
  fetchMock.mockImplementation(async () => {
    lastContact = Date.now();
    return new Response(null, { status: 204 });
  });
  await act(async () => root.render(<Session />));
  // A fast connection can fill more than a minute of buffer. Playing from
  // that buffer makes no segment requests, but still owns the transcode.
  for (let elapsed = 0; elapsed < 90000; elapsed += 15000) {
    await act(async () => vi.advanceTimersByTime(15000));
    expect(Date.now() - lastContact).toBeLessThan(60000);
  }
  expect(fetchMock).toHaveBeenCalledWith(
    expect.stringContaining(
      "/scene/1/streams.keepalive?keep_type=hls&keep_resolution=LOW",
    ),
    { method: "POST", keepalive: true },
  );
});

it("suspends heartbeats while hidden and renews immediately on return", async () => {
  await act(async () => root.render(<Session />));
  fetchMock.mockClear();
  const hidden = vi.spyOn(document, "hidden", "get").mockReturnValue(true);
  document.dispatchEvent(new Event("visibilitychange"));
  await act(async () => vi.advanceTimersByTime(90000));
  expect(fetchMock).not.toHaveBeenCalled();
  hidden.mockReturnValue(false);
  document.dispatchEvent(new Event("visibilitychange"));
  expect(fetchMock).toHaveBeenCalledOnce();
});

it("moves the lease on quality changes and releases it on unmount", async () => {
  await act(async () => root.render(<Session />));
  fetchMock.mockClear();
  await act(async () =>
    root.render(<Session src={source.replace("LOW", "STANDARD_HD")} />),
  );
  expect(beacon).toHaveBeenLastCalledWith(
    expect.stringContaining(
      "/streams.stop?keep_type=hls&keep_resolution=STANDARD_HD",
    ),
  );
  await act(async () => vi.advanceTimersByTime(15000));
  expect(
    fetchMock.mock.calls.every(([url]) => String(url).includes("STANDARD_HD")),
  ).toBe(true);
  await act(async () => root.render(null));
  expect(beacon).toHaveBeenLastCalledWith(
    expect.stringMatching(/\/streams\.stop$/),
  );
  fetchMock.mockClear();
  await act(async () => vi.advanceTimersByTime(90000));
  expect(fetchMock).not.toHaveBeenCalled();
});

it("does not acquire a transcode lease for a direct file", async () => {
  await act(async () =>
    root.render(<Session src="https://stash.test/scene/1/stream" />),
  );
  await act(async () => vi.advanceTimersByTime(90000));
  expect(fetchMock).not.toHaveBeenCalled();
  expect(beacon).not.toHaveBeenCalled();
});
