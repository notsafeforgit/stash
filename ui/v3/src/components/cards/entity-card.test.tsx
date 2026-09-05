import type { AnchorHTMLAttributes } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { IntlProvider } from "react-intl";
import { describe, expect, it, vi } from "vitest";

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  Link: ({
    to,
    children,
    viewTransition: _,
    ...props
  }: AnchorHTMLAttributes<HTMLAnchorElement> & {
    to: string;
    viewTransition?: boolean;
  }) => (
    <a href={to} {...props}>
      {children}
    </a>
  ),
}));
vi.mock("src/utils/screen", () => ({ useIsTouch: () => false }));
vi.mock("src/hooks/config", () => ({
  useConfigurationContextOptional: () => undefined,
}));

import { EntityCard } from "./entity-card";
import { CardLayoutContext } from "../list/card-layout-context";

describe("entity card actions", () => {
  it.each([
    "grid",
    "wall",
  ] as const)("names navigation, preview, and selection in %s layout", (layout) => {
    const markup = renderToStaticMarkup(
      <IntlProvider locale="en" messages={{ "actions.preview": "Preview" }}>
        <CardLayoutContext.Provider value={layout}>
          <EntityCard
            id="17"
            href="/scenes/17"
            label="A named scene"
            selected
            onSelectedChanged={() => {}}
            onPreviewClick={() => {}}
          >
            <EntityCard.SelectCheckbox />
            <EntityCard.Preview image="https://stash.test/thumbnail.jpg" />
          </EntityCard>
        </CardLayoutContext.Provider>
      </IntlProvider>,
    );

    expect(markup).toMatch(/<a\b[^>]*aria-label="A named scene"/);
    expect(markup).toMatch(/<button\b[^>]*aria-label="Preview A named scene"/);
    expect(markup).toMatch(
      /<button\b[^>]*aria-pressed="true"[^>]*aria-label="Select A named scene"/,
    );
    expect(markup).not.toContain('aria-label="/scenes/17"');
  });
});
