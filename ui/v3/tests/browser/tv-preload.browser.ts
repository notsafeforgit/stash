import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

for (const count of [1, 3, 5]) {
  for (const markers of [false, true]) {
    test(`${count} prepared HLS ${markers ? "markers" : "scenes"} retain the window and release on exit`, async ({
      page,
    }) => {
      await serveSceneMedia(page);
      const alive = new Map<string, { scene: string; start: string | null }>();
      const warmed = new Set<string>();
      const released = new Set<string>();
      page.on("request", (request) => {
        const url = new URL(request.url());
        const session = url.searchParams.get("stream_session");
        if (!session) return;
        if (url.pathname.endsWith("/stream.master.m3u8")) {
          const scene = /\/scene\/(\d+)\//.exec(url.pathname)?.[1];
          if (scene)
            alive.set(session, { scene, start: url.searchParams.get("start") });
        } else if (url.pathname.includes("/media/hls/segment-"))
          warmed.add(session);
        else if (
          url.pathname.endsWith("/streams.stop") &&
          !url.searchParams.has("keep_type")
        ) {
          alive.delete(session);
          released.add(session);
        }
      });
      await page.goto(
        `/tv-fixture/tv?activity&preload=${count}&low&long-marker${markers ? "&markers" : ""}`,
      );
      const player = page.locator("[data-scene-player]");
      await expect(player).toHaveAttribute("data-playback-ready", "true");
      const radius = (count - 1) / 2;
      await expect.poll(() => alive.size).toBe(radius + 1);
      await expect
        .poll(() => [...alive.keys()].every((id) => warmed.has(id)))
        .toBe(true);
      if (markers && radius > 0)
        expect([...alive.values()].every((item) => item.scene === "1")).toBe(
          true,
        );
      const first = [...alive.keys()][0];
      if (!first) throw new Error("Missing current session");
      for (let selected = 1; selected <= 3; selected++) {
        await page.keyboard.press("ArrowDown");
        const scene = Math.floor(selected / 3) + 1;
        const key = markers
          ? `marker:${scene * 10 + (selected % 3)}`
          : `scene:${selected + 1}`;
        await expect(player).toHaveAttribute(
          "data-playback-key",
          new RegExp(`${key}$`),
        );
        await expect(player).toHaveAttribute("data-playback-ready", "true");
        await expect
          .poll(() => alive.size)
          .toBe(Math.min(selected, radius) + radius + 1);
        await expect
          .poll(() => [...alive.keys()].every((id) => warmed.has(id)))
          .toBe(true);
        expect(alive.has(first)).toBe(selected <= radius);
        await expect(page.locator("video")).toHaveCount(1);
      }
      expect(released.has(first)).toBe(true);
      const retained = new Set(alive.keys());
      await page.keyboard.press("ArrowUp");
      await expect(player).toHaveAttribute(
        "data-playback-key",
        markers ? /marker:12$/ : /scene:3$/,
      );
      await expect(player).toHaveAttribute("data-playback-ready", "true");
      await expect.poll(() => alive.size).toBe(count);
      if (count > 1)
        await expect
          .poll(() => [...alive.keys()].filter((id) => retained.has(id)).length)
          .toBe(count - 1);

      await page
        .getByRole("button", { name: "Navigation", exact: true })
        .click();
      await page.getByRole("link", { name: "Scenes", exact: true }).click();
      await expect(page.locator("[data-tv]")).toHaveCount(0);
      await expect.poll(() => alive.size).toBe(0);
      const activity = await page.evaluate(() =>
        window.tvFixtureRequests.filter((request) =>
          /Scene(SaveActivity|AddPlay)/.test(request.name),
        ),
      );
      if (markers) expect(activity).toHaveLength(0);
      else {
        expect(activity.length).toBeGreaterThan(0);
        // Only scenes actually selected may write activity; scenes 5/6 were
        // prepared but never watched during this traversal.
        for (const request of activity)
          expect(request.variables).toMatchObject({
            id: expect.stringMatching(/^[1-4]$/),
          });
      }
    });
  }
}

test("rapid scrolling followed by exit cannot start late prepared streams", async ({
  page,
}) => {
  await serveSceneMedia(page);
  const sessions = new Set<string>();
  const released = new Set<string>();
  const late: string[] = [];
  let exited = false;
  page.on("request", (request) => {
    const url = new URL(request.url());
    const session = url.searchParams.get("stream_session");
    if (!session) return;
    if (url.pathname.endsWith("/stream.master.m3u8")) {
      sessions.add(session);
      if (exited) late.push(session);
    }
    if (
      url.pathname.endsWith("/streams.stop") &&
      !url.searchParams.has("keep_type")
    )
      released.add(session);
  });
  await page.goto("/tv-fixture/tv?preload=5&slow&low");
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
  for (let i = 0; i < 6; i++) await page.keyboard.press("ArrowDown");
  await page.getByRole("button", { name: "Navigation", exact: true }).click();
  await page.getByRole("link", { name: "Scenes", exact: true }).click();
  await expect(page.locator("[data-tv]")).toHaveCount(0);
  exited = true;
  // Wait beyond the fixture's delayed metadata so late continuations run.
  await page.waitForTimeout(750);
  expect(late).toEqual([]);
  expect([...sessions].every((id) => released.has(id))).toBe(true);
});

test("direct files prepare adjacent loads without extra visible players or transcodes", async ({
  page,
}) => {
  await serveSceneMedia(page);
  const loaded = new Set<string>();
  const transcodes: string[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.pathname.endsWith("/stream")) loaded.add(url.pathname);
    if (/streams\.(keepalive|stop)$|\.m3u8$/.test(url.pathname))
      transcodes.push(url.pathname);
  });
  await page.goto("/tv-fixture/tv?paused&preload=5");
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
  await expect.poll(() => loaded.size).toBe(3);
  await page.keyboard.press("ArrowDown");
  await expect.poll(() => loaded.size).toBe(4);
  await page.keyboard.press("ArrowDown");
  await expect.poll(() => loaded.size).toBe(5);
  await expect(page.locator("video")).toHaveCount(1);
  expect(transcodes).toEqual([]);
});

test("prepared-video capacity saves immediately with localized descriptive options", async ({
  page,
}) => {
  await page.goto("/tv-fixture/settings/tv?preload=5");
  const capacity = page.getByRole("combobox", {
    name: "Prepared videos",
    exact: true,
  });
  await expect(capacity).toContainText("5 — current, 2 before and 2 after");
  for (const [label, count] of [
    ["3 — current, 1 before and 1 after", 3],
    ["1 — current video only", 1],
  ] as const) {
    await capacity.click();
    await page.getByRole("option", { name: label, exact: true }).click();
    await expect(capacity).toContainText(label);
    await expect
      .poll(() =>
        page.evaluate(() =>
          window.tvFixtureRequests
            .filter((request) => request.name === "ConfigureUISetting")
            .at(-1),
        ),
      )
      .toMatchObject({
        variables: { key: "tv", value: { preloadCount: count } },
      });
  }
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});
