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

test("snapshot galleries page independently and Back retains the loaded overview", async ({
  page,
}) => {
  const base = "/share/abcdefghijklmnopqrstuv/";
  const gallery = `gallery-${libraryID(500)}`;
  const stamp = {
    server_time: new Date().toISOString(),
    expires_at: new Date(Date.now() + 3600000).toISOString(),
  };
  const media = (id: number): SharedMedia => ({
    key: `image-${libraryID(id)}`,
    kind: "IMAGE",
    title: `Photo ${id}`,
    width: 64,
    height: 48,
    duration: 0,
    video: false,
    thumbnail: `${base}media/image-${libraryID(id)}/thumbnail`,
    image: `${base}media/image-${libraryID(id)}/image`,
    download: "",
  });
  const requests: { gallery: string | null; cursor: string | null }[] = [];
  await page.route("**/share/*/content**", (route) => {
    const params = new URL(route.request().url()).searchParams;
    const context = params.get("gallery");
    const cursor = params.get("cursor");
    requests.push({ gallery: context, cursor });
    const ids = context ? (cursor ? [3, 4] : [1, 2]) : cursor ? [3] : [1, 2];
    const response: SharedContent = {
      ...stamp,
      label: "Fixed selection",
      next: cursor ? "" : context ? "gallery-next" : "overview-next",
      media: ids.map(media),
      entries: [
        {
          key: gallery,
          kind: "GALLERY",
          title: "Large album",
          media_count: 4,
          media_keys: (context ? ids : [1]).map((id) => media(id).key),
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
  await page.getByRole("button", { name: "Load more", exact: true }).click();
  await expect(
    page.getByRole("tab", { name: "Images (3)", exact: true }),
  ).toBeVisible();
  await page.getByRole("tab", { name: "Galleries (1)", exact: true }).click();
  const card = page.locator(`article[data-id="${gallery}"]`);
  await expect(card.getByText("4 items", { exact: true })).toBeVisible();
  await card.getByText("Large album", { exact: true }).click();
  await expect(
    page.locator(`article[data-id="${media(2).key}"]`),
  ).toBeVisible();
  await page.getByRole("button", { name: "Load more", exact: true }).click();
  for (const id of [1, 2, 3, 4])
    await expect(
      page.locator(`article[data-id="${media(id).key}"]`),
    ).toBeVisible();
  await page.getByRole("button", { name: "Back", exact: true }).click();
  await expect(
    page.getByRole("tab", { name: "Images (3)", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Load more", exact: true }),
  ).toHaveCount(0);
  expect(requests).toContainEqual({ gallery, cursor: "gallery-next" });
  expect(requests.filter((item) => !item.gallery)).toHaveLength(2);
});
