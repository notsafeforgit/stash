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
  { mode: "lightbox", hls: false, rate: 2 },
  { mode: "lightbox", hls: false, background: true },
] as const;

for (const scenario of cases) {
  const { mode, hls } = scenario;
  const rate = "rate" in scenario ? scenario.rate : 1;
  const background = "background" in scenario;
  test(`${mode} ${hls ? "HLS" : "Direct"} loops in place at ${rate}×${background ? " with the EOF fallback" : ""}`, async ({
    page,
  }, testInfo) => {
    test.setTimeout(45000);
    await serveSceneMedia(page);
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
          ? "/scene-lightbox?mode=markers"
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
        events: [] as string[],
        loops: [] as {
          lastTime: number;
          firstTime: number;
          wall: number;
          gapMs: number;
          firstFrameMs: number | null;
          progressing: boolean;
        }[],
      };
      let previous: { wall: number; media: number } | undefined;
      const frame = (now: number, metadata: VideoFrameCallbackMetadata) => {
        const media = metadata.mediaTime;
        if (previous && media < previous.media - 0.5)
          data.loops.push({
            lastTime: previous.media,
            firstTime: media,
            wall: now,
            gapMs: now - previous.wall,
            firstFrameMs: null,
            progressing: false,
          });
        const loop = data.loops.at(-1);
        if (loop && media > loop.firstTime) {
          loop.firstFrameMs ??= now - loop.wall;
          if (media > loop.firstTime + 0.5) loop.progressing = true;
        }
        previous = { wall: now, media };
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
      // Frame callbacks can miss a presentation under load. Allow the same
      // few-frame tolerance at the beginning and end of the media.
      expect(loop.lastTime).toBeGreaterThan(data.duration - 0.15);
      expect(loop.firstTime).toBeLessThan(0.15);
      // The background fallback restarts a decoder that has reached EOF.
      // Keep the visible transition budget on the foreground path, and
      // check that both paths advance promptly after their first frame.
      if (!background) expect(loop.gapMs).toBeLessThan(250);
      expect(loop.firstFrameMs).toBeLessThan(200);
    }
    expect(data.events).not.toContain("emptied");
    expect(data.events.filter((event) => event === "seeking").length).toBe(
      data.loops.length,
    );
    if (background) expect(data.events).toContain("ended");
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
