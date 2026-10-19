// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, expect, it, vi } from "vitest";
import { LoginForm } from "./login-form";

let cleanup = async () => {};
afterEach(async () => {
  await cleanup();
  vi.unstubAllGlobals();
});

it("retains credentials after rejection and submits a same-origin form on retry", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const transport = vi
    .fn<typeof fetch>()
    .mockResolvedValueOnce(new Response("invalid", { status: 401 }))
    .mockResolvedValueOnce(new Response(null, { status: 200 }));
  vi.stubGlobal("fetch", transport);
  const success = vi.fn();
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
        <LoginForm onSuccess={success} />
      </IntlProvider>,
    ),
  );
  for (const [id, value] of [
    ["login-username", "owner"],
    ["login-password", "a&b+c"],
  ]) {
    const input = document.getElementById(id!) as HTMLInputElement;
    await act(async () => {
      Object.getOwnPropertyDescriptor(
        HTMLInputElement.prototype,
        "value",
      )?.set?.call(input, value);
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
  }
  const form = container.querySelector("form");
  if (!form) throw new Error("Missing login form");
  await act(async () => {
    form.dispatchEvent(
      new Event("submit", { bubbles: true, cancelable: true }),
    );
  });
  expect(container.querySelector('[role="alert"]')?.textContent).toContain(
    "Invalid username or password",
  );
  expect(success).not.toHaveBeenCalled();
  expect(
    (document.getElementById("login-password") as HTMLInputElement).value,
  ).toBe("a&b+c");
  expect(
    container.querySelector('[name="password"]')?.getAttribute("autocomplete"),
  ).toBe("current-password");
  await act(async () => {
    form.dispatchEvent(
      new Event("submit", { bubbles: true, cancelable: true }),
    );
  });
  expect(success).toHaveBeenCalledOnce();
  for (const [, init] of transport.mock.calls) {
    expect(init).toMatchObject({
      credentials: "same-origin",
      method: "POST",
      redirect: "error",
    });
    expect(init?.body?.toString()).toBe("username=owner&password=a%26b%2Bc");
  }
});
