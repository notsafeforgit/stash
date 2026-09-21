import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

const cases = [
  { mode: "scene detail", hls: false },
  { mode: "lightbox", hls: false },
  { mode: "TV", hls: false },
  { mode: "scene detail", hls: true },
  { mode: "lightbox", hls: true },
  { mode: "TV", hls: true },
  { mode: "marker lightbox", hls: true },
  { mode: "marker lightbox", hls: true, background: true },
  { mode: "lightbox", hls: false, rate: 2 },
  { mode: "lightbox", hls: false, background: true },
] as const;

for (const scenario of cases) {
  const { mode, hls } = scenario;
  const rate = "rate" in scenario ? scenario.rate : 1;
  const background = "background" in scenario;
  const markerEof = mode === "marker lightbox" && background;
  test(`${mode} ${hls ? "HLS" : "Direct"} loops in place at ${rate}×${background ? " with the EOF fallback" : ""}`, async ({
    page,
  }, testInfo) => {
    test.setTimeout(45000);
    // The encoded marker ends before its metadata boundary, so the fallback
    // must handle actual native EOF instead of the range's timeupdate gate.
    await serveSceneMedia(page, "landscape", {
      clipEnd: markerEof ? 8 : undefined,
    });
    if (!hls)
      await page.route("**/scene/*/stream", async (route) => {
        // Reordered frames exercise the Direct native-loop failure in WebKit.
        // The shorter actual duration also checks EOF before library metadata.
        const response = await route.fetch({
          url: "http://127.0.0.1:3025/media/loop-bframes.mp4",
        });
        await route.fulfill({ response });
      });
    await page.addInitScript((background) => {
      localStorage.setItem("stash-lightbox-loop", "true");
      if (background)
        Object.defineProperty(document, "hidden", { get: () => true });
    }, background);
    await page.goto(
      mode === "TV"
        ? `/tv-fixture/tv?loop${hls ? "&low" : ""}`
        : mode === "marker lightbox"
          ? `/scene-lightbox?mode=markers${markerEof ? "&late-marker-end" : ""}`
          : mode === "lightbox"
            ? `/scene-lightbox${hls ? "?mode=hls" : ""}`
            : `/scene-detail?landscape&short${hls ? "&hls" : ""}`,
    );
    if (mode.includes("lightbox"))
      await page.getByRole("button", { name: "Open scenes" }).click();
    const player = page.locator("[data-scene-player]");
    const video = player.locator("video");
    if (mode === "scene detail") {
      await player.locator("[data-player-native-button]").click();
      await player.getByRole("button", { name: /^Playback:/ }).click();
    }
    await expect(player).toHaveAttribute("data-playback-ready", "true");
    await expect(video).toHaveJSProperty("paused", false);
    await expect
      .poll(() => video.evaluate((v: HTMLVideoElement) => v.currentTime))
      .toBeGreaterThan(0.25);
    const identity = await video.elementHandle();
    const observation = await video.evaluateHandle((v: HTMLVideoElement) => {
      const data = {
        duration: v.duration,
        nativeEnds: 0,
        events: [] as string[],
        loops: [] as {
          lastTime: number;
          firstTime: number;
          missedFrames: number;
          nativeEnd: boolean;
          wall: number;
          gapMs: number;
          firstFrameMs: number | null;
          progressing: boolean;
        }[],
      };
      let atNativeEnd = false;
      const observeNativeEnd = () => {
        // Video.js samples `ended` on timeupdate too. A loop can seek back
        // before the browser dispatches its queued ended event; observe EOF
        // before the player's handlers rather than counting event delivery.
        if (v.ended) {
          if (!atNativeEnd) data.nativeEnds++;
          atNativeEnd = true;
        } else if (!v.seeking && v.currentTime < v.duration - 0.25) {
          atNativeEnd = false;
        }
      };
      for (const type of ["timeupdate", "pause", "ended", "seeked"])
        v.addEventListener(type, observeNativeEnd, { capture: true });
      let nativeEndsAtPreviousLoop = 0;
      let previous:
        | { wall: number; media: number; presentedFrames: number }
        | undefined;
      const frame = (now: number, metadata: VideoFrameCallbackMetadata) => {
        const media = metadata.mediaTime;
        if (previous && media < previous.media - 0.5) {
          data.loops.push({
            lastTime: previous.media,
            firstTime: media,
            missedFrames: Math.max(
              0,
              metadata.presentedFrames - previous.presentedFrames - 1,
            ),
            nativeEnd: data.nativeEnds > nativeEndsAtPreviousLoop,
            wall: now,
            gapMs: now - previous.wall,
            firstFrameMs: null,
            progressing: false,
          });
          nativeEndsAtPreviousLoop = data.nativeEnds;
        }
        const loop = data.loops.at(-1);
        if (loop && media > loop.firstTime) {
          loop.firstFrameMs ??= now - loop.wall;
          if (media > loop.firstTime + 0.5) loop.progressing = true;
        }
        previous = {
          wall: now,
          media,
          presentedFrames: metadata.presentedFrames,
        };
        v.requestVideoFrameCallback(frame);
      };
      v.requestVideoFrameCallback(frame);
      for (const type of ["seeking", "pause", "play", "emptied", "ended"])
        v.addEventListener(type, () => data.events.push(type));
      return data;
    });
    await video.evaluate((v: HTMLVideoElement, rate) => {
      v.playbackRate = rate;
    }, rate);
    // Natural EOF, including two complete returns to the beginning. Seeking
    // near EOF or merely observing currentTime=0 misses a frozen first frame.
    await expect
      .poll(
        () =>
          observation.evaluate(
            (data) => data.loops.filter((loop) => loop.progressing).length,
          ),
        { timeout: 35000 },
      )
      .toBeGreaterThanOrEqual(2);
    const data = await observation.jsonValue();
    await testInfo.attach("loop-frame-timing", {
      body: JSON.stringify(data),
      contentType: "application/json",
    });
    for (const loop of data.loops.filter((loop) => loop.progressing)) {
      // All fixtures are 30 fps. A callback can miss frames the compositor
      // presented under load; use its counter instead of assuming the first
      // observed frame was the first displayed frame after the loop.
      expect(loop.lastTime).toBeGreaterThan(data.duration - 0.15);
      expect(loop.firstTime).toBeLessThan(0.15 + loop.missedFrames / 30);
      // A busy foreground page can also reach native EOF before the early
      // restart runs. Apply the tight gap budget to actual early restarts;
      // both paths must advance promptly after their first decoded frame.
      if (!loop.nativeEnd) expect(loop.gapMs).toBeLessThan(250);
      expect(loop.firstFrameMs).toBeLessThan(200);
    }
    expect(data.events).not.toContain("emptied");
    expect(data.events.filter((event) => event === "seeking").length).toBe(
      data.loops.length,
    );
    if (background) expect(data.nativeEnds).toBe(data.loops.length);
    await expect(video).toHaveJSProperty("paused", false);
    await expect(video).toHaveJSProperty("playbackRate", rate);
    expect(
      await video.evaluate(
        (element, original) => element === original,
        identity,
      ),
    ).toBe(true);
    if (mode.includes("lightbox"))
      await expect(page.getByTestId("view")).toHaveText("0");
  });
}
