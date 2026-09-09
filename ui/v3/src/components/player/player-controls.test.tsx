// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { Container, createPlayer } from "@videojs/react";
import { Video, videoFeatures } from "@videojs/react/video";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import messages from "@/locales/en-GB.json";
import flattenMessages from "@/utils/flatten-messages";
import { PlayerControls } from "./player-controls";

const Player = createPlayer({ features: videoFeatures });
const noop = () => {};
const playerMessages = flattenMessages(messages);

let container: HTMLDivElement;
let root: Root;
const onError = vi.fn();
const onTemporaryPlaybackRateChange = vi.fn();

function PlayerSession() {
  return (
    <IntlProvider locale="en-GB" messages={playerMessages}>
      <Player.Player>
        <Container>
          <Video />
          <PlayerControls
            Player={Player}
            sources={[]}
            activeSource={null}
            onSourceChange={noop}
            markers={[]}
            fileDuration={60}
            offsetStart={0}
            onSeek={noop}
            playbackMode="advance"
            canAdvance
            onCyclePlaybackMode={noop}
            onTemporaryPlaybackRateChange={onTemporaryPlaybackRateChange}
          />
        </Container>
      </Player.Player>
    </IntlProvider>
  );
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.clearAllMocks();
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  vi.stubGlobal("matchMedia", (media: string) => ({
    media,
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  }));
  vi.spyOn(HTMLMediaElement.prototype, "load").mockImplementation(() => {});
  vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => {});
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container, { onUncaughtError: onError });
});

afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function video() {
  const element = container.querySelector("video");
  if (!element) throw new Error("Missing player video");
  return element;
}

async function startHold() {
  await act(async () => root.render(<PlayerSession key="first" />));
  const media = video();
  await act(async () => {
    media.currentTime = 1;
    media.playbackRate = 1.25;
    media.dispatchEvent(new Event("play"));
    media.dispatchEvent(new Event("ratechange"));
  });
  const overlay = container.querySelector('[class*="pointer:coarse"]');
  if (!overlay) throw new Error("Missing touch controls");
  const start = new Event("touchstart", { bubbles: true });
  Object.defineProperty(start, "touches", {
    value: [{ clientX: 100, clientY: 100 }],
  });
  await act(async () => {
    overlay.dispatchEvent(start);
  });
  return { media, overlay };
}

it("restores the previous speed when a hold is released on a live player", async () => {
  const { media, overlay } = await startHold();
  await act(async () => {
    vi.advanceTimersByTime(500);
  });
  expect(media.playbackRate).toBe(2);
  expect(onTemporaryPlaybackRateChange).toHaveBeenLastCalledWith(2);

  await act(async () => {
    overlay.dispatchEvent(new Event("touchend", { bubbles: true }));
  });
  expect(media.playbackRate).toBe(1.25);
  expect(onTemporaryPlaybackRateChange).toHaveBeenLastCalledWith(null);
  expect(onError).not.toHaveBeenCalled();
});

it("can replace the player while 2× is held without interrupting the next player", async () => {
  const { media } = await startHold();
  await act(async () => {
    vi.advanceTimersByTime(500);
  });
  expect(media.playbackRate).toBe(2);

  // The lightbox replaces the active player on scene and marker advance.
  // The previous media detaches before the controls' passive cleanup runs.
  await act(async () => root.render(<PlayerSession key="next" />));
  expect(onError).not.toHaveBeenCalled();
  expect(video()).not.toBe(media);
  expect(video().playbackRate).toBe(1);
  expect(onTemporaryPlaybackRateChange).toHaveBeenLastCalledWith(null);
});

it("cancels a pending hold when its player leaves before activation", async () => {
  await startHold();
  await act(async () => root.render(<PlayerSession key="next" />));
  await act(async () => {
    vi.advanceTimersByTime(500);
  });
  expect(onTemporaryPlaybackRateChange).not.toHaveBeenCalledWith(2);
  expect(video().playbackRate).toBe(1);
  expect(onError).not.toHaveBeenCalled();
});
