// @vitest-environment jsdom
import { act, StrictMode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { OfflineEntry } from "./offline-db";
const mocks = vi.hoisted(() => ({ query: vi.fn(), patch: vi.fn() }));
vi.mock("@apollo/client/react", () => ({ useApolloClient: () => mocks }));
vi.mock("./offline-db", () => ({ patchEntry: mocks.patch }));
import { useOfflineMetadataRefresh } from "./offline-metadata-refresh";

const entry: OfflineEntry = {
  scene_id: "1",
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
let resolve: (value: { data: { findScenes: { scenes: never[] } } }) => void;
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
  await act(async () =>
    root.render(
      <StrictMode>
        <Probe entries={[{ ...entry, bytes_downloaded: 10 }]} />
      </StrictMode>,
    ),
  );
  expect(mocks.query).toHaveBeenCalledTimes(requests);
  await act(async () => resolve({ data: { findScenes: { scenes: [] } } }));
  expect(mocks.patch).toHaveBeenCalledWith("1", { server_status: "missing" });
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
