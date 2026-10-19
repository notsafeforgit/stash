// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import { Input } from "@/components/ui/input";
import { sessionFetch } from "@/core/session-recovery";
import { getPlatformURL } from "@/core/platform-url";
import { SessionRecoveryDialog } from "./session-recovery-dialog";

let cleanup = async () => {};
afterEach(async () => {
  await cleanup();
  vi.unstubAllGlobals();
});

it("keeps the mounted editor and its draft while a rejected request resumes after login", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  let authenticated = false;
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async (input) => {
      const path = new URL(String(input)).pathname;
      if (path.endsWith("login/locale"))
        return Response.json({ locale: "en-GB", messages: {} });
      if (path.endsWith("login")) {
        authenticated = true;
        return new Response(null, { status: 200 });
      }
      return new Response(null, { status: authenticated ? 204 : 401 });
    }),
  );
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  cleanup = async () => {
    await act(async () => root.unmount());
    container.remove();
  };
  await act(async () =>
    root.render(
      <>
        <SessionRecoveryDialog />
        <Input id="draft" defaultValue="Unsubmitted title" />
      </>,
    ),
  );
  const editor = document.getElementById("draft") as HTMLInputElement;
  let pending!: Promise<Response>;
  await act(async () => {
    pending = sessionFetch(getPlatformURL("graphql"), {
      method: "POST",
      body: '{"operation":"save"}',
    });
  });
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
    "Your edits are kept",
  );
  for (const id of ["login-username", "login-password"]) {
    const input = document.getElementById(id) as HTMLInputElement;
    await act(async () => {
      Object.getOwnPropertyDescriptor(
        HTMLInputElement.prototype,
        "value",
      )?.set?.call(input, "owner");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
  }
  const form = document.querySelector("form");
  if (!form) throw new Error("Missing session recovery form");
  await act(async () => {
    form.dispatchEvent(
      new Event("submit", { bubbles: true, cancelable: true }),
    );
  });
  expect((await pending).status).toBe(204);
  expect(document.getElementById("draft")).toBe(editor);
  expect(editor.value).toBe("Unsubmitted title");
});
