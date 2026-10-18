import type { Page } from "@playwright/test";
import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

async function capabilities(page: Page, hdrDisplay: boolean, hevc = true) {
  await page.addInitScript(
    ({ hdrDisplay, hevc }) => {
      const matchMedia = window.matchMedia.bind(window);
      window.matchMedia = (query) => {
        const result = matchMedia(query);
        // Safari currently uses dynamic-range. Leave video-dynamic-range
        // unsupported so this also exercises the production Safari fallback.
        if (query.includes("dynamic-range")) {
          Object.defineProperty(result, "matches", {
            value: query === "(dynamic-range: high)" && hdrDisplay,
          });
        }
        return result;
      };
      for (const engine of [
        window.MediaSource,
        "ManagedMediaSource" in window ? window.ManagedMediaSource : undefined,
      ]) {
        if (
          typeof engine === "function" &&
          "isTypeSupported" in engine &&
          typeof engine.isTypeSupported === "function"
        ) {
          const supports = engine.isTypeSupported;
          engine.isTypeSupported = (type: string) =>
            type.includes("hvc1.2.4.L153.B0")
              ? hevc
              : supports.call(engine, type);
        }
      }
      localStorage.setItem(
        "stash-player-quality",
        '{"kind":"fixed","resolution":"FULL_HD"}',
      );
    },
    { hdrDisplay, hevc },
  );
}

for (const variant of [
  { name: "HDR display", display: true, hevc: true, hdr: true },
  { name: "SDR display", display: false, hevc: true, hdr: false },
  {
    name: "HDR display without HEVC decoding",
    display: true,
    hevc: false,
    hdr: false,
  },
]) {
  test(`${variant.name} has one automatic rendition per quality`, async ({
    page,
  }) => {
    await capabilities(page, variant.display, variant.hevc);
    await serveSceneMedia(page);
    const manifest = page.waitForRequest((request) =>
      new URL(request.url()).pathname.endsWith(
        `/stream${variant.hdr ? ".hevc" : ""}.master.m3u8`,
      ),
    );
    await page.goto("/scene-detail?hdr&short");
    expect(new URL((await manifest).url()).searchParams.get("resolution")).toBe(
      "FULL_HD",
    );
    const player = page.locator("[data-scene-player]");
    await expect(player).toHaveAttribute("data-playback-ready", "true");
    await page.getByRole("button", { name: "Quality", exact: true }).tap();
    const items = page.getByRole("menuitemradio");
    await expect(items).toHaveCount(6);
    const prefix = variant.hdr ? "HLS (HDR)" : "HLS";
    await expect(
      page.getByRole("menuitemradio", {
        name: `${prefix} Full HD (1080p)`,
        exact: true,
      }),
    ).toHaveAttribute("aria-checked", "true");
    await expect(items).toHaveText([
      variant.hdr ? prefix : "HLS (transcode)",
      `${prefix} 4K (2160p)`,
      `${prefix} Full HD (1080p)`,
      `${prefix} HD (720p)`,
      `${prefix} Standard (480p)`,
      `${prefix} Low (240p)`,
    ]);
    if (variant.hdr)
      await page.screenshot({
        path: test.info().outputPath("hdr-quality-menu.png"),
        animations: "disabled",
      });
    const next = page.waitForRequest((request) => {
      const url = new URL(request.url());
      return (
        url.pathname.endsWith(
          `/stream${variant.hdr ? ".hevc" : ""}.master.m3u8`,
        ) && url.searchParams.get("resolution") === "STANDARD_HD"
      );
    });
    await page
      .getByRole("menuitemradio", { name: `${prefix} HD (720p)`, exact: true })
      .tap();
    await next;
    await expect(player).toHaveAttribute("data-playback-ready", "true");
    expect(
      await page.evaluate(() =>
        JSON.parse(localStorage.getItem("stash-player-quality") ?? "null"),
      ),
    ).toEqual({ kind: "fixed", resolution: "STANDARD_HD" });
  });
}

test("HDR decoding recovery remains selectable without duplicate qualities or player replacement", async ({
  page,
}) => {
  await capabilities(page, true);
  await serveSceneMedia(page);
  await page.goto("/scene-detail?hdr&short");
  const player = page.locator("[data-scene-player]");
  await expect(player).toHaveAttribute("data-playback-ready", "true");
  const video = page.locator("video[data-playback-video]");
  const identity = await video.elementHandle();
  const fallback = page.waitForRequest((request) => {
    const url = new URL(request.url());
    return (
      url.pathname.endsWith("/stream.master.m3u8") &&
      url.searchParams.get("resolution") === "FULL_HD"
    );
  });
  await video.evaluate((element: HTMLVideoElement) => {
    Object.defineProperty(element, "error", {
      configurable: true,
      value: { code: 3, message: "Synthetic HDR decoding failure" },
    });
    element.dispatchEvent(new Event("error"));
    Reflect.deleteProperty(element, "error");
  });
  await fallback;
  await expect(player).toHaveAttribute("data-playback-ready", "true");
  expect(
    await identity?.evaluate(
      (element) =>
        element === document.querySelector("video[data-playback-video]"),
    ),
  ).toBe(true);
  await page.getByRole("button", { name: "Quality", exact: true }).tap();
  await expect(page.getByRole("menuitemradio")).toHaveCount(6);
  await expect(
    page.getByRole("menuitemradio", {
      name: "HLS Full HD (1080p)",
      exact: true,
    }),
  ).toHaveAttribute("aria-checked", "true");
  await expect(
    page.getByRole("menuitemradio", {
      name: "HLS (HDR) Full HD (1080p)",
      exact: true,
    }),
  ).toHaveCount(0);
});

test("TV uses the same automatic HDR quality choices", async ({ page }) => {
  await capabilities(page, true);
  await serveSceneMedia(page);
  await page.goto("/tv-fixture/tv?paused&full-hd&hdr");
  await expect(page.locator("[data-scene-player]")).toHaveAttribute(
    "data-playback-ready",
    "true",
  );
  await page.getByRole("button", { name: "Playback", exact: true }).tap();
  await page.getByRole("menuitem", { name: "Quality", exact: true }).tap();
  await page.getByRole("combobox", { name: "Current item quality" }).tap();
  await expect(page.getByRole("option")).toHaveCount(6);
  await expect(
    page.getByRole("option", {
      name: "HLS (HDR) Full HD (1080p)",
      exact: true,
    }),
  ).toHaveAttribute("aria-selected", "true");
  const next = page.waitForRequest((request) => {
    const url = new URL(request.url());
    return (
      url.pathname.endsWith("/stream.hevc.master.m3u8") &&
      url.searchParams.get("resolution") === "LOW"
    );
  });
  await page
    .getByRole("option", { name: "HLS (HDR) Low (240p)", exact: true })
    .tap();
  await next;
});
