// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { expect, it, vi } from "vitest";
import { PlayerNoFileOverlay } from "./player-overlays";

it("explains a scene without a file in place of playback", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  try {
    await act(async () => {
      root.render(
        <IntlProvider locale="en-GB" messages={{}}>
          <PlayerNoFileOverlay />
        </IntlProvider>,
      );
    });
    const status = container.querySelector('[role="status"]');
    expect(status?.textContent).toContain("No file");
    expect(status?.textContent).toContain("This scene has no file to play.");
  } finally {
    await act(async () => root.unmount());
    container.remove();
    vi.unstubAllGlobals();
  }
});
