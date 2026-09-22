import { describe, expect, it, vi, afterEach } from "vitest";
import {
  shareTarget,
  shareDeadline,
  shareRequest,
  shareStatusSchema,
  shareDetailSchema,
  ShareUnavailableError,
} from "./share-contract";

afterEach(() => vi.unstubAllGlobals());

describe("share capabilities in the browser", () => {
  const id = "abcdefghijklmnopqrstuv";
  it("keeps the capability in a fragment and resolves mounted share paths", () => {
    const target = shareTarget(
      `https://share.test/mounted/share/${id}/#secret`,
      "https://share.test/mounted/share/",
    );
    expect(target?.base.href).toBe(`https://share.test/mounted/share/${id}/`);
    expect(target?.secret).toBe("secret");
    expect(
      shareTarget(
        `https://other.test/share/${id}/`,
        "https://share.test/share/",
      ),
    ).toBeNull();
    expect(
      shareTarget(
        "https://share.test/share/../graphql",
        "https://share.test/share/",
      ),
    ).toBeNull();
    expect(
      shareTarget(
        `https://share.test/share/${id}/media/image-1/`,
        "https://share.test/share/",
      ),
    ).toBeNull();
  });
  it("uses server time to expire when the recipient clock is wrong", () => {
    expect(
      shareDeadline(
        {
          server_time: "2026-01-01T01:00:00Z",
          expires_at: "2026-01-01T01:01:00Z",
        },
        100,
      ),
    ).toBe(60_100);
    expect(
      shareDeadline(
        {
          server_time: "2026-01-01T01:01:00Z",
          expires_at: "2026-01-01T01:00:00Z",
        },
        100,
      ),
    ).toBe(100);
  });
  it("validates responses and never follows redirects to authentication or other origins", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        new Response(
          JSON.stringify({ expires_at: "invalid", server_time: "invalid" }),
        ),
      );
    vi.stubGlobal("fetch", fetcher);
    const url = new URL(`https://share.test/share/${id}/status`);
    await expect(shareRequest(url, shareStatusSchema)).rejects.toThrow();
    expect(fetcher).toHaveBeenCalledWith(
      url,
      expect.objectContaining({
        credentials: "same-origin",
        cache: "no-store",
        redirect: "error",
      }),
    );
    fetcher.mockResolvedValue(new Response("unavailable", { status: 404 }));
    await expect(shareRequest(url, shareStatusSchema)).rejects.toBeInstanceOf(
      ShareUnavailableError,
    );
  });
});

it("accepts scoped HDR renditions and rejects external preview URLs", () => {
  const preview = {
    fallback: "/share/example/media/scene-1/preview-image/cover.jpg",
    sources: [
      {
        url: "/share/example/media/scene-1/preview-image/cover.avif",
        mime_type: "image/avif",
        dynamic_range: "HDR",
        width: 640,
        height: 360,
      },
    ],
  };
  const detail = {
    media: {
      key: "scene-1",
      kind: "SCENE",
      title: "",
      width: 640,
      height: 360,
      duration: 1,
      video: true,
      thumbnail: "/thumbnail",
      image: "/image",
      download: "",
      preview_image: { ...preview, thumbnail: preview },
    },
    video_codec: "h264",
    audio_codec: "",
    frame_rate: 30,
    streams: [],
  };
  expect(shareDetailSchema.parse(detail).media.preview_image).toEqual(
    detail.media.preview_image,
  );
  for (const url of [
    "https://owner.test/secret",
    "//owner.test/secret",
    "/\\owner.test/secret",
  ]) {
    expect(
      shareDetailSchema.safeParse({
        ...detail,
        media: {
          ...detail.media,
          preview_image: { ...preview, fallback: url },
        },
      }).success,
    ).toBe(false);
    expect(
      shareDetailSchema.safeParse({
        ...detail,
        media: {
          ...detail.media,
          preview_image: {
            ...preview,
            sources: [{ ...preview.sources[0], url }],
          },
        },
      }).success,
    ).toBe(false);
  }
});
