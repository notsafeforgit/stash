import { test, expect } from "./test";
import { serveSceneMedia } from "./scene-media";

for (const kind of ["image", "scene"] as const) {
  for (const width of [390, 1280]) {
    test(`${kind} dismissal returns to the retained list at ${width}px without refetching`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 844 });
      await serveSceneMedia(page);
      await page.goto(`/lightbox-list?kind=${kind}&p=2`);
      await expect(page.locator("article[data-id]").first()).toBeVisible();
      const scroller = page.locator("[data-scroll-restoration-id]").last();
      await scroller.evaluate((element) => {
        element.scrollTop = 600;
      });
      await expect
        .poll(() => scroller.evaluate((el) => el.scrollTop))
        .toBe(600);
      const preview = page.locator("[data-entity-card-preview]").filter({
        visible: true,
      });
      const visibleCard = () =>
        preview.evaluateAll((elements) => {
          const element = elements.find((element) => {
            const rect = element.getBoundingClientRect();
            return rect.bottom > 160 && rect.top < innerHeight - 160;
          });
          return element?.closest("article")?.getAttribute("data-id");
        });
      await expect.poll(visibleCard).toBeTruthy();
      const id = await visibleCard();
      const card = page.locator(`article[data-id="${id}"]`);
      const original = await card.elementHandle();
      const url = page.url();
      const requests = await page.evaluate(() =>
        window.lightboxListRequests.filter((name) => /List/.test(name)),
      );
      for (const dismiss of ["close", "back", "escape"] as const) {
        // Dispatch on the visible part of a tall portrait card without
        // Playwright scrolling its center into view before the click.
        await card.locator("[data-entity-card-preview]").dispatchEvent("click");
        const portal = page.locator(".yarl__portal");
        await expect(portal).toBeVisible();
        if (kind === "image" && width >= 768) {
          const thumbnails = portal.locator(".yarl__thumbnails_thumbnail img");
          await expect(thumbnails.first()).toBeVisible();
          expect(
            await thumbnails.evaluateAll((images) =>
              images.every((image) =>
                image.getAttribute("src")?.endsWith("#thumbnail"),
              ),
            ),
          ).toBe(true);
        } else if (kind === "image") {
          await expect(
            portal.locator(".yarl__thumbnails_container"),
          ).toHaveCount(0);
        }
        await expect
          .poll(() => page.evaluate(() => !!history.state.stashV3Lightbox))
          .toBe(true);
        if (dismiss === "back") await page.goBack();
        else if (dismiss === "escape") await page.keyboard.press("Escape");
        else
          await portal
            .getByRole("button", { name: "Close", exact: true })
            .dispatchEvent("click");
        await expect(portal).toHaveCount(0);
        await expect(page).toHaveURL(url);
        await expect
          .poll(() => scroller.evaluate((element) => element.scrollTop))
          .toBe(600);
        expect(await original?.evaluate((element) => element.isConnected)).toBe(
          true,
        );
        expect(
          await page.evaluate(() =>
            window.lightboxListRequests.filter((name) => /List/.test(name)),
          ),
        ).toEqual(requests);
        expect(await page.evaluate(() => history.state.stashV3Lightbox)).toBe(
          undefined,
        );
        await expect(page.locator("[data-app-viewport]")).not.toHaveAttribute(
          "inert",
        );
        await expect(page.locator("video")).toHaveCount(0);
      }
    });
  }

  test(`${kind} consumes history during exit and cannot pop a quick reopen`, async ({
    page,
  }) => {
    await page.goto("/motion");
    await page.evaluate(() => {
      const back = history.back.bind(history);
      Object.assign(window, {
        releaseLightboxBack: back,
        lightboxBackCalls: 0,
        lightboxHistoryBeforeExit: false,
      });
      history.back = () => {
        window.lightboxBackCalls++;
      };
      const animate = Element.prototype.animate;
      Element.prototype.animate = function (...args) {
        const animation = animate.apply(this, args);
        if (animation.id === "lightbox-exit")
          window.lightboxHistoryBeforeExit = window.lightboxBackCalls === 1;
        return animation;
      };
    });
    const open = page.getByRole("button", {
      name: kind === "image" ? "Open images" : "Open scenes",
      exact: true,
    });
    await open.tap();
    const portal = page.locator(".yarl__portal");
    const close = portal.getByRole("button", { name: "Close", exact: true });
    await close.dispatchEvent("click");
    expect(await page.evaluate(() => window.lightboxHistoryBeforeExit)).toBe(
      true,
    );
    // Simulate an unusually slow browser traversal. Exit can finish first,
    // but must not remove the overlay and admit a new push ahead of that pop.
    await expect(portal.locator("[data-lightbox-reveal]")).toHaveCSS(
      "opacity",
      "1",
    );
    await close.dispatchEvent("click");
    await expect(page.getByTestId("close-count")).toHaveText("0");
    expect(await page.evaluate(() => window.lightboxBackCalls)).toBe(1);
    await page.evaluate(() => window.releaseLightboxBack());
    await expect(portal).toHaveCount(0);
    await expect(page.getByTestId("close-count")).toHaveText("1");
    await page.evaluate(() => {
      history.back = window.releaseLightboxBack;
    });
    await open.tap();
    await expect(portal).toBeVisible();
    await page.goBack();
    await expect(portal).toHaveCount(0);
    await expect(page.getByTestId("close-count")).toHaveText("2");
    await expect(page).toHaveURL(/\/motion$/);
  });
}

test("rotating a displayed image refreshes its thumbnail too", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 844 });
  await page.goto("/lightbox-list?kind=image");
  await page
    .locator("[data-entity-card-preview]")
    .first()
    .dispatchEvent("click");
  const portal = page.locator(".yarl__portal");
  await portal
    .getByRole("button", { name: "Rotate clockwise", exact: true })
    .click();
  const image = portal.locator(".yarl__slide_current img");
  await expect(image).toHaveAttribute("src", /#rotated$/);
  await expect(
    portal.locator(".yarl__thumbnails_thumbnail_active img"),
  ).toHaveAttribute("src", /#rotated$/);
});

declare global {
  interface Window {
    releaseLightboxBack: () => void;
    lightboxBackCalls: number;
    lightboxHistoryBeforeExit: boolean;
  }
}
