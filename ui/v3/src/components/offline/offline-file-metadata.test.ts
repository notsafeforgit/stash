import { describe, expect, it } from "vitest";
import { FindScenesMobileDocument } from "@/core/generated-graphql";
import type { OfflineEntry } from "./offline-db";
import { offlineEntrySchema } from "./offline-entry-schema";
import { offlineEntryToSceneData } from "./offline-scene-adapter";
import { offlineEntryToSceneCardScene } from "./offline-scene-card-data";
import { sceneDownloadSnapshot } from "./scene-download-input";

const file = {
  path: "/library/example.mp4",
  duration: 60,
  width: 3840,
  height: 2160,
  video_codec: "hevc",
  audio_codec: "flac",
  frame_rate: 29.97,
  bit_rate: 24_000_000,
  bit_depth: 10,
  color_range: "tv",
  color_space: "bt2020nc",
  color_transfer: "smpte2084",
  color_primaries: "bt2020",
};

function download(format: OfflineEntry["format"] = "copy"): OfflineEntry {
  const snapshot = sceneDownloadSnapshot({ id: "1", paths: {}, files: [file] });
  if (!snapshot) throw new Error("Missing download snapshot");
  return {
    ...snapshot,
    format,
    resolution: "ORIGINAL",
    width_actual: file.width,
    height_actual: file.height,
    bytes: 90_000_000,
    status: "complete",
    downloaded_at: 1,
    opfs_path: "scenes/1.mp4",
    server_status: "present",
  };
}

describe("downloaded file info", () => {
  it("retains frame rate and HDR metadata through persistence and both adapters", () => {
    const saved = offlineEntrySchema.parse(
      JSON.parse(JSON.stringify(download())),
    );
    const detail = offlineEntryToSceneData(saved, "blob:offline").files[0];
    const card = offlineEntryToSceneCardScene(saved).files[0];
    for (const output of [detail, card]) {
      expect(output).toMatchObject({
        video_codec: "hevc",
        audio_codec: "flac",
        frame_rate: 29.97,
        bit_rate: 12_000_000,
        bit_depth: 10,
        color_transfer: "smpte2084",
        color_primaries: "bt2020",
        color_space: "bt2020nc",
        color_range: "tv",
      });
    }
  });

  it("keeps copied video metadata while reporting AAC audio conversion", () => {
    expect(
      offlineEntryToSceneData(download("copy-aac"), "blob:offline").files[0],
    ).toMatchObject({
      video_codec: "hevc",
      audio_codec: "aac",
      color_transfer: "smpte2084",
    });
  });

  it.each(["smpte2084", "arib-std-b67"])(
    "preserves %s in HDR transcodes",
    (transfer) => {
      for (const format of ["hevc", "av1"] as const) {
        const saved = download(format);
        saved.source_video_codec = "vp9";
        saved.source_file_metadata = {
          ...file,
          color_transfer: transfer,
          bit_depth: 12,
        };
        expect(
          offlineEntryToSceneData(saved, "blob:offline").files[0],
        ).toMatchObject({
          video_codec: format,
          audio_codec: "aac",
          bit_rate: 12_000_000,
          bit_depth: 10,
          color_transfer: transfer,
        });
      }
    },
  );

  it("does not mistake source HDR tags or source bitrate for an H.264 download", () => {
    const saved = download("h264");
    expect(
      offlineEntryToSceneData(saved, "blob:offline").files[0],
    ).toMatchObject({
      video_codec: "h264",
      audio_codec: "aac",
      frame_rate: 29.97,
      bit_rate: 12_000_000,
      color_transfer: null,
      bit_depth: null,
    });
    saved.status = "downloading";
    expect(
      offlineEntryToSceneData(saved, "blob:offline").files[0]?.bit_rate,
    ).toBe(0);
  });

  it("reads old entries and handles silent sources and unavailable metadata", () => {
    const saved = download("hevc");
    delete saved.source_file_metadata;
    saved.source_audio_codec = "";
    saved.duration = 0;
    const parsed = offlineEntrySchema.parse(saved);
    expect(
      offlineEntryToSceneData(parsed, "blob:offline").files[0],
    ).toMatchObject({
      audio_codec: "",
      frame_rate: 0,
      bit_rate: 0,
      bit_depth: null,
      color_transfer: null,
    });
  });

  it("requests download metadata in the mobile list query", () => {
    const fragment = FindScenesMobileDocument.definitions.find(
      (node) =>
        node.kind === "FragmentDefinition" &&
        node.name.value === "MobileSceneData",
    );
    if (fragment?.kind !== "FragmentDefinition")
      throw new Error("Missing mobile scene fragment");
    const files = fragment.selectionSet.selections.find(
      (node) => node.kind === "Field" && node.name.value === "files",
    );
    if (files?.kind !== "Field") throw new Error("Missing mobile files");
    const fields = files.selectionSet?.selections.flatMap((node) =>
      node.kind === "Field" ? [node.name.value] : [],
    );
    expect(fields).toEqual(
      expect.arrayContaining([
        "video_codec",
        "audio_codec",
        "frame_rate",
        "bit_rate",
        "bit_depth",
        "color_range",
        "color_space",
        "color_transfer",
        "color_primaries",
      ]),
    );
  });
});
