import { test, expect } from "./test";
import { libraryID } from "../fixtures/library";
import type {
  SharedContent,
  SharedMedia,
} from "../../src/components/sharing/share-contract";

test("ongoing recipients can page, deduplicate galleries and refresh after deletion", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const base = "/share/abcdefghijklmnopqrstuv/";
  const media = (id: number): SharedMedia => ({
    key: `image-${libraryID(id)}`,
    kind: "IMAGE",
    title: `Image ${id}`,
    width: 64,
    height: 48,
    duration: 0,
    video: false,
    thumbnail: `${base}media/image-${libraryID(id)}/thumbnail`,
    image: `${base}media/image-${libraryID(id)}/image`,
    download: "",
  });
  const stamp = {
    server_time: new Date().toISOString(),
    expires_at: new Date(Date.now() + 3600_000).toISOString(),
  };
  let refreshed = false;
  await page.route("**/share/*/content**", (route) => {
    const next = new URL(route.request().url()).searchParams.get("cursor");
    const items = refreshed
      ? [media(2)]
      : next
        ? [media(2), media(3)]
        : [media(1), media(2)];
    const response: SharedContent = {
      ...stamp,
      label: "Ongoing performer",
      next: refreshed || next ? "" : "next-page",
      media: items,
      entries: [
        {
          key: `gallery-${libraryID(500)}`,
          kind: "GALLERY",
          title: "Album",
          media_keys: refreshed
            ? [media(2).key]
            : next
              ? [media(1).key, media(2).key, media(3).key]
              : items.map((item) => item.key),
        },
      ],
    };
    return route.fulfill({ json: response });
  });
  await page.route("**/share/*/status", (route) =>
    route.fulfill({ json: stamp }),
  );
  await page.route("**/share/*/media/*/thumbnail", (route) =>
    route.fulfill({
      contentType: "image/svg+xml",
      body: '<svg xmlns="http://www.w3.org/2000/svg" width="64" height="48"/>',
    }),
  );
  await page.goto("/share-recipient.html");
  await expect(
    page.getByRole("tab", { name: "Images (2)", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Load more", exact: true }).click();
  await expect(
    page.getByRole("tab", { name: "Images (3)", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("tab", { name: "Galleries (1)", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Load more", exact: true }),
  ).toHaveCount(0);
  refreshed = true;
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(
    page.getByRole("tab", { name: "Images (1)", exact: true }),
  ).toBeVisible();
  await expect(
    page.locator(`article[data-id="image-${libraryID(1)}"]`),
  ).toHaveCount(0);
  await expect(
    page.locator(`article[data-id="image-${libraryID(2)}"]`),
  ).toBeVisible();
});
