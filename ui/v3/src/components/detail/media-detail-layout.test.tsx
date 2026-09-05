import { renderToStaticMarkup } from "react-dom/server";
import { IntlProvider } from "react-intl";
import { describe, expect, it } from "vitest";
import { MediaDetailLayout } from "./media-detail-layout";

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
});
