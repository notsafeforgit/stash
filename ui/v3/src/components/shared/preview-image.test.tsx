// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PreviewImage, type PreviewImageData } from "./preview-image";
import { PreviewImageDynamicRange } from "@/core/generated-graphql";

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});

afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  vi.unstubAllGlobals();
});

function preview(dynamicRange: PreviewImageDynamicRange): PreviewImageData {
  return {
    fallback: "/tone-mapped.jpg",
    sources: [
      {
        url: "/hdr.avif",
        mime_type: "image/avif",
        dynamic_range: dynamicRange,
        width: 1920,
        height: 1080,
      },
    ],
  };
}

describe("preview image rendering", () => {
  it("renders a complete preview without any legacy screenshot URL", async () => {
    await act(async () =>
      root.render(
        <PreviewImage
          preview={preview(PreviewImageDynamicRange.Adaptive)}
          alt=""
        />,
      ),
    );
    expect(container.querySelector("img")?.getAttribute("src")).toBe(
      "/tone-mapped.jpg",
    );
    expect(container.querySelector("source")?.srcset).toBe("/hdr.avif");
  });
  it("uses adaptive AVIF on either display with an SDR fallback", async () => {
    await act(async () =>
      root.render(
        <PreviewImage
          preview={preview(PreviewImageDynamicRange.Adaptive)}
          src="/legacy.jpg"
          alt="Scene cover"
        />,
      ),
    );
    const source = container.querySelector("source");
    expect(source?.getAttribute("type")).toBe("image/avif");
    expect(source?.hasAttribute("media")).toBe(false);
    expect(container.querySelector("img")?.getAttribute("src")).toBe(
      "/tone-mapped.jpg",
    );
    expect(container.querySelector("img")?.alt).toBe("Scene cover");
  });

  it("restricts plain HDR to HDR displays", async () => {
    await act(async () =>
      root.render(
        <PreviewImage
          preview={preview(PreviewImageDynamicRange.Hdr)}
          src="/legacy.jpg"
          alt=""
        />,
      ),
    );
    expect(container.querySelector("source")?.media).toBe(
      "(dynamic-range: high)",
    );
  });

  it("recovers from failed AVIF and deleted renditions, and resets for a new cover", async () => {
    const onError = vi.fn();
    const render = async (fallback: string) => {
      await act(async () =>
        root.render(
          <PreviewImage
            preview={{
              ...preview(PreviewImageDynamicRange.Adaptive),
              fallback,
            }}
            src="/legacy.jpg"
            alt=""
            onError={onError}
          />,
        ),
      );
    };
    const fail = async () => {
      await act(async () =>
        container.querySelector("img")?.dispatchEvent(new Event("error")),
      );
    };
    await render("/tone-mapped.jpg");
    const image = container.querySelector("img");
    if (!image) throw new Error("Missing preview image");
    Object.defineProperty(image, "currentSrc", {
      configurable: true,
      value: "http://localhost/hdr.avif",
    });
    await fail();
    expect(container.querySelector("source")).toBeNull();
    expect(container.querySelector("img")?.getAttribute("src")).toBe(
      "/tone-mapped.jpg",
    );
    Object.defineProperty(image, "currentSrc", {
      configurable: true,
      value: "",
    });
    await fail();
    expect(container.querySelector("img")?.getAttribute("src")).toBe(
      "/legacy.jpg",
    );
    await fail();
    expect(onError).toHaveBeenCalledOnce();
    await render("/new-cover.jpg");
    expect(container.querySelector("source")).not.toBeNull();
  });

  it("falls straight through when the browser's JPEG choice fails", async () => {
    await act(async () =>
      root.render(
        <PreviewImage
          preview={preview(PreviewImageDynamicRange.Hdr)}
          src="/legacy.jpg"
          alt=""
        />,
      ),
    );
    await act(async () =>
      container.querySelector("img")?.dispatchEvent(new Event("error")),
    );
    expect(container.querySelector("img")?.getAttribute("src")).toBe(
      "/legacy.jpg",
    );
    expect(container.querySelector("source")).toBeNull();
  });
});
