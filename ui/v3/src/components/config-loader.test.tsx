import type { PropsWithChildren } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { IntlProvider } from "react-intl";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { useQueryMock } = vi.hoisted(() => ({
  useQueryMock: vi.fn(),
}));

vi.mock("@apollo/client/react", () => ({
  useQuery: useQueryMock,
}));

vi.mock("./locale-provider", () => ({
  DEFAULT_LOCALE: "en-GB",
  LocaleProvider: ({ children }: PropsWithChildren) => (
    <IntlProvider locale="en-GB">{children}</IntlProvider>
  ),
}));

vi.mock("src/hooks/config", () => ({
  ConfigurationProvider: ({ children }: PropsWithChildren) => children,
}));

import { ConfigLoader } from "./config-loader";

describe("ConfigLoader", () => {
  beforeEach(() => {
    useQueryMock.mockReset();
  });

  it("keeps the app mounted while cached configuration refetches", () => {
    useQueryMock.mockReturnValue({
      loading: true,
      data: {
        configuration: {
          interface: { language: "en-GB" },
        },
      },
    });

    const markup = renderToStaticMarkup(
      <ConfigLoader>
        <div data-testid="app-content" />
      </ConfigLoader>,
    );

    expect(markup).toContain('data-testid="app-content"');
  });

  it("shows the startup loader when configuration has not loaded", () => {
    useQueryMock.mockReturnValue({
      loading: true,
      data: undefined,
    });

    const markup = renderToStaticMarkup(
      <ConfigLoader>
        <div data-testid="app-content" />
      </ConfigLoader>,
    );

    expect(markup).not.toContain('data-testid="app-content"');
  });

  it("offers retry when initial configuration fails", () => {
    useQueryMock.mockReturnValue({
      loading: false,
      error: new Error("Server unavailable"),
      refetch: vi.fn(),
    });
    const markup = renderToStaticMarkup(
      <ConfigLoader>
        <div data-testid="app-content" />
      </ConfigLoader>,
    );
    expect(markup).toContain("Server unavailable");
    expect(markup).toContain("Retry");
    expect(markup).not.toContain('data-testid="app-content"');
  });

  it("preserves the mounted app and explains a failed refresh", () => {
    useQueryMock.mockReturnValue({
      loading: false,
      error: new Error("Offline"),
      refetch: vi.fn(),
      data: { configuration: { interface: {} } },
    });
    const markup = renderToStaticMarkup(
      <ConfigLoader>
        <div data-testid="app-content" />
      </ConfigLoader>,
    );
    expect(markup).toContain('data-testid="app-content"');
    expect(markup).toContain("Showing previously loaded data");
    expect(markup).toContain("Retry");
  });
});
