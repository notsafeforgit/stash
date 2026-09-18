// @vitest-environment jsdom
import { StrictMode } from "react";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import {
  ApolloClient,
  ApolloLink,
  InMemoryCache,
  Observable,
} from "@apollo/client";
import { ApolloProvider } from "@apollo/client/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { defaultTvSettings, type TvSettings } from "@/core/tv/settings";
import type { TvFeedItem } from "@/core/tv/feed-state";
import { usePlayerTranscodeSession } from "@/components/player/use-player-transcode-session";
import { injectStreamSession } from "@/components/player/scene-player-source-url";
import { tvMediaWindow, useTvMediaWindow } from "./use-tv-media-window";

const items: TvFeedItem[] = Array.from({ length: 10 }, (_, i) => ({
  kind: "marker",
  id: String(i + 1),
  key: `marker:${i + 1}`,
  sceneId: "1",
}));
const beacon = vi.fn<Navigator["sendBeacon"]>();
const fetchMock = vi.fn<typeof fetch>();
let root: Root;
let container: HTMLDivElement;
let client: ApolloClient;

function Harness({
  selected,
  count,
  leaving = false,
}: {
  selected: number;
  count: TvSettings["preloadCount"];
  leaving?: boolean;
}) {
  const session = useTvMediaWindow({
    items,
    selected,
    settings: { ...defaultTvSettings, preloadCount: count },
    seed: 42,
    identity: "feed",
    leaving,
    ready: false,
  });
  usePlayerTranscodeSession(
    "1",
    session
      ? injectStreamSession(
          "https://stash.test/scene/1/stream.master.m3u8?resolution=LOW",
          session.id,
        )
      : undefined,
    session,
  );
  return <div data-session={session?.id} />;
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
  client = new ApolloClient({
    cache: new InMemoryCache(),
    link: new ApolloLink(
      () =>
        new Observable((observer) => {
          observer.next({ data: { findScene: null } });
          observer.complete();
        }),
    ),
  });
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});

afterEach(async () => {
  await act(async () => root.unmount());
  client.stop();
  container.remove();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

async function show(
  selected: number,
  count: TvSettings["preloadCount"],
  leaving = false,
) {
  await act(async () =>
    root.render(
      <StrictMode>
        <ApolloProvider client={client}>
          <Harness selected={selected} count={count} leaving={leaving} />
        </ApolloProvider>
      </StrictMode>,
    ),
  );
  return container
    .querySelector("[data-session]")
    ?.getAttribute("data-session");
}

it("centers 1, 3 and 5 without shifting extra entries to the available side at feed edges", () => {
  for (const count of [1, 3, 5] as const) {
    expect(tvMediaWindow(items, 5, count)).toHaveLength(count);
    expect(tvMediaWindow(items, 0, count)).toEqual(
      items.slice(0, (count + 1) / 2),
    );
    expect(tvMediaWindow(items, 9, count)).toEqual(
      items.slice(10 - (count + 1) / 2),
    );
  }
});

it("retains two previous marker encoders, releases the oldest on movement, and closes everything on exit", async () => {
  const first = await show(0, 5);
  const second = await show(1, 5);
  const third = await show(2, 5);
  expect(new Set([first, second, third]).size).toBe(3);
  expect(beacon).not.toHaveBeenCalled();
  await act(async () => vi.advanceTimersByTime(15000));
  for (const id of [first, second, third])
    expect(
      fetchMock.mock.calls.some(
        ([url]) =>
          new URL(String(url)).searchParams.get("stream_session") === id,
      ),
    ).toBe(true);
  const fourth = await show(3, 5);
  await act(async () => vi.runAllTicks());
  expect(beacon).toHaveBeenCalledOnce();
  expect(
    new URL(String(beacon.mock.calls[0]?.[0])).searchParams.get(
      "stream_session",
    ),
  ).toBe(first);
  expect(
    new URL(String(beacon.mock.calls[0]?.[0])).searchParams.get("release"),
  ).toBe("1");
  await show(3, 5, true);
  await act(async () => vi.runAllTicks());
  expect(
    new Set(
      beacon.mock.calls.map(([url]) =>
        new URL(String(url)).searchParams.get("stream_session"),
      ),
    ),
  ).toEqual(new Set([first, second, third, fourth]));
  fetchMock.mockClear();
  await act(async () => vi.advanceTimersByTime(90000));
  expect(fetchMock).not.toHaveBeenCalled();
});

it("shrinks the window without replacing the active session", async () => {
  await show(0, 5);
  await show(1, 5);
  const active = await show(2, 5);
  expect(await show(2, 1)).toBe(active);
  await act(async () => vi.runAllTicks());
  expect(beacon).toHaveBeenCalledTimes(2);
  expect(
    beacon.mock.calls.every(
      ([url]) =>
        new URL(String(url)).searchParams.get("stream_session") !== active,
    ),
  ).toBe(true);
});

it("stops every retained encoder on pagehide and renews only after a cached-page restore", async () => {
  const first = await show(0, 5);
  const second = await show(1, 5);
  const third = await show(2, 5);
  fetchMock.mockClear();
  await act(async () =>
    window.dispatchEvent(
      new PageTransitionEvent("pagehide", { persisted: true }),
    ),
  );
  expect(
    new Set(
      beacon.mock.calls.map(([url]) =>
        new URL(String(url)).searchParams.get("stream_session"),
      ),
    ),
  ).toEqual(new Set([first, second, third]));
  expect(
    beacon.mock.calls.every(
      ([url]) => !new URL(String(url)).searchParams.has("release"),
    ),
  ).toBe(true);
  await act(async () => vi.advanceTimersByTime(90000));
  expect(fetchMock).not.toHaveBeenCalled();
  await act(async () =>
    window.dispatchEvent(
      new PageTransitionEvent("pageshow", { persisted: true }),
    ),
  );
  expect(fetchMock).toHaveBeenCalledTimes(3);

  beacon.mockClear();
  await act(async () =>
    window.dispatchEvent(
      new PageTransitionEvent("pagehide", { persisted: false }),
    ),
  );
  expect(beacon).toHaveBeenCalledTimes(3);
  expect(
    beacon.mock.calls.every(
      ([url]) => new URL(String(url)).searchParams.get("release") === "1",
    ),
  ).toBe(true);
});
