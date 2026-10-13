import { test, expect, holdForContextMenu } from "./test";
import { DisplayMode } from "../../src/models/list-filter/types";
import { serveSceneMedia } from "./scene-media";

test("long press on a marker thumbnail opens its actions without opening playback", async ({
  page,
  browserName,
}) => {
  await page.goto("/scenes/markers");
  const card = page.locator(".marker-card").first();
  const thumbnail = card.locator("[data-entity-card-preview] img").first();
  await expect(thumbnail).toBeVisible();
  const menu = page.getByRole("menu");
  if (browserName === "chromium") {
    // Real browser touch input includes the pointer and compatibility events
    // that a synthesized touchstart cannot exercise.
    const session = await page.context().newCDPSession(page);
    const bounds = await thumbnail.boundingBox();
    if (!bounds) throw new Error("Missing marker thumbnail");
    await session.send("Input.dispatchTouchEvent", {
      type: "touchStart",
      touchPoints: [
        { x: bounds.x + bounds.width / 2, y: bounds.y + bounds.height / 2 },
      ],
    });
    await expect(menu).toBeVisible();
    await session.send("Input.dispatchTouchEvent", {
      type: "touchEnd",
      touchPoints: [],
    });
    await session.detach();
  } else {
    await holdForContextMenu(thumbnail, menu);
  }
  await expect(
    menu.getByRole("menuitem", { name: "Select", exact: true }),
  ).toBeVisible();
  await expect(
    menu.getByRole("menuitem", { name: "Edit…", exact: true }),
  ).toBeVisible();
  await expect(
    menu.getByRole("menuitem", { name: "Delete…", exact: true }),
  ).toBeVisible();
  await expect(page).toHaveURL(/\/scenes\/markers$/);
  await expect(page.locator(".yarl__root")).toHaveCount(0);
  await menu.getByRole("menuitem", { name: "Select", exact: true }).tap();
  await expect(card).toHaveAttribute("data-selected", "true");
  await expect(menu).toBeHidden();
});

test("marker thumbnails and card links do not become browser drag sources", async ({
  page,
}) => {
  await page.goto("/scenes/markers");
  const card = page.locator(".marker-card").first();
  const thumbnail = card.locator("[data-entity-card-preview] img").first();
  await expect(thumbnail).toBeVisible();
  await expect(thumbnail).toHaveAttribute("draggable", "false");
  await expect(card.locator("[data-card-link]")).toHaveAttribute(
    "draggable",
    "false",
  );
  expect(
    await thumbnail.evaluate((element) =>
      getComputedStyle(element).getPropertyValue("-webkit-user-drag"),
    ),
  ).toBe("none");
});

test("moving or cancelling a marker hold leaves scrolling and ordinary taps available", async ({
  page,
}) => {
  await serveSceneMedia(page);
  await page.clock.install();
  await page.goto("/scenes/markers");
  const thumbnail = page
    .locator(".marker-card [data-entity-card-preview] img")
    .first();
  await expect(thumbnail).toBeVisible();
  for (const kind of ["touchmove", "touchcancel"] as const) {
    await thumbnail.evaluate((element, kind) => {
      const rect = element.getBoundingClientRect();
      const fire = (type: string, y: number) => {
        const event = new Event(type, { bubbles: true });
        Object.defineProperty(event, "touches", {
          value: [{ clientX: rect.x + rect.width / 2, clientY: y }],
        });
        element.dispatchEvent(event);
      };
      fire("touchstart", rect.y + rect.height / 2);
      fire(kind, rect.y + rect.height / 2 + 40);
    }, kind);
    await page.clock.runFor(700);
    await expect(page.getByRole("menu")).toHaveCount(0);
    await thumbnail.dispatchEvent("touchend", { touches: [] });
  }
  await thumbnail.tap();
  await expect(page.locator(".yarl__portal")).toBeVisible();
});

for (const layout of ["grid", "table"] as const) {
  test(`marker ${layout} menu confirms deletion of only the marker`, async ({
    page,
  }) => {
    if (layout === "table") {
      await page.addInitScript((mode) => {
        localStorage.setItem("list-display-mode:scene_markers", String(mode));
      }, DisplayMode.Table);
    }
    await page.goto("/scenes/markers");
    const items = page.locator(
      layout === "table" ? "tbody tr" : ".marker-card",
    );
    await expect(items).toHaveCount(3);
    const menu = page.getByRole("menu");
    await holdForContextMenu(items.first(), menu);
    await menu.getByRole("menuitem", { name: "Delete…", exact: true }).tap();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("checkbox")).toHaveCount(0);
    await dialog.getByRole("button", { name: "Cancel", exact: true }).tap();
    expect(await page.evaluate(() => window.markerListMutations)).toEqual([]);
    await expect(items).toHaveCount(3);
    await holdForContextMenu(items.first(), menu);
    await menu.getByRole("menuitem", { name: "Delete…", exact: true }).tap();
    await dialog.getByRole("button", { name: "Delete", exact: true }).tap();
    await expect(items).toHaveCount(2);
    await expect(dialog).toBeHidden();
    expect(await page.evaluate(() => window.markerListMutations)).toEqual([
      {
        operation: "SceneMarkerDestroy",
        id: "00000000-0000-4000-8000-000000000001",
      },
    ]);
  });
}

test("desktop marker context menu opens the marker's parent scene at its timestamp", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 844 });
  await page.goto("/scenes/markers");
  const card = page.locator(".marker-card").first();
  const href = await card.locator("[data-card-link]").getAttribute("href");
  if (!href) throw new Error("Missing marker destination");
  await card.click({ button: "right" });
  const menu = page.getByRole("menu");
  await expect(menu).toBeVisible();
  await menu.getByRole("menuitem", { name: "Edit…", exact: true }).click();
  await expect(page).toHaveURL(new URL(href, page.url()).href);
  await expect(page).toHaveURL(/tab=markers/);
  await expect(page).toHaveURL(/t=6/);
});
