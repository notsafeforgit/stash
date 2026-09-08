// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createStoredState } from "./stored-state";
import { interfacePreferencesSchema } from "./interface-preferences";

const data = new Map<string, string>();
beforeEach(() => {
  data.clear();
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => {
      data.set(key, value);
    },
  });
});
afterEach(() => vi.unstubAllGlobals());

function makeStore() {
  return createStoredState(
    "interface",
    interfacePreferencesSchema,
    interfacePreferencesSchema.parse({}),
  );
}

it("shares updates between mounted consumers and retains extension fields", async () => {
  data.set("interface", JSON.stringify({ plugin: { enabled: true } }));
  const store = makeStore();
  const container = document.createElement("div");
  const root = createRoot(container);
  let first: ReturnType<typeof store.useStoredState> | undefined;
  let second: ReturnType<typeof store.useStoredState> | undefined;
  function First() {
    first = store.useStoredState();
    return null;
  }
  function Second() {
    second = store.useStoredState();
    return null;
  }
  try {
    await act(async () =>
      root.render(
        <>
          <First />
          <Second />
        </>,
      ),
    );
    await act(async () =>
      first?.[1]((previous) => ({
        ...previous,
        viewConfig: { ...previous.viewConfig, scenes: { showSidebar: true } },
      })),
    );
    await act(async () =>
      second?.[1]((previous) => ({
        ...previous,
        viewConfig: { ...previous.viewConfig, images: { showSidebar: false } },
      })),
    );
    expect(first?.[0]).toEqual(second?.[0]);
    expect(store.getSnapshot()).toMatchObject({
      plugin: { enabled: true },
      viewConfig: {
        scenes: { showSidebar: true },
        images: { showSidebar: false },
      },
    });
    data.set(
      "interface",
      JSON.stringify({ viewConfig: { tags: { showSidebar: true } } }),
    );
    await act(async () =>
      window.dispatchEvent(new StorageEvent("storage", { key: "interface" })),
    );
    expect(second?.[0].viewConfig.tags?.showSidebar).toBe(true);
  } finally {
    await act(async () => root.unmount());
  }
});

it("keeps edits usable when reads work but persistence fails", () => {
  const store = makeStore();
  vi.spyOn(localStorage, "setItem").mockImplementation(() => {
    throw new Error("quota");
  });
  store.set((previous) => ({
    ...previous,
    viewConfig: { scenes: { showSidebar: true } },
  }));
  store.set((previous) => ({
    ...previous,
    viewConfig: { ...previous.viewConfig, images: { showSidebar: true } },
  }));
  expect(Object.keys(store.getSnapshot().viewConfig)).toEqual([
    "scenes",
    "images",
  ]);
});
