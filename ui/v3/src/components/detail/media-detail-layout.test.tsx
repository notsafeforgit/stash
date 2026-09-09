// @vitest-environment jsdom
import { act, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";
import { IntlProvider } from "react-intl";
import { describe, expect, it, vi } from "vitest";
import { MediaDetailLayout } from "./media-detail-layout";
import { Button } from "@/components/ui/button";

const viewport = vi.hoisted(() => ({ mobile: false }));
vi.mock("@/utils/screen", () => ({ useMediaQuery: () => viewport.mobile }));

describe("MediaDetailLayout focus viewer", () => {
  it("promotes the existing primary content and isolates background controls", () => {
    const markup = renderToStaticMarkup(
      <IntlProvider locale="en">
        <MediaDetailLayout
          primaryContent={<video muted data-testid="same-player" />}
          tabs={[{ id: "details", label: "Details", content: <p>Info</p> }]}
          mobilePageScroll
          primaryFocusMode
          onClosePrimaryFocus={() => {}}
        />
      </IntlProvider>,
    );

    expect(markup).toContain('data-testid="same-player"');
    expect(markup).toContain('role="dialog"');
    expect(markup).toContain('aria-modal="true"');
    expect(markup).toContain("fixed inset-0 z-[9999]");
    expect(markup).toContain("Close scene viewer");
    expect(markup).toContain("inert");
  });

  it("retains the player and restores focus when the mobile viewer closes", async () => {
    viewport.mobile = true;
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    const container = document.createElement("div");
    document.body.append(container);
    const root = createRoot(container);
    function Page() {
      const [focused, setFocused] = useState(false);
      const triggerRef = useRef<HTMLButtonElement>(null);
      return (
        <IntlProvider locale="en">
          <MediaDetailLayout
            primaryContent={
              <>
                <video muted />
                <Button ref={triggerRef} onClick={() => setFocused(true)}>
                  Open viewer
                </Button>
              </>
            }
            tabs={[{ id: "details", label: "Details", content: <p>Info</p> }]}
            mobilePageScroll
            primaryFocusMode={focused}
            onClosePrimaryFocus={() => setFocused(false)}
            primaryFocusReturnRef={triggerRef}
          />
        </IntlProvider>
      );
    }
    try {
      await act(async () => root.render(<Page />));
      const player = container.querySelector("video");
      const trigger = Array.from(container.querySelectorAll("button")).find(
        (button) => button.textContent === "Open viewer",
      );
      if (!player || !trigger) throw new Error("Viewer is missing");
      trigger.focus();
      await act(async () => trigger.click());
      const close = container.querySelector<HTMLButtonElement>(
        '[aria-label="Close scene viewer"]',
      );
      if (!close) throw new Error("Viewer Close is missing");
      expect(document.activeElement).toBe(close);
      expect(player.closest('[role="dialog"]')).not.toBeNull();
      expect(container.querySelector("video")).toBe(player);
      expect(container.querySelector("[data-mobile-detail-footer]")).toBeNull();
      await act(async () => close.click());
      expect(container.querySelector("video")).toBe(player);
      expect(document.activeElement).toBe(trigger);
      expect(
        container.querySelector("[data-mobile-detail-footer]"),
      ).not.toBeNull();
    } finally {
      await act(async () => root.unmount());
      container.remove();
      viewport.mobile = false;
      vi.unstubAllGlobals();
    }
  });
});
