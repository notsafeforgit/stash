import { test, expect, type Page } from "@playwright/test";
import type { OfflineEntry } from "@/components/offline/offline-db";

async function install(page: Page, prefix: string) {
  const response = await page.goto(`${prefix}offline.html`);
  expect(response?.headers()["content-security-policy"]).toContain(
    "worker-src blob: 'self'",
  );
  await expect(page.locator("[data-offline-launch]")).toBeVisible();
  await page.evaluate(async (prefix) => {
    await navigator.serviceWorker.register(`${prefix}service-worker.js`, {
      scope: prefix,
    });
    await navigator.serviceWorker.ready;
    if (!navigator.serviceWorker.controller)
      await new Promise<void>((resolve) =>
        navigator.serviceWorker.addEventListener(
          "controllerchange",
          () => resolve(),
          { once: true },
        ),
      );
  }, prefix);
}

async function readEntry(page: Page, id: string) {
  return page.evaluate(async (id): Promise<OfflineEntry | undefined> => {
    const db = await new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open(`stash-offline:v1:${location.origin}/`);
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
    return new Promise((resolve, reject) => {
      const request = db
        .transaction("offline_scenes")
        .objectStore("offline_scenes")
        .get(id);
      request.onsuccess = () => {
        resolve(request.result);
        db.close();
      };
      request.onerror = () => {
        reject(request.error);
        db.close();
      };
    });
  }, id);
}

for (const prefix of ["/", "/stash/"]) {
  test(`cold offline launch preserves the deployment base ${prefix}`, async ({
    page,
    context,
    baseURL,
  }) => {
    await install(page, prefix);
    await context.setOffline(true);
    const response = await page.goto(`${prefix}performers/123`);
    expect(response?.headers()["content-security-policy"]).toContain(
      "worker-src blob: 'self'",
    );
    expect(response?.headers()["referrer-policy"]).toBe("same-origin");
    await expect(
      page.getByRole("heading", { name: "Offline library" }),
    ).toBeVisible();
    expect(await page.evaluate(() => document.baseURI)).toBe(
      `${baseURL}${prefix}`,
    );
    await expect(
      page.getByText("No saved videos", { exact: true }),
    ).toBeVisible();
  });
}

test("browser downloads finish with the app closed and play on an offline cold launch", async ({
  page,
  context,
  browserName,
}) => {
  test.skip(
    browserName !== "chromium",
    "Native Background Fetch is exercised in Chromium; playback also requires its media codecs.",
  );
  await install(page, "/");
  await page.evaluate(async () => {
    const db = await new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open(`stash-offline:v1:${location.origin}/`);
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
    const digest = await crypto.subtle.digest(
      "SHA-256",
      new TextEncoder().encode(`${location.origin}/`),
    );
    const directory = [...new Uint8Array(digest)]
      .map((byte) => byte.toString(16).padStart(2, "0"))
      .join("");
    const tx = db.transaction("offline_scenes", "readwrite");
    for (const sceneId of ["900000001", "900000002"]) {
      const entry: OfflineEntry = {
        scene_id: sceneId,
        request_id: crypto.randomUUID(),
        queued_at: Number(sceneId),
        title: `Fixture ${sceneId}`,
        studio_name: null,
        studio_id: null,
        performers: [],
        tags: [],
        duration: 1,
        width: 64,
        height: 36,
        date: null,
        paths: { screenshot: null, preview: null, sprite: null, vtt: null },
        format: "copy",
        source_video_codec: "h264",
        source_audio_codec: "aac",
        resolution: "STANDARD",
        width_actual: 64,
        height_actual: 36,
        bytes: 0,
        downloaded_at: 0,
        status: "queued",
        opfs_path: `stash-offline/${directory}/scenes/${sceneId}.mp4`,
        server_status: "unknown",
      };
      tx.objectStore("offline_scenes").put(entry);
    }
    await new Promise<void>((resolve, reject) => {
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
    });
    db.close();
  });
  await page.reload();
  await expect
    .poll(async () => (await readEntry(page, "900000001"))?.background_fetch_id)
    .toBeTruthy();
  // Keep the browser profile, close every app window before completion.
  await page.close();
  // Both deliberately slow fixture transfers finish while no page is open.
  await new Promise((resolve) => setTimeout(resolve, 3500));
  const reopened = await context.newPage();
  await reopened.goto("/offline.html");
  await expect
    .poll(async () => (await readEntry(reopened, "900000001"))?.status)
    .toBe("complete");
  await expect
    .poll(async () => (await readEntry(reopened, "900000002"))?.status)
    .toBe("complete");
  // A second deployment must not expose the first deployment's saved library.
  await install(reopened, "/stash/");
  await expect(
    reopened.getByText("No saved videos", { exact: true }),
  ).toBeVisible();
  await context.setOffline(true);
  await reopened.goto("/performers/123");
  await reopened
    .getByRole("button", { name: "Fixture 900000001", exact: true })
    .click();
  const video = reopened.locator("video");
  await expect(video).toBeVisible();
  await video.evaluate((element) => {
    if (!(element instanceof HTMLVideoElement))
      throw new Error("Missing video");
    return element.play();
  });
  await expect
    .poll(() =>
      video.evaluate((element) =>
        element instanceof HTMLVideoElement ? element.currentTime : 0,
      ),
    )
    .toBeGreaterThan(0);
  expect(
    await reopened.evaluate(() => navigator.mediaSession.metadata?.title),
  ).toBe("Fixture 900000001");
  if (await reopened.evaluate(() => document.pictureInPictureEnabled)) {
    await reopened
      .getByRole("button", { name: "Enter picture-in-picture", exact: true })
      .click();
    await expect
      .poll(() => reopened.evaluate(() => !!document.pictureInPictureElement))
      .toBe(true);
    await reopened.evaluate(() => document.exitPictureInPicture());
  }
  await reopened.getByRole("button", { name: "Back", exact: true }).click();
  expect(
    await reopened.evaluate(() => navigator.mediaSession.metadata),
  ).toBeNull();
});
