// @vitest-environment jsdom
import { act, type AnchorHTMLAttributes } from "react";
import { createRoot } from "react-dom/client";
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
import { CardMediaContext } from "./card-media-context";

describe("entity card actions", () => {
  it("stops a distant video and resumes it on return without remounting controls", async () => {
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    const play = vi
      .spyOn(HTMLMediaElement.prototype, "play")
      .mockResolvedValue();
    const pause = vi
      .spyOn(HTMLMediaElement.prototype, "pause")
      .mockImplementation(() => {});
    const load = vi
      .spyOn(HTMLMediaElement.prototype, "load")
      .mockImplementation(() => {});
    const container = document.createElement("div");
    document.body.append(container);
    const root = createRoot(container);
    const render = (active: boolean) =>
      root.render(
        <IntlProvider locale="en" messages={{ "actions.preview": "Preview" }}>
          <CardMediaContext value={active}>
            <EntityCard
              id="17"
              label="Scene"
              onPreviewClick={() => {}}
              destination={{
                to: "/scenes/$sceneId",
                params: { sceneId: "17" },
                search: undefined,
              }}
            >
              <EntityCard.Preview image="/cover.jpg" video="/preview.mp4" />
            </EntityCard>
          </CardMediaContext>
        </IntlProvider>,
      );
    try {
      await act(async () => render(true));
      const video = container.querySelector("video");
      const button = container.querySelector(
        "button[data-card-preview-button]",
      );
      expect(video).not.toBeNull();
      expect(play).toHaveBeenCalledOnce();
      await act(async () => render(false));
      expect(video?.isConnected).toBe(false);
      expect(video?.getAttribute("src")).toBeNull();
      expect(pause).toHaveBeenCalled();
      expect(load).toHaveBeenCalledOnce();
      expect(container.querySelector("button[data-card-preview-button]")).toBe(
        button,
      );
      await act(async () => render(true));
      expect(container.querySelector("video")).not.toBe(video);
      expect(play).toHaveBeenCalledTimes(2);
    } finally {
      await act(async () => root.unmount());
      container.remove();
      vi.restoreAllMocks();
      vi.unstubAllGlobals();
    }
  });

  it.each(["grid", "wall"] as const)(
    "releases media without removing preview controls in %s layout",
    (layout) => {
      const render = (active: boolean) =>
        renderToStaticMarkup(
          <IntlProvider locale="en" messages={{ "actions.preview": "Preview" }}>
            <CardLayoutContext value={layout}>
              <CardMediaContext value={active}>
                <EntityCard
                  id="17"
                  label="A named scene"
                  onPreviewClick={() => {}}
                  destination={{
                    to: "/scenes/$sceneId",
                    params: { sceneId: "17" },
                    search: undefined,
                  }}
                >
                  <EntityCard.Preview
                    image="/cover.jpg"
                    video="/preview.mp4"
                    studioImagePath="/studio.png"
                  />
                </EntityCard>
              </CardMediaContext>
            </CardLayoutContext>
          </IntlProvider>,
        );
      expect(render(true)).toContain("/preview.mp4");
      const inactive = render(false);
      expect(inactive).not.toMatch(/<(?:img|video|picture)\b/);
      expect(inactive).not.toContain("/cover.jpg");
      expect(inactive).not.toContain("/studio.png");
      expect(inactive).toContain('aria-label="Preview A named scene"');
      expect(inactive).toContain('data-entity-card-preview=""');
    },
  );

  it.each(["grid", "wall"] as const)(
    "names navigation, preview, and selection in %s layout",
    (layout) => {
      const markup = renderToStaticMarkup(
        <IntlProvider locale="en" messages={{ "actions.preview": "Preview" }}>
          <CardLayoutContext.Provider value={layout}>
            <EntityCard
              id="17"
              destination={{
                to: "/scenes/$sceneId",
                params: { sceneId: "17" },
                search: undefined,
              }}
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
      expect(markup).toMatch(
        /<button\b[^>]*aria-label="Preview A named scene"/,
      );
      expect(markup).toMatch(
        /<button\b[^>]*aria-pressed="true"[^>]*aria-label="Select A named scene"/,
      );
      expect(markup).not.toContain('aria-label="/scenes/17"');
    },
  );
});
