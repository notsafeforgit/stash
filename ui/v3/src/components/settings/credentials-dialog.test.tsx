// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, expect, it, vi } from "vitest";
import { CredentialsDialog } from "./credentials-dialog";

let cleanup = async () => {};
afterEach(async () => {
  await cleanup();
  vi.unstubAllGlobals();
});

it("keeps a failed draft open and omits blank passwords on retry", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const save = vi
    .fn<(input: unknown) => Promise<void>>()
    .mockResolvedValue(undefined)
    .mockRejectedValueOnce(new Error("Write failed"))
    .mockResolvedValueOnce();
  const close = vi.fn();
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  cleanup = async () => {
    await act(async () => root.unmount());
    container.remove();
  };
  await act(async () =>
    root.render(
      <IntlProvider locale="en">
        <CredentialsDialog
          onOpenChange={close}
          initialUsername="owner"
          passwordConfigured
          onSave={save}
        />
      </IntlProvider>,
    ),
  );
  const form = document.querySelector("form");
  if (!form) throw new Error("Missing credentials form");
  await act(async () => {
    form.dispatchEvent(
      new Event("submit", { bubbles: true, cancelable: true }),
    );
  });
  expect(close).not.toHaveBeenCalled();
  expect(document.querySelector('[role="alert"]')?.textContent).toContain(
    "Credentials were not saved",
  );
  await act(async () => {
    form.dispatchEvent(
      new Event("submit", { bubbles: true, cancelable: true }),
    );
  });
  expect(save).toHaveBeenNthCalledWith(1, { username: "owner" });
  expect(save).toHaveBeenNthCalledWith(2, { username: "owner" });
  expect(close).toHaveBeenCalledWith(false);
  const clear = document.getElementById("clear-password");
  if (!clear) throw new Error("Missing explicit password removal control");
  await act(async () => clear.click());
  await act(async () => {
    form.dispatchEvent(
      new Event("submit", { bubbles: true, cancelable: true }),
    );
  });
  expect(save).toHaveBeenLastCalledWith({
    username: "owner",
    clearPassword: true,
  });
});
