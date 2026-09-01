import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import { IDENTITY_TRANSFORM, VideoFrameZoom } from "./video-frame-zoom";

function renderZoom(enabled: boolean) {
  return renderToStaticMarkup(
    <VideoFrameZoom
      enabled={enabled}
      transform={IDENTITY_TRANSFORM}
      onTransformChange={vi.fn()}
    >
      <video data-testid="stable-video" />
    </VideoFrameZoom>,
  );
}

describe("VideoFrameZoom", () => {
  it("keeps its wrapper mounted while gesture ownership is disabled", () => {
    const markup = renderZoom(false);

    expect(markup).toContain('data-video-frame-zoom=""');
    expect(markup).toContain('data-testid="stable-video"');
    expect(markup).not.toContain("data-pinch-zoom-allowed");
    expect(markup).not.toContain("data-zoom-enabled");
    expect(markup).not.toContain("touch-action:none");
    expect(markup).not.toContain("translate3d");
    expect(markup).not.toContain("will-change:transform");
  });

  it("opts into local pinch handling only while enabled", () => {
    const markup = renderZoom(true);

    expect(markup).toContain('data-pinch-zoom-allowed=""');
    expect(markup).toContain('data-zoom-enabled=""');
    expect(markup).toContain("touch-action:none");
    expect(markup).toContain("translate3d");
  });
});
