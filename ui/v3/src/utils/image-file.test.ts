import { afterEach, expect, it, vi } from "vitest";
import { copyImage, saveImage } from "./image-file";

afterEach(() => vi.unstubAllGlobals());

it("requests clipboard access during the click, before fetching finishes", async () => {
  let resolveFetch: ((response: Response) => void) | undefined;
  const fetch = vi.fn(
    () =>
      new Promise<Response>((resolve) => {
        resolveFetch = resolve;
      }),
  );
  class Item {
    constructor(readonly data: Record<string, Promise<Blob>>) {}
  }
  let copied: Blob | undefined;
  const write = vi.fn(async (items: Item[]) => {
    copied = await items[0]?.data["image/png"];
  });
  vi.stubGlobal("window", { isSecureContext: true });
  vi.stubGlobal("navigator", { clipboard: { write } });
  vi.stubGlobal("ClipboardItem", Item);
  vi.stubGlobal("fetch", fetch);

  const copying = copyImage({ src: "https://stash.test/image/original?t=2" });
  expect(write).toHaveBeenCalledOnce();
  expect(copied).toBeUndefined();
  resolveFetch?.(
    new Response("original PNG bytes", {
      headers: { "Content-Type": "image/png" },
    }),
  );
  await copying;
  expect(await copied?.text()).toBe("original PNG bytes");
  expect(fetch).toHaveBeenCalledWith("https://stash.test/image/original?t=2");
});

it("does not download an authentication page as an image", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response("<html>Sign in</html>", {
          headers: { "Content-Type": "text/html" },
        }),
    ),
  );
  await expect(
    saveImage({ src: "https://stash.test/image/original" }),
  ).rejects.toThrow("Not an image");
});

it("reports failed image requests instead of downloading an error response", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response("Not found", { status: 404 })),
  );
  await expect(
    saveImage({ src: "https://stash.test/image/missing" }),
  ).rejects.toThrow("Image request failed");
});
