import { test, expect } from "./test";

const id = "00000000-0000-4000-8000-000000000001";

test("expanded deletion options fit a short mobile viewport", async ({
  page,
}, testInfo) => {
  await page.setViewportSize({ width: 390, height: 600 });
  await page.goto("/gallery-deletion.html");
  await page.getByRole("button", { name: "Delete gallery", exact: true }).tap();
  const dialog = page.getByRole("dialog");
  await dialog
    .getByRole("checkbox", {
      name: "Also delete the gallery’s images and scenes",
    })
    .check();
  const box = await dialog.boundingBox();
  expect(box).not.toBeNull();
  expect(box!.y).toBeGreaterThanOrEqual(15);
  expect(box!.height).toBeLessThanOrEqual(568);
  await dialog
    .getByRole("button", { name: "Delete", exact: true })
    .scrollIntoViewIfNeeded();
  await expect(
    dialog.getByRole("button", { name: "Delete", exact: true }),
  ).toBeInViewport();
  await page.screenshot({
    path: testInfo.outputPath("gallery-deletion-mobile.png"),
  });
});

test("gallery-only is the default and sends no media/file deletion flags", async ({
  page,
}) => {
  await page.goto("/gallery-deletion.html");
  await page.getByRole("button", { name: "Delete gallery", exact: true }).tap();
  const dialog = page.getByRole("dialog");
  await expect(
    dialog.getByText("Contains 3 images and 2 scenes (each counted once)."),
  ).toBeVisible();
  await expect(dialog.getByRole("checkbox")).toHaveCount(1);
  await expect(dialog.getByRole("checkbox")).not.toBeChecked();
  await dialog.getByRole("button", { name: "Delete", exact: true }).tap();
  await expect(dialog).toBeHidden();
  expect(await page.evaluate(() => window.galleryDeletionRequests)).toEqual([
    {
      ids: [id],
      delete_media: false,
      delete_shared_media: false,
      delete_file: false,
      delete_generated: false,
    },
  ]);
});

for (const shared of [false, true]) {
  test(`mixed-media deletion ${shared ? "explicitly includes" : "protects"} shared members`, async ({
    page,
  }) => {
    await page.goto("/gallery-deletion.html");
    await page
      .getByRole("button", { name: "Delete gallery", exact: true })
      .tap();
    const dialog = page.getByRole("dialog");
    await dialog
      .getByRole("checkbox", {
        name: "Also delete the gallery’s images and scenes",
      })
      .check();
    const sharedCheckbox = dialog.getByRole("checkbox", {
      name: "Also delete media used by other galleries",
    });
    await expect(sharedCheckbox).not.toBeChecked();
    await expect(
      dialog.getByText("Will delete 2 images and 1 scene from the library."),
    ).toBeVisible();
    if (shared) {
      await sharedCheckbox.check();
      await expect(
        dialog.getByText("Will delete 3 images and 2 scenes from the library."),
      ).toBeVisible();
    }
    await dialog
      .getByRole("checkbox", { name: "Also delete the media files from disk" })
      .check();
    await dialog.getByRole("button", { name: "Delete", exact: true }).tap();
    await expect(dialog).toBeHidden();
    expect(await page.evaluate(() => window.galleryDeletionRequests)).toEqual([
      {
        ids: [id],
        delete_media: true,
        delete_shared_media: shared,
        delete_file: true,
        delete_generated: true,
      },
    ]);
  });
}

test("cancel resets destructive choices and returning to gallery-only masks file options", async ({
  page,
}) => {
  await page.goto("/gallery-deletion.html");
  await page.getByRole("button", { name: "Delete gallery", exact: true }).tap();
  const dialog = page.getByRole("dialog");
  const media = dialog.getByRole("checkbox", {
    name: "Also delete the gallery’s images and scenes",
  });
  await media.check();
  await dialog
    .getByRole("checkbox", { name: "Also delete the media files from disk" })
    .check();
  await dialog.getByRole("button", { name: "Cancel", exact: true }).tap();
  await expect(dialog).toBeHidden();
  expect(await page.evaluate(() => window.galleryDeletionRequests)).toEqual([]);
  await page.getByRole("button", { name: "Delete gallery", exact: true }).tap();
  await expect(media).not.toBeChecked();
  await media.check();
  await expect(
    dialog.getByRole("checkbox", {
      name: "Also delete the media files from disk",
    }),
  ).not.toBeChecked();
  await dialog
    .getByRole("checkbox", { name: "Also delete the media files from disk" })
    .check();
  await media.uncheck();
  await dialog.getByRole("button", { name: "Delete", exact: true }).tap();
  expect(
    await page.evaluate(() => window.galleryDeletionRequests[0]),
  ).toMatchObject({
    delete_media: false,
    delete_file: false,
    delete_generated: false,
  });
});

test("a failed membership preview cannot authorize deletion", async ({
  page,
}) => {
  await page.goto("/gallery-deletion.html?fail=preview");
  await page.getByRole("button", { name: "Delete gallery", exact: true }).tap();
  const dialog = page.getByRole("dialog");
  await expect(
    dialog.getByText("Cannot load gallery membership"),
  ).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: "Delete", exact: true }),
  ).toBeDisabled();
  expect(await page.evaluate(() => window.galleryDeletionRequests)).toEqual([]);
});
