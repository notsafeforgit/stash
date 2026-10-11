import { renderToStaticMarkup } from "react-dom/server";
import { IntlProvider } from "react-intl";
import { describe, expect, it, vi } from "vitest";
import type { PropsWithChildren } from "react";
import { ConfigurationProvider } from "@/hooks/config";
import { playerConfiguration } from "../../../tests/browser/fixture/player-configuration";
import { SourceTrackingGate } from "./source-tracking-gate";
import { sourceTrackingTabs } from "@/core/source-tracking";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, ...props }: PropsWithChildren<{ to: string }>) => (
    <a href={props.to}>{children}</a>
  ),
}));

describe("source tracking capability", () => {
  it("does not mount source pages until the running server enables them", () => {
    let mounted = 0;
    function SourcePage() {
      mounted++;
      return <p>Source page content</p>;
    }
    function render(pathname: string, saved: boolean, active: boolean) {
      return renderToStaticMarkup(
        <IntlProvider locale="en-GB">
          <ConfigurationProvider
            configuration={{
              ...playerConfiguration,
              general: {
                ...playerConfiguration.general,
                sourceTrackingEnabled: saved,
                sourceTrackingActive: active,
              },
            }}
          >
            <SourceTrackingGate routeId={pathname}>
              <SourcePage />
            </SourceTrackingGate>
          </ConfigurationProvider>
        </IntlProvider>,
      );
    }
    expect(render("/source-posts", false, false)).toContain(
      "Source tracking is off",
    );
    expect(render("/account-review/", true, false)).toContain(
      "Source tracking is off",
    );
    expect(mounted).toBe(0);
    expect(render("/source-posts", false, true)).toContain(
      "Source page content",
    );
    expect(render("/scenes", false, false)).toContain("Source page content");
    expect(mounted).toBe(2);
  });

  it("keeps library and provider tabs available without source tracking", () => {
    const tabs = [
      "details",
      "source-review",
      "source-albums",
      "source-accounts",
      "metadata-review",
      "provider-imports",
      "fileinfo",
    ].map((id) => ({ id }));
    expect(sourceTrackingTabs(tabs, false).map((tab) => tab.id)).toEqual([
      "details",
      "metadata-review",
      "provider-imports",
      "fileinfo",
    ]);
    expect(sourceTrackingTabs(tabs, true)).toEqual(tabs);
  });
});
