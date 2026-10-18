// Advertise the real endpoint families; browser tests stub only HDR capability
// probes and serve synthetic SDR fragments. This tests selection, not HDR pixels.
export function hdrStreams(sceneId: string) {
  return [
    ["ORIGINAL", ""],
    ["FOUR_K", " 4K (2160p)"],
    ["FULL_HD", " Full HD (1080p)"],
    ["STANDARD_HD", " HD (720p)"],
    ["STANDARD", " Standard (480p)"],
    ["LOW", " Low (240p)"],
  ].flatMap(([resolution, label]) =>
    [true, false].map((hdr) => ({
      __typename: "SceneStreamEndpoint" as const,
      url: new URL(
        `/scene/${sceneId}/stream${hdr ? ".hevc" : ""}.master.m3u8?resolution=${resolution}`,
        location.href,
      ).href,
      mime_type: "application/vnd.apple.mpegurl",
      label: `HLS${hdr ? " (HDR)" : ""}${label}`,
    })),
  );
}
