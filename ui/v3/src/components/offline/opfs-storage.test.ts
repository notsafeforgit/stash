// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import {
  canRequestPersistence,
  canWriteOfflineFiles,
  isPersisted,
  requestDownloadPersistence,
  requestPersistent,
  storageEstimate,
} from "./opfs-storage";

afterEach(() => vi.unstubAllGlobals());

it("treats unavailable or rejected advisory APIs as unknown, not a storage error", async () => {
  vi.stubGlobal("navigator", {});
  expect(canRequestPersistence()).toBe(false);
  await expect(storageEstimate()).resolves.toEqual({
    usage: undefined,
    quota: undefined,
  });
  await expect(requestDownloadPersistence()).resolves.toBe(false);
  const refused = () =>
    Promise.reject(new DOMException("Denied", "SecurityError"));
  vi.stubGlobal("navigator", {
    storage: { estimate: refused, persist: refused, persisted: refused },
  });
  await expect(storageEstimate()).resolves.toEqual({
    usage: undefined,
    quota: undefined,
  });
  await expect(requestPersistent()).resolves.toBe(false);
  await expect(isPersisted()).resolves.toBe(false);
});

it("requests retention immediately once for downloads, while allowing a settings retry", async () => {
  const persist = vi.fn().mockResolvedValue(false);
  vi.stubGlobal("navigator", { storage: { persist } });
  const first = requestDownloadPersistence();
  expect(persist).toHaveBeenCalledOnce();
  const second = requestDownloadPersistence();
  expect(second).toBe(first);
  await expect(first).resolves.toBe(false);
  await requestDownloadPersistence();
  expect(persist).toHaveBeenCalledOnce();
  persist.mockResolvedValue(true);
  await expect(requestPersistent()).resolves.toBe(true);
  expect(persist).toHaveBeenCalledTimes(2);
});

it("requires streaming OPFS writes, not just the older read API", () => {
  vi.stubGlobal("isSecureContext", true);
  vi.stubGlobal("navigator", { storage: { getDirectory: vi.fn() } });
  vi.stubGlobal("FileSystemFileHandle", class {});
  expect(canWriteOfflineFiles()).toBe(false);
  vi.stubGlobal(
    "FileSystemFileHandle",
    class {
      createWritable() {}
    },
  );
  expect(canWriteOfflineFiles()).toBe(true);
  vi.stubGlobal("isSecureContext", false);
  expect(canWriteOfflineFiles()).toBe(false);
});
