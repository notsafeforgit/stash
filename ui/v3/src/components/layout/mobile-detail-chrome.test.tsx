// @vitest-environment jsdom
import { act, useState } from "react";
import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { expect, it, vi } from "vitest";
import { DetailTabs } from "@/components/detail/detail-tabs";
import { Button } from "@/components/ui/button";
import messages from "@/locales/en-GB.json";
import flattenMessages from "@/utils/flatten-messages";
import {
  MobileDetailChromeProvider,
  MobileDetailFooter,
} from "./mobile-detail-chrome";

const viewport = vi.hoisted(() => ({ mobile: true }));
vi.mock("@/components/layout/mobile-nav-sheet", () => ({
  MobileNavSheet: () => null,
}));
vi.mock("@/utils/screen", () => ({
  useMediaQuery: () => viewport.mobile,
}));

function StatefulPanel() {
  const [value, setValue] = useState(0);
  return <Button onClick={() => setValue(value + 1)}>Count {value}</Button>;
}

it("preserves tab state between the mobile picker and desktop strip", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const scrollIntoView = vi.fn();
  const originalScroll = HTMLElement.prototype.scrollIntoView;
  HTMLElement.prototype.scrollIntoView = scrollIntoView;
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  const onBack = vi.fn();
  function Page() {
    const [tab, setTab] = useState("scenes");
    return (
      <IntlProvider locale="en-GB" messages={flattenMessages(messages)}>
        <MobileDetailChromeProvider>
          <div data-page-content>
            <DetailTabs
              tabs={[
                { id: "scenes", label: "Scenes", content: <StatefulPanel /> },
                { id: "images", label: "Images", content: <p>Images</p> },
              ]}
              activeTab={tab}
              onTabChange={setTab}
            />
          </div>
          <MobileDetailFooter onBack={onBack} />
        </MobileDetailChromeProvider>
      </IntlProvider>
    );
  }
  function button(label: string) {
    const found = Array.from(document.querySelectorAll("button")).find(
      (element) =>
        element.textContent === label ||
        element.getAttribute("aria-label") === label,
    );
    if (!found) throw new Error(`Missing ${label} control`);
    return found;
  }
  function tab(label: string) {
    const found = Array.from(
      document.querySelectorAll<HTMLButtonElement>('[role="tab"]'),
    ).find((element) => element.textContent === label);
    if (!found) throw new Error(`Missing ${label} tab`);
    return found;
  }
  async function chooseTab(label: string) {
    // Exercise the real tab controls without jsdom's unavailable popup layout.
    // Browser coverage opens the picker and checks its focus/dismissal behavior.
    await act(async () => tab(label).click());
    expect(button("Detail sections").getAttribute("aria-expanded")).toBe(
      "false",
    );
  }
  try {
    await act(async () => root.render(<Page />));
    expect(document.querySelectorAll('[role="tablist"]')).toHaveLength(1);
    expect(
      document
        .querySelector('[role="tablist"]')
        ?.getAttribute("aria-orientation"),
    ).toBe("vertical");
    expect(
      button("Detail sections").closest("[data-mobile-detail-footer]"),
    ).not.toBeNull();
    await chooseTab("Scenes");
    expect(scrollIntoView).toHaveBeenCalledOnce();
    await act(async () => button("Count 0").click());
    await chooseTab("Images");
    expect(tab("Images").getAttribute("aria-selected")).toBe("true");
    expect(scrollIntoView).toHaveBeenCalled();
    await chooseTab("Scenes");
    const counter = button("Count 1");

    viewport.mobile = false;
    await act(async () => root.render(<Page />));
    expect(container.querySelectorAll('[role="tablist"]')).toHaveLength(1);
    expect(container.querySelector("[data-mobile-detail-footer]")).toBeNull();
    expect(tab("Scenes").closest("[data-page-content]")).not.toBeNull();
    expect(button("Count 1")).toBe(counter);

    viewport.mobile = true;
    await act(async () => root.render(<Page />));
    expect(document.querySelectorAll('[role="tablist"]')).toHaveLength(1);
    expect(button("Count 1")).toBe(counter);
    await act(async () => button("Back").click());
    expect(onBack).toHaveBeenCalledOnce();
  } finally {
    await act(async () => root.unmount());
    container.remove();
    HTMLElement.prototype.scrollIntoView = originalScroll;
    vi.unstubAllGlobals();
  }
});
