import type { PropsWithChildren } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { useQueryMock } = vi.hoisted(() => ({
  useQueryMock: vi.fn(),
}));

vi.mock("@apollo/client/react", () => ({
  useQuery: useQueryMock,
}));

vi.mock("./locale-provider", () => ({
  DEFAULT_LOCALE: "en-GB",
  LocaleProvider: ({ children }: PropsWithChildren) => children,
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
});
