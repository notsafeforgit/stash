import { describe, expect, it, vi, afterEach } from "vitest";
import {
  shareTarget,
  shareDeadline,
  shareRequest,
  shareStatusSchema,
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
