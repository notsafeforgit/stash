// @vitest-environment jsdom
import { act, useEffect, useState, type FC } from "react";
import { createRoot, type Root } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { deferred } from "@/test-utils/deferred";
import { deferredDialog } from "./deferred-overlay";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}
let root: Root;
let container: HTMLDivElement;
beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});
afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it("does not load closed dialogs, allows cancellation while loading, and retains the mounted component", async () => {
  const pending = deferred<{ default: FC<Props> }>();
  const load = vi.fn(() => pending.promise);
  const Dialog = deferredDialog(load);
  const mount = vi.fn();
  function Content({ open }: Props) {
    const [draft] = useState("retained draft");
    useEffect(mount, []);
    return <div data-open={open}>{draft}</div>;
  }
  const onOpenChange = vi.fn();
  const render = async (open: boolean) =>
    act(async () =>
      root.render(
        <IntlProvider locale="en">
          <Dialog open={open} onOpenChange={onOpenChange} />
        </IntlProvider>,
      ),
    );
  await render(false);
  expect(load).not.toHaveBeenCalled();
  await render(true);
  expect(load).toHaveBeenCalledTimes(1);
  expect(document.querySelector('[role="dialog"]')).not.toBeNull();
  const close = document.querySelector<HTMLButtonElement>(
    '[data-slot="dialog-close"]',
  );
  expect(close).not.toBeNull();
  await act(async () => close?.click());
  expect(onOpenChange).toHaveBeenCalledWith(false);
  await render(false);
  await act(async () => pending.resolve({ default: Content }));
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(container.querySelector('[data-open="false"]')).not.toBeNull();
  await render(true);
  await render(false);
  await render(true);
  expect(mount).toHaveBeenCalledTimes(1);
  expect(load).toHaveBeenCalledTimes(1);
  expect(container.textContent).toContain("retained draft");
});

it("offers recovery for a failed chunk and keeps it dismissible", async () => {
  vi.spyOn(console, "error").mockImplementation(() => {});
  const Dialog = deferredDialog<Props>(async () => {
    throw new Error("Chunk unavailable");
  });
  const close = vi.fn();
  await act(async () =>
    root.render(
      <IntlProvider locale="en">
        <Dialog open onOpenChange={close} />
      </IntlProvider>,
    ),
  );
  expect(document.body.textContent).toContain("Chunk unavailable");
  expect(document.body.textContent).toContain("Retry");
  await act(async () =>
    document
      .querySelector<HTMLButtonElement>('[data-slot="dialog-close"]')
      ?.click(),
  );
  expect(close).toHaveBeenCalledWith(false);
});
