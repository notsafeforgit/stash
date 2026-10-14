// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { expect, it, vi } from "vitest";
import { GalleryAlternateTitles } from "./alternate-titles";

it("shows alternate titles and expands the remaining titles on demand", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  try {
    await act(async () =>
      root.render(
        <IntlProvider locale="en" messages={{}}>
          <GalleryAlternateTitles
            primaryTitle="Primary caption"
            titles={[
              "Primary caption",
              "First caption",
              "First caption",
              "Second caption",
              " Second caption ",
              "Third caption",
              "Fourth caption",
              "Fourth caption",
              "",
              " ",
            ]}
          />
        </IntlProvider>,
      ),
    );
    expect(container.textContent).toContain("Alternate post titles");
    expect(container.textContent).toContain("First caption");
    expect(container.textContent).not.toContain("Primary caption");
    expect(
      [...container.querySelectorAll("li")].map((li) => li.textContent),
    ).toEqual(["First caption", "Second caption", "Third caption"]);
    expect(container.textContent).not.toContain("Fourth caption");
    const button = container.querySelector("button");
    expect(button?.textContent).toContain("1 more title");
    await act(async () => button?.click());
    expect(container.textContent).toContain("Fourth caption");
    expect(
      [...container.querySelectorAll("li")].map((li) => li.textContent),
    ).toEqual([
      "First caption",
      "Second caption",
      "Third caption",
      "Fourth caption",
    ]);
  } finally {
    await act(async () => root.unmount());
    container.remove();
    vi.unstubAllGlobals();
  }
});

it("hides the alternate title section when only the primary title or blanks remain", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () =>
      root.render(
        <IntlProvider locale="en" messages={{}}>
          <GalleryAlternateTitles
            primaryTitle="Primary caption"
            titles={["Primary caption", " Primary caption ", " "]}
          />
        </IntlProvider>,
      ),
    );
    expect(container.textContent).toBe("");
    expect(container.querySelector("button")).toBeNull();
  } finally {
    await act(async () => root.unmount());
    vi.unstubAllGlobals();
  }
});
