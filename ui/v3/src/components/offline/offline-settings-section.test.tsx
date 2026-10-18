// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import messages from "@/locales/en-GB.json";
import flattenMessages from "@/utils/flatten-messages";

const mocks = vi.hoisted(() => ({ removeAll: vi.fn() }));
vi.mock("./use-download-queue", () => ({
  useDownloadCommands: () => mocks,
  canDownloadScenes: () => true,
}));
vi.mock("./offline-recovery-control", () => ({
  OfflineRecoveryControl: () => null,
}));
vi.mock("@/pwa/register", () => ({ registerOfflineWorker: async () => null }));
vi.mock("./opfs-storage", () => ({
  canRequestPersistence: () => false,
  isPersisted: async () => false,
  requestPersistent: async () => false,
  storageEstimate: async () => ({ usage: 1024, quota: 2048 }),
}));
import { OfflineSettingsSection } from "./offline-settings-section";

let root: Root;
beforeEach(async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  mocks.removeAll.mockReset();
  document.body.innerHTML = "<div id='test-root'></div>";
  const container = document.getElementById("test-root");
  if (!container) throw new Error("Missing test root");
  root = createRoot(container);
  await act(async () =>
    root.render(
      <IntlProvider locale="en-GB" messages={flattenMessages(messages)}>
        <OfflineSettingsSection />
      </IntlProvider>,
    ),
  );
});
afterEach(async () => {
  await act(async () => root.unmount());
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
});

function button(text: string, scope: ParentNode = document): HTMLButtonElement {
  const element = [...scope.querySelectorAll("button")].find(
    (button) => button.textContent?.trim() === text,
  );
  if (!(element instanceof HTMLButtonElement))
    throw new Error(`Missing button: ${text}`);
  return element;
}

it("confirms deletion in the shared dialog and retains it for failure/retry", async () => {
  await act(async () => button("Clear all").click());
  expect(mocks.removeAll).not.toHaveBeenCalled();
  const initialDialog = document.querySelector('[role="dialog"]');
  expect(initialDialog?.textContent).toContain(
    "Delete all offline downloads from this device?",
  );
  if (!initialDialog) throw new Error("Missing confirmation dialog");
  await act(async () => button("Cancel", initialDialog).click());
  expect(mocks.removeAll).not.toHaveBeenCalled();

  await act(async () => button("Clear all").click());
  const dialog = document.querySelector('[role="dialog"]');
  if (!dialog) throw new Error("Missing confirmation dialog");
  mocks.removeAll.mockRejectedValueOnce(new Error("storage unavailable"));
  await act(async () => button("Clear all", dialog).click());
  expect(mocks.removeAll).toHaveBeenCalledTimes(1);
  expect(document.querySelector('[role="dialog"]')).not.toBeNull();
  mocks.removeAll.mockResolvedValueOnce(undefined);
  await act(async () => button("Clear all", dialog).click());
  expect(mocks.removeAll).toHaveBeenCalledTimes(2);
  expect(document.querySelector('[role="dialog"]')).toBeNull();
});

it("blocks repeated confirmation and dismissal while deletion is pending", async () => {
  let finish = () => {};
  mocks.removeAll.mockImplementation(
    () =>
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
  );
  await act(async () => button("Clear all").click());
  const dialog = document.querySelector('[role="dialog"]');
  if (!dialog) throw new Error("Missing confirmation dialog");
  await act(async () => button("Clear all", dialog).click());
  expect(button("Clear all", dialog).disabled).toBe(true);
  expect(button("Cancel", dialog).disabled).toBe(true);
  await act(async () => button("Clear all", dialog).click());
  expect(mocks.removeAll).toHaveBeenCalledTimes(1);
  await act(async () => finish());
  expect(document.querySelector('[role="dialog"]')).toBeNull();
});
