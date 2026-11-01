import {
  test as base,
  expect,
  type Locator,
  type Page,
} from "@playwright/test";

export { expect };

// Match the same module in the unbundled dev server and the prebuilt fixture.
// A source path disambiguates components from fixtures with the same basename.
export function fixtureModule(source: string) {
  const escapePattern = (value: string) =>
    value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const name = escapePattern(source.split("/").at(-1)!);
  return new RegExp(
    `/(?:${escapePattern(source)}\\.tsx|${name}\\.js|assets/lazy-${name}-[\\w-]+\\.js)(?:\\?.*)?$`,
  );
}

export const test = base.extend<{ expectedConsoleErrors: string[] }>({
  expectedConsoleErrors: [[], { option: true }],
  page: async ({ page, baseURL, expectedConsoleErrors }, use) => {
    if (!baseURL) throw new Error("Missing fixture URL");
    const origin = new URL(baseURL).origin;
    const failures: string[] = [];
    page.on("pageerror", (error) => failures.push(error.message));
    page.on("console", (message) => {
      if (
        message.type() === "error" &&
        !expectedConsoleErrors.some((expected) =>
          message.text().includes(expected),
        )
      )
        failures.push(message.text());
    });
    await page.route("**/*", async (route) => {
      const request = route.request();
      if (
        new URL(request.url()).origin !== origin ||
        request.method() !== "GET"
      ) {
        failures.push(
          `Unexpected fixture request: ${request.method()} ${request.url()}`,
        );
        await route.abort();
      } else {
        await route.continue();
      }
    });
    await use(page);
    expect(failures, "browser errors or unexpected network requests").toEqual(
      [],
    );
  },
});

export function detailFooter(page: Page) {
  return page.locator("[data-mobile-detail-footer]");
}

export async function expectCompactRow(row: Locator) {
  await expect(row).toBeVisible();
  await expect
    .poll(() =>
      row.evaluate((element) => element.getBoundingClientRect().height),
    )
    .toBe(57);
  await expect
    .poll(() =>
      row.evaluate((element) => {
        const rect = element.getBoundingClientRect();
        return Math.abs(rect.bottom - window.innerHeight);
      }),
    )
    .toBeLessThan(1);
  const buttons = row.getByRole("button");
  await expect
    .poll(() =>
      buttons.evaluateAll((elements) => {
        const boxes = elements.map((element) =>
          element.getBoundingClientRect(),
        );
        return boxes.every(
          (box, index) =>
            box.left >= 0 &&
            box.right <= window.innerWidth &&
            box.left >= (boxes[index - 1]?.right ?? 0) - 0.1,
        );
      }),
    )
    .toBe(true);
}

export async function expectTouchTargets(scope: Locator) {
  await expect
    .poll(() =>
      scope.getByRole("button").evaluateAll((buttons) =>
        buttons.every((button) => {
          const rect = button.getBoundingClientRect();
          return rect.width >= 43.9 && rect.height >= 43.9;
        }),
      ),
    )
    .toBe(true);
}

export async function chooseSection(page: Page, name: string) {
  const trigger = detailFooter(page).getByRole("button", {
    name: "Detail sections",
  });
  await trigger.click();
  await page.getByRole("tab", { name, exact: true }).click();
  await expect(trigger).toHaveAttribute("aria-expanded", "false");
  await expect(trigger).toHaveText(name);
}

/** Exercise Base UI's touch gesture in both engines (Playwright only exposes tap). */
export async function holdForContextMenu(
  trigger: Locator,
  menu: Locator,
  position = { x: 0.5, y: 0.5 },
) {
  await trigger.evaluate((element, position) => {
    const bounds = element.getBoundingClientRect();
    // WebKit doesn't expose a constructible Touch. Supply the coordinates that
    // the real primitive consumes without replacing its event handlers/timer.
    const touch = {
      clientX: bounds.x + bounds.width * position.x,
      clientY: bounds.y + bounds.height * position.y,
    } satisfies Pick<Touch, "clientX" | "clientY">;
    const event = new Event("touchstart", { bubbles: true });
    Object.defineProperty(event, "touches", { value: [touch] });
    element.dispatchEvent(event);
  }, position);
  await expect(menu).toBeVisible();
  await trigger.dispatchEvent("touchend", {
    touches: [],
    targetTouches: [],
    changedTouches: [],
  });
}
