// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  createPlatformMediaSession,
  type PlatformMediaOptions,
} from "./platform-media-session";

const handlers = new Map<MediaSessionAction, MediaSessionActionHandler>();
const session = {
  metadata: null,
  playbackState: "none",
  setPositionState: vi.fn(),
  setActionHandler:
    vi.fn<
      (
        action: MediaSessionAction,
        handler: MediaSessionActionHandler | null,
      ) => void
    >(),
};
const disposers: (() => void)[] = [];
beforeEach(() => {
  handlers.clear();
  vi.clearAllMocks();
  session.setActionHandler.mockImplementation((action, handler) => {
    if (handler) handlers.set(action, handler);
    else handlers.delete(action);
  });
  vi.stubGlobal("navigator", { mediaSession: session });
  vi.stubGlobal(
    "MediaMetadata",
    class {
      constructor(init: MediaMetadataInit) {
        Object.assign(this, init);
      }
    },
  );
});
afterEach(() => {
  for (const dispose of disposers.splice(0)) dispose();
  vi.unstubAllGlobals();
});
function player() {
  const playback = {
    state: {
      paused: true,
      ended: false,
      currentTime: 5,
      duration: 180,
      playbackRate: 1.5,
    },
    play: vi.fn(),
    pause: vi.fn(),
  };
  const options: PlatformMediaOptions = {
    metadata: { title: "Saved video" },
    offsetStart: 120,
    duration: 300,
    suspended: false,
    seek: vi.fn(),
  };
  const controller = createPlatformMediaSession(playback, () => options);
  disposers.push(controller.dispose);
  return { playback, options, ...controller };
}
function action(
  action: MediaSessionAction,
  details: Partial<MediaSessionActionDetails> = {},
) {
  const handler = handlers.get(action);
  if (!handler) throw new Error(`No handler for ${action}`);
  handler({ action, ...details });
}
it("uses scene time for lock-screen seeks and clamps clipped timelines", () => {
  const p = player();
  p.playback.state.paused = false;
  p.refresh();
  expect(session.setPositionState).toHaveBeenLastCalledWith({
    duration: 300,
    position: 125,
    playbackRate: 1.5,
  });
  action("seekforward");
  expect(p.options.seek).toHaveBeenLastCalledWith(135);
  action("seekto", { seekTime: 500 });
  expect(p.options.seek).toHaveBeenLastCalledWith(300);
  p.options.offsetStart = -20;
  p.options.duration = 30;
  p.playback.state.currentTime = 45;
  p.refresh();
  action("seekforward", { seekOffset: 15 });
  expect(p.options.seek).toHaveBeenLastCalledWith(30);
});
it("keeps paused-player controls and protects the new owner's session from old cleanup", () => {
  const first = player();
  first.playback.state.paused = false;
  first.refresh();
  const second = player();
  second.refresh();
  action("pause");
  expect(first.playback.pause).toHaveBeenCalledOnce();
  second.playback.state.paused = false;
  second.options.metadata = { title: "Second video" };
  second.refresh();
  first.dispose();
  action("play");
  expect(second.playback.play).toHaveBeenCalledOnce();
  second.playback.state.paused = true;
  second.refresh();
  expect(session.playbackState).toBe("paused");
  second.options.suspended = true;
  second.refresh();
  expect(session.metadata).toBeNull();
  expect(handlers.size).toBe(0);
});
it("tolerates unsupported actions and clears invalid position state", () => {
  session.setActionHandler.mockImplementation((action, handler) => {
    if (action === "stop") throw new Error("Unsupported action");
    if (handler) handlers.set(action, handler);
    else handlers.delete(action);
  });
  const p = player();
  p.playback.state.paused = false;
  p.options.duration = Number.NaN;
  expect(p.refresh).not.toThrow();
  expect(session.setPositionState).toHaveBeenLastCalledWith(undefined);
  action("seekto", { seekTime: 4 });
  expect(p.options.seek).not.toHaveBeenCalled();
  action("play");
  expect(p.playback.play).toHaveBeenCalledOnce();
});

it("routes OS pause and stop through the player's current explicit pause handler", () => {
  const p = player();
  p.playback.state.paused = false;
  p.options.pause = vi.fn();
  p.refresh();
  action("pause");
  expect(p.options.pause).toHaveBeenCalledOnce();
  const next = vi.fn();
  p.options.pause = next;
  action("stop");
  expect(next).toHaveBeenCalledOnce();
  expect(p.playback.pause).not.toHaveBeenCalled();
  expect(session.playbackState).toBe("none");
});
