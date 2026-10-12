// @vitest-environment jsdom
import { act, StrictMode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, assert, beforeEach, expect, it, vi } from "vitest";
import type { OfflineEntry } from "./offline-db";
import type { SlimSceneDataFragment } from "@/core/generated-graphql";
import { offlineEntryToSceneData } from "./offline-scene-adapter";
import { snapshotFileMetadata } from "./offline-file-metadata";
const mocks = vi.hoisted(() => ({
  query: vi.fn(),
  patch: vi.fn(),
  identity: vi.fn(),
}));
vi.mock("@apollo/client/react", () => ({ useApolloClient: () => mocks }));
vi.mock("./offline-db", () => ({ patchEntry: mocks.patch }));
vi.mock("@/core/native-archive/metadata-review-api", () => ({
  createMetadataReviewAPI: () => ({ identity: mocks.identity }),
}));
import { NativeArchiveError } from "@/core/native-archive/client";
import { useOfflineMetadataRefresh } from "./offline-metadata-refresh";

const entry: OfflineEntry = {
  scene_id: "e2bc54aa-c990-484e-84bd-9f3b9f45eb41",
  title: "old",
  studio_name: null,
  studio_id: null,
  performers: [],
  tags: [],
  duration: 1,
  width: 1,
  height: 1,
  date: null,
  paths: { screenshot: null, preview: null, sprite: null, vtt: null },
  format: "copy",
  source_video_codec: "h264",
  source_audio_codec: "aac",
  resolution: "STANDARD",
  width_actual: 1,
  height_actual: 1,
  bytes: 0,
  downloaded_at: 0,
  status: "downloading",
  opfs_path: "scenes/1.mp4",
  server_status: "unknown",
};
let root: Root;
let resolve: (value: {
  data: { findScenes: { scenes: SlimSceneDataFragment[] } };
}) => void;
function Probe({ entries }: { entries: OfflineEntry[] }) {
  useOfflineMetadataRefresh({ entries });
  return null;
}
beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.clearAllMocks();
  mocks.query.mockImplementation(
    () =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  root = createRoot(document.createElement("div"));
});
afterEach(async () => {
  await act(async () => root.unmount());
  vi.unstubAllGlobals();
});

it("finishes metadata refresh across Strict Mode and progress updates", async () => {
  await act(async () =>
    root.render(
      <StrictMode>
        <Probe entries={[entry]} />
      </StrictMode>,
    ),
  );
  const requests = mocks.query.mock.calls.length;
  expect(mocks.query).toHaveBeenCalledWith(
    expect.objectContaining({
      variables: { ids: ["e2bc54aa-c990-484e-84bd-9f3b9f45eb41"] },
    }),
  );
  await act(async () =>
    root.render(
      <StrictMode>
        <Probe entries={[{ ...entry, bytes_downloaded: 10 }]} />
      </StrictMode>,
    ),
  );
  expect(mocks.query).toHaveBeenCalledTimes(requests);
  await act(async () => resolve({ data: { findScenes: { scenes: [] } } }));
  expect(mocks.patch).toHaveBeenCalledWith(
    "e2bc54aa-c990-484e-84bd-9f3b9f45eb41",
    { server_status: "missing" },
  );
});

it("retries when connectivity returns and ignores a superseded request", async () => {
  await act(async () => root.render(<Probe entries={[entry]} />));
  const stale = resolve;
  await act(async () => window.dispatchEvent(new Event("online")));
  await act(async () => stale({ data: { findScenes: { scenes: [] } } }));
  expect(mocks.patch).not.toHaveBeenCalled();
  await act(async () => resolve({ data: { findScenes: { scenes: [] } } }));
  expect(mocks.patch).toHaveBeenCalledOnce();
});

it("never sends an empty ID list as an unfiltered query", async () => {
  await act(async () =>
    root.render(<Probe entries={[{ ...entry, scene_id: "invalid" }]} />),
  );
  expect(mocks.query).not.toHaveBeenCalled();
});

function sourceScene(saved: OfflineEntry): SlimSceneDataFragment {
  const scene = offlineEntryToSceneData(saved, "blob:offline");
  const source = scene.files[0];
  assert(source);
  scene.files[0] = {
    ...source,
    frame_rate: 59.94,
    bit_rate: 8_000_000,
    bit_depth: 10,
    color_transfer: "arib-std-b67",
    color_primaries: "bt2020",
  };
  return scene;
}

it("resolves an old offline key without renaming its downloaded file", async () => {
  const saved = {
    ...entry,
    scene_id: "1",
    source_file_path: "/library/original.mp4",
  };
  mocks.identity.mockResolvedValue({ uuid: entry.scene_id });
  await act(async () => root.render(<Probe entries={[saved]} />));
  expect(mocks.identity).toHaveBeenCalledWith("scene", "1");
  expect(mocks.query).toHaveBeenCalledWith(
    expect.objectContaining({ variables: { ids: [entry.scene_id] } }),
  );
  await act(async () =>
    resolve({
      data: {
        findScenes: {
          scenes: [sourceScene({ ...saved, scene_id: entry.scene_id })],
        },
      },
    }),
  );
  expect(mocks.patch).toHaveBeenCalledWith(
    "1",
    expect.objectContaining({ server_status: "present" }),
  );
  expect(mocks.patch.mock.calls[0]?.[1]).not.toHaveProperty("opfs_path");
});

it("does not attach an old download to a reused numeric key", async () => {
  const saved = {
    ...entry,
    scene_id: "1",
    source_file_path: "/library/original.mp4",
  };
  mocks.identity.mockResolvedValue({ uuid: entry.scene_id });
  await act(async () => root.render(<Probe entries={[saved]} />));
  const replacement = sourceScene({
    ...saved,
    scene_id: entry.scene_id,
    source_file_path: "/library/replacement.mp4",
  });
  await act(async () =>
    resolve({ data: { findScenes: { scenes: [replacement] } } }),
  );
  expect(mocks.patch).toHaveBeenCalledWith("1", { server_status: "missing" });
});

it("does not send an unfiltered query when every old download was deleted", async () => {
  mocks.identity.mockRejectedValue(new NativeArchiveError(404, "not_found"));
  await act(async () =>
    root.render(<Probe entries={[{ ...entry, scene_id: "1" }]} />),
  );
  expect(mocks.query).not.toHaveBeenCalled();
  expect(mocks.patch).toHaveBeenCalledWith("1", { server_status: "missing" });
});

it("backfills an old download from its original file even after the primary changes", async () => {
  const saved = { ...entry, source_file_path: "/library/original.mp4" };
  const scene = sourceScene(saved);
  const source = scene.files[0];
  assert(source);
  scene.files.unshift({
    ...source,
    id: "new",
    path: "/library/new.mp4",
  });
  await act(async () => root.render(<Probe entries={[saved]} />));
  await act(async () => resolve({ data: { findScenes: { scenes: [scene] } } }));
  expect(mocks.patch).toHaveBeenCalledWith(
    "e2bc54aa-c990-484e-84bd-9f3b9f45eb41",
    expect.objectContaining({
      source_file_metadata: expect.objectContaining({
        frame_rate: 59.94,
        bit_depth: 10,
        color_transfer: "arib-std-b67",
      }),
      server_status: "present",
    }),
  );
});

it("does not replace an existing technical snapshot with current server metadata", async () => {
  const saved = {
    ...entry,
    source_file_path: "/library/original.mp4",
    source_file_metadata: snapshotFileMetadata({
      frame_rate: 24,
      color_transfer: "bt709",
    }),
  };
  await act(async () => root.render(<Probe entries={[saved]} />));
  await act(async () =>
    resolve({ data: { findScenes: { scenes: [sourceScene(saved)] } } }),
  );
  expect(mocks.patch).toHaveBeenCalledWith(
    "e2bc54aa-c990-484e-84bd-9f3b9f45eb41",
    { server_status: "present" },
  );
});

it.each([
  "path",
  "duration",
  "width",
  "height",
  "video_codec",
  "audio_codec",
] as const)(
  "leaves old metadata unknown when the source %s no longer matches",
  async (property) => {
    const saved = { ...entry, source_file_path: "/library/original.mp4" };
    const scene = sourceScene(saved);
    const source = scene.files[0];
    assert(source);
    scene.files[0] = {
      ...source,
      [property]: typeof source[property] === "number" ? 999 : "changed",
    };
    await act(async () => root.render(<Probe entries={[saved]} />));
    await act(async () =>
      resolve({ data: { findScenes: { scenes: [scene] } } }),
    );
    expect(mocks.patch).toHaveBeenCalledWith(
      "e2bc54aa-c990-484e-84bd-9f3b9f45eb41",
      { server_status: "present" },
    );
  },
);
