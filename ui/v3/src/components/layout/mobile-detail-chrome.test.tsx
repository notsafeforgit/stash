// @vitest-environment jsdom
import { act, useState } from "react";
import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { expect, it, vi } from "vitest";
import { DetailTabs } from "@/components/detail/detail-tabs";
import { Button } from "@/components/ui/button";
import {
  MobileDetailChromeProvider,
  MobileDetailFooter,
} from "./mobile-detail-chrome";

const viewport = vi.hoisted(() => ({ mobile: true }));
vi.mock("@/utils/screen", () => ({
  useMediaQuery: () => viewport.mobile,
}));

function StatefulPanel() {
  const [value, setValue] = useState(0);
  return <Button onClick={() => setValue(value + 1)}>Count {value}</Button>;
}

it("keeps one working tab strip and preserves panel state across tabs and breakpoints", async () => {
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
      <IntlProvider locale="en" messages={{ "actions.back": "Back" }}>
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
    const found = Array.from(container.querySelectorAll("button")).find(
      (element) =>
        element.textContent === label ||
        element.getAttribute("aria-label") === label,
    );
    if (!found) throw new Error(`Missing ${label} control`);
    return found;
  }
  try {
    await act(async () => root.render(<Page />));
    expect(container.querySelectorAll('[role="tablist"]')).toHaveLength(1);
    expect(
      button("Scenes").closest("[data-mobile-detail-footer]"),
    ).not.toBeNull();
    await act(async () => button("Scenes").click());
    expect(scrollIntoView).toHaveBeenCalledOnce();
    await act(async () => button("Count 0").click());
    await act(async () => button("Images").click());
    expect(button("Images").getAttribute("aria-selected")).toBe("true");
    expect(scrollIntoView).toHaveBeenCalled();
    await act(async () => button("Scenes").click());
    const counter = button("Count 1");

    viewport.mobile = false;
    await act(async () => root.render(<Page />));
    expect(container.querySelectorAll('[role="tablist"]')).toHaveLength(1);
    expect(container.querySelector("[data-mobile-detail-footer]")).toBeNull();
    expect(button("Scenes").closest("[data-page-content]")).not.toBeNull();
    expect(button("Count 1")).toBe(counter);

    viewport.mobile = true;
    await act(async () => root.render(<Page />));
    expect(container.querySelectorAll('[role="tablist"]')).toHaveLength(1);
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
