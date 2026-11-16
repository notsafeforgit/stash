// @vitest-environment jsdom
import { act, useRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import type { VideoPlayerStore } from "@videojs/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useScenePlayerSources } from "./use-scene-player-sources";

// Source eligibility is not under test here; keep both streams selectable.
vi.mock("./player-utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./player-utils")>()),
  canPlaySource: () => true,
}));

type Api = ReturnType<typeof useScenePlayerSources>;

const DIRECT_URL = "https://stash.test/scene/1/stream";
const HLS_URL =
  "https://stash.test/scene/1/stream.master.m3u8?resolution=ORIGINAL";

/** Video.js store double. A seek resolves when it is superseded, as
 * `store.seek` does in @videojs/core, and when it reaches `seeked`. */
interface FakeStore {
  state: { currentTime: number; paused: boolean; playbackRate: number };
  seek: ReturnType<typeof vi.fn<(time: number) => Promise<unknown>>>;
  play: ReturnType<typeof vi.fn<() => Promise<void>>>;
  pause: ReturnType<typeof vi.fn<() => void>>;
  setPlaybackRate: ReturnType<typeof vi.fn<(rate: number) => void>>;
  /** Resolve the in-flight seek, as a `seeked` event or a new source would. */
  settle(): void;
}

function createStore(): FakeStore {
  let inflight: (() => void) | null = null;
  const settle = () => {
    const resolve = inflight;
    inflight = null;
    resolve?.();
  };
  const store: FakeStore = {
    state: { currentTime: 0, paused: false, playbackRate: 1 },
    seek: vi.fn(
      (_time: number) =>
        new Promise<unknown>((resolve) => {
          // A newer seek supersedes the previous one; the old promise resolves.
          settle();
          inflight = () => resolve(undefined);
        }),
    ),
    play: vi.fn(() => {
      store.state.paused = false;
      return Promise.resolve();
    }),
    pause: vi.fn(() => {
      store.state.paused = true;
    }),
    setPlaybackRate: vi.fn((rate: number) => {
      store.state.playbackRate = rate;
    }),
    settle,
  };
  return store;
}

/** An hls.js-shaped engine so the drag preview pauses and resumes buffering. */
function createEngine() {
  return {
    engine: {
      stopLoad: vi.fn(),
      startLoad: vi.fn(),
      trigger: vi.fn(),
      bufferingEnabled: true,
      pauseBuffering: vi.fn(),
      resumeBuffering: vi.fn(),
    },
  };
}

const EMPTY_RANGES = {
  length: 0,
  start: () => 0,
  end: () => 0,
} as unknown as TimeRanges;

let latest: Api;
let store: FakeStore;
let media: ReturnType<typeof createEngine>;
let container: HTMLDivElement;
let root: Root;
let video: HTMLVideoElement;
let canvasContext: { drawImage: ReturnType<typeof vi.fn> };

interface HarnessProps {
  initialTimestamp?: number;
  playbackRate: number;
}

function Harness({ initialTimestamp, playbackRate }: HarnessProps) {
  const rootRef = useRef<HTMLDivElement>(null);
  const storeRef = useRef(store as unknown as VideoPlayerStore);
  const mediaRef = useRef<unknown>(media);
  const playbackRateRef = useRef(playbackRate);
  const api = useScenePlayerSources({
    transcodeSession: undefined,
    qualityPreference: undefined,
    scene: {
      id: "scene-1",
      sceneStreams: [
        { url: DIRECT_URL, mime_type: "video/mp4", label: "Direct" },
        { url: HLS_URL, mime_type: "application/vnd.apple.mpegurl" },
      ],
      resume_time: null,
    },
    autoplay: true,
    playbackKey: "scene-1",
    suspended: false,
    playbackRateRef,
    initialTimestamp,
    canDecode: true,
    canDecodeVideo: true,
    canDecodeDirect: true,
    frameRate: 30,
    fileDuration: 600,
    fileWidth: 1920,
    fileHeight: 1080,
    sourceRevision: "rev-1",
    rootRef,
    storeRef,
    mediaRef,
    allowAutoplay: true,
    clipRange: undefined,
  });
  latest = api;
  return (
    <div ref={rootRef} data-testid="root">
      {/* biome-ignore lint/a11y/useMediaCaption: synthetic media clock only. */}
      <video />
      {api.freezeFrameCanvas}
    </div>
  );
}

/** Give the rendered element the media state the hook reads. */
function instrumentVideo(element: HTMLVideoElement) {
  let time = 0;
  Object.defineProperties(element, {
    readyState: { get: () => 4 },
    videoWidth: { get: () => 1920 },
    videoHeight: { get: () => 1080 },
    seeking: { get: () => false },
    buffered: { get: () => EMPTY_RANGES },
    seekable: { get: () => EMPTY_RANGES },
    currentTime: {
      get: () => time,
      set: (next: number) => {
        time = next;
      },
    },
  });
  vi.spyOn(element, "pause").mockImplementation(() => {});
  return element;
}

async function mount(props: Partial<HarnessProps> = {}) {
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <Harness
        initialTimestamp={props.initialTimestamp}
        playbackRate={props.playbackRate ?? 1}
      />,
    );
  });
  video = instrumentVideo(container.querySelector("video") as HTMLVideoElement);
}

async function flushMicrotasks() {
  for (let i = 0; i < 10; i++) await Promise.resolve();
}

/** Scrub surface as `SceneScrubPreview` lays it out while a remote preview shows. */
function showScrubPreview() {
  const surface = document.createElement("div");
  surface.dataset.previewReady = "true";
  surface.dataset.previewKind = "video";
  const scrubVideo = document.createElement("video");
  scrubVideo.dataset.scrubPreview = "";
  surface.append(scrubVideo);
  container.querySelector('[data-testid="root"]')?.append(surface);
  return scrubVideo;
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  store = createStore();
  media = createEngine();
  canvasContext = { drawImage: vi.fn() };
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue({
    ...canvasContext,
    clearRect: vi.fn(),
    fillRect: vi.fn(),
    getImageData: vi.fn(() => ({ data: new Uint8ClampedArray(4) })),
  } as never);
  vi.spyOn(HTMLCanvasElement.prototype, "toBlob").mockImplementation(() => {});
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("readiness seek superseded by a same-load seek", () => {
  it("finishes readiness with the newest target without re-seeking", async () => {
    await mount({ initialTimestamp: 30, playbackRate: 1 });
    await act(async () => {
      void latest.handleCanPlay();
    });
    expect(store.seek).toHaveBeenLastCalledWith(30);

    await act(async () => {
      latest.handleSeek(60);
    });
    await flushMicrotasks();

    expect(store.seek.mock.calls).toEqual([[30], [60]]);
    expect(latest.ready).toBe(true);
    expect(store.play).toHaveBeenCalledTimes(1);
    expect(store.pause).not.toHaveBeenCalled();
    // The newer seek is still loading, so readiness must not clear its feedback
    // even after the minimum spinner window has elapsed.
    await act(async () => {
      vi.advanceTimersByTime(300);
    });
    expect(latest.reloading).toBe(true);

    await act(async () => {
      video.dispatchEvent(new Event("seeked"));
      vi.advanceTimersByTime(300);
    });
    expect(latest.reloading).toBe(false);
  });

  it("applies a pause issued while the readiness seek is pending", async () => {
    await mount({ initialTimestamp: 30, playbackRate: 1 });
    await act(async () => {
      void latest.handleCanPlay();
    });

    await act(async () => {
      latest.setPendingPaused(true);
      latest.handleSeek(60);
    });
    await flushMicrotasks();

    expect(latest.ready).toBe(true);
    expect(store.pause).toHaveBeenCalledTimes(1);
    expect(store.play).not.toHaveBeenCalled();
  });

  it("keeps a playback rate the user changed during the readiness seek", async () => {
    await mount({ initialTimestamp: 30, playbackRate: 1.5 });
    await act(async () => {
      void latest.handleCanPlay();
    });
    // The user changes rate while the readiness seek is in flight.
    store.setPlaybackRate(2);

    await act(async () => {
      latest.handleSeek(60);
    });
    await flushMicrotasks();

    expect(latest.ready).toBe(true);
    expect(store.state.playbackRate).toBe(2);
  });

  it("abandons the readiness seek when a source reload commits", async () => {
    await mount({ initialTimestamp: 30, playbackRate: 1 });
    await act(async () => {
      void latest.handleCanPlay();
    });

    await act(async () => {
      latest.retrySource(30);
    });
    store.settle();
    await flushMicrotasks();

    expect(latest.ready).toBe(false);
    expect(store.play).not.toHaveBeenCalled();
    expect(latest.reloading).toBe(true);

    // The reloaded source has its own canplay, which owns readiness.
    await act(async () => {
      const ready = latest.handleCanPlay();
      store.settle();
      await ready;
    });
    expect(latest.ready).toBe(true);
    expect(store.play).toHaveBeenCalledTimes(1);
  });

  it("abandons the readiness seek when a reload is requested but not yet committed", async () => {
    await mount({ initialTimestamp: 30, playbackRate: 1 });
    await act(async () => {
      void latest.handleCanPlay();
    });

    // Requested outside act, so React has not rendered the new source yet.
    latest.retrySource(30);
    store.settle();
    await flushMicrotasks();

    expect(store.play).not.toHaveBeenCalled();
    expect(store.pause).toHaveBeenCalledTimes(1);

    await act(async () => {});
    expect(latest.ready).toBe(false);
  });
});

describe("drag preview commit and cancel", () => {
  async function readyScene() {
    await mount({ playbackRate: 1 });
    await act(async () => {
      await latest.handleCanPlay();
    });
    expect(latest.ready).toBe(true);
  }

  it("commits a release through the drag session exactly once", async () => {
    await readyScene();
    const scrubVideo = showScrubPreview();

    await act(async () => {
      latest.handleSeekPreview(300);
    });
    expect(media.engine.pauseBuffering).toHaveBeenCalledTimes(1);

    await act(async () => {
      latest.handleSeek(300);
    });

    expect(media.engine.resumeBuffering).toHaveBeenCalledTimes(1);
    expect(store.seek).toHaveBeenLastCalledWith(300);
    expect(store.play).toHaveBeenCalledTimes(1);
    // The release holds the frame that was on screen, not the stale main frame.
    expect(canvasContext.drawImage.mock.calls[0]?.[0]).toBe(scrubVideo);
  });

  it("restores the original position once and does not capture the drag frame", async () => {
    await readyScene();
    showScrubPreview();
    video.currentTime = 120;

    await act(async () => {
      latest.handleSeekPreview(300);
    });
    // A buffered in-element preview moved the main element away from the
    // original position; cancelling must seek it back to 120.
    video.currentTime = 150;
    await act(async () => {
      latest.handleSeekPreview(null);
    });

    expect(media.engine.pauseBuffering).toHaveBeenCalledTimes(1);
    expect(media.engine.resumeBuffering).toHaveBeenCalledTimes(1);
    expect(store.seek).toHaveBeenLastCalledWith(120);
    // The restore target is not the drag frame on screen, so nothing is frozen.
    expect(canvasContext.drawImage).not.toHaveBeenCalled();
  });
});
