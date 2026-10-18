// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { expect, it, vi } from "vitest";
import { FolderCriterion } from "@/models/list-filter/criteria/folder";
import messages from "@/locales/en-GB.json";
import flattenMessages from "@/utils/flatten-messages";
const result = { data: { findFolders: { folders: [] } } };
vi.mock("@apollo/client/react", () => ({ useQuery: () => result }));
import { FolderFilter } from "./folder-filter";

it("names selected-folder removal and removes only the chosen selection", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  const criterion = new FolderCriterion();
  criterion.value.items = [{ id: "selected", label: "Library" }];
  criterion.value.excluded = [{ id: "excluded", label: "Private" }];
  const setCriterion = vi.fn();
  try {
    await act(async () =>
      root.render(
        <IntlProvider locale="en-GB" messages={flattenMessages(messages)}>
          <FolderFilter criterion={criterion} setCriterion={setCriterion} />
        </IntlProvider>,
      ),
    );
    const remove = container.querySelector(
      '[aria-label="Remove Library from selection"]',
    );
    expect(remove).toBeInstanceOf(HTMLButtonElement);
    if (!(remove instanceof HTMLButtonElement))
      throw new Error("Missing removal control");
    await act(async () => remove.click());
    expect(setCriterion).toHaveBeenCalledWith(
      expect.objectContaining({
        value: expect.objectContaining({
          items: [],
          excluded: [{ id: "excluded", label: "Private" }],
        }),
      }),
    );
  } finally {
    await act(async () => root.unmount());
    container.remove();
    vi.unstubAllGlobals();
  }
});
