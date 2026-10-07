import { IDBFactory, IDBKeyRange, IDBObjectStore } from "fake-indexeddb";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { reviewKeys, withReviewRecord } from "./review-storage";

beforeEach(() => {
  vi.stubGlobal("indexedDB", new IDBFactory());
  vi.stubGlobal("IDBKeyRange", IDBKeyRange);
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

async function save(database: string, keys: string[]) {
  await withReviewRecord(database, "", "readwrite", (_, store) => {
    for (const key of keys)
      store.add({ state: "pending", body: "retained original request" }, key);
  });
}

it("pages only keys and retains the original requests without loading their bodies", async () => {
  await save("one", ["c", "a", "b"]);
  const get = vi.spyOn(IDBObjectStore.prototype, "get");
  const getAll = vi.spyOn(IDBObjectStore.prototype, "getAll");
  const cursor = vi.spyOn(IDBObjectStore.prototype, "openCursor");
  expect(await reviewKeys("one", "", 2)).toEqual({
    keys: ["a", "b"],
    next: "b",
  });
  expect(await reviewKeys("one", "b", 2)).toEqual({
    keys: ["c"],
    next: null,
  });
  expect(get).not.toHaveBeenCalled();
  expect(getAll).not.toHaveBeenCalled();
  expect(cursor).not.toHaveBeenCalled();
  expect(
    await withReviewRecord("one", "a", "readonly", (value) => value),
  ).toEqual({ state: "pending", body: "retained original request" });
});

it("does not invent another page for an exact full page or mix deployment stores", async () => {
  await save("deployment-one", ["a", "b"]);
  await save("deployment-two", ["other"]);
  expect(await reviewKeys("deployment-one", "", 2)).toEqual({
    keys: ["a", "b"],
    next: null,
  });
  expect(await reviewKeys("deployment-two")).toEqual({
    keys: ["other"],
    next: null,
  });
  expect(await reviewKeys("unused")).toEqual({ keys: [], next: null });
});

it("keeps paging after the cursor's request was completed in another tab", async () => {
  await save("one", ["a", "b", "c"]);
  const first = await reviewKeys("one", "", 1);
  await withReviewRecord("one", "a", "readwrite", (_, store) =>
    store.delete("a"),
  );
  expect(await reviewKeys("one", first.next ?? "", 1)).toEqual({
    keys: ["b"],
    next: "b",
  });
});

it("reports malformed keys without deleting or interpreting their saved values", async () => {
  await withReviewRecord("damaged", "", "readwrite", (_, store) => {
    store.add({ untouched: true }, 1);
    store.add({ untouched: true }, "valid");
  });
  await expect(reviewKeys("damaged")).rejects.toMatchObject({
    code: "invalid_saved_request_key",
  });
  expect(
    await withReviewRecord("damaged", "valid", "readonly", (value) => value),
  ).toEqual({ untouched: true });
  const count = await new Promise<number>((resolve, reject) => {
    const open = indexedDB.open("damaged", 1);
    open.onsuccess = () => {
      const tx = open.result.transaction("requests", "readonly");
      const count = tx.objectStore("requests").count();
      tx.oncomplete = () => {
        open.result.close();
        resolve(count.result);
      };
      tx.onerror = () => reject(tx.error);
    };
  });
  expect(count).toBe(2);
});

it("bounds every page before opening storage", async () => {
  const open = vi.spyOn(indexedDB, "open");
  for (const limit of [0, -1, 101, 1.5, Number.NaN])
    await expect(reviewKeys("one", "", limit)).rejects.toMatchObject({
      code: "invalid_review_page",
    });
  expect(open).not.toHaveBeenCalled();
});
