import { describe, expect, it } from "vitest";
import { supportsSceneVideoRotation } from "./scene-video-rotation";

describe("supportsSceneVideoRotation", () => {
  it.each([
    "video.mkv",
    "video.mp4",
    "video.m4v",
    "video.mov",
    "VIDEO.MP4",
  ])("supports %s", (path) => {
    expect(supportsSceneVideoRotation(path)).toBe(true);
  });

  it.each([
    "video.avi",
    "video.webm",
    "video.mpeg",
    "video",
  ])("rejects %s", (path) => {
    expect(supportsSceneVideoRotation(path)).toBe(false);
  });

  it("rejects a missing path", () => {
    expect(supportsSceneVideoRotation(null)).toBe(false);
    expect(supportsSceneVideoRotation(undefined)).toBe(false);
  });
});
