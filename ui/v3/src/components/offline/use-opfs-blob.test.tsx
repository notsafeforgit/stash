// @vitest-environment jsdom
import { act, useLayoutEffect } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { deferred } from "@/test-utils/deferred";
import { useOpfsBlobUrl } from "./use-opfs-blob";
import { readScene } from "./opfs-storage";

vi.mock("./opfs-storage", () => ({ readScene: vi.fn() }));
const committed = vi.fn();
const revoke = vi.fn();
let root: Root;
let container: HTMLDivElement;

function BlobView({ sceneId }: { sceneId: string }) {
  const result = useOpfsBlobUrl(sceneId);
  useLayoutEffect(() => {
    committed(sceneId, result.url);
  }, [sceneId, result.url]);
  return null;
}

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal(
    "URL",
    class extends URL {
      static createObjectURL(blob: Blob) {
        return `blob:${blob instanceof File ? blob.name : "unknown"}`;
      }
      static revokeObjectURL = revoke;
    },
  );
  vi.clearAllMocks();
  container = document.createElement("div");
  root = createRoot(container);
});
afterEach(async () => {
  await act(async () => root.unmount());
  vi.unstubAllGlobals();
});

it("never commits the previous scene's URL under the next scene's identity", async () => {
  vi.mocked(readScene).mockResolvedValueOnce(new File([], "first"));
  const pending = deferred<File | null>();
  vi.mocked(readScene).mockReturnValueOnce(pending.promise);
  await act(async () => root.render(<BlobView sceneId="first" />));
  await act(async () => root.render(<BlobView sceneId="second" />));
  expect(committed).not.toHaveBeenCalledWith("second", "blob:first");
  expect(committed).toHaveBeenCalledWith("second", null);
  expect(revoke).toHaveBeenCalledWith("blob:first");
  await act(async () => pending.resolve(new File([], "second")));
  expect(committed).toHaveBeenLastCalledWith("second", "blob:second");
});

it("ignores a late OPFS read after selecting a different scene", async () => {
  const first = deferred<File | null>();
  vi.mocked(readScene).mockReturnValueOnce(first.promise);
  vi.mocked(readScene).mockResolvedValueOnce(new File([], "second"));
  await act(async () => root.render(<BlobView sceneId="first" />));
  await act(async () => root.render(<BlobView sceneId="second" />));
  await act(async () => first.resolve(new File([], "first")));
  expect(committed).toHaveBeenLastCalledWith("second", "blob:second");
  expect(committed).not.toHaveBeenCalledWith("second", "blob:first");
});
