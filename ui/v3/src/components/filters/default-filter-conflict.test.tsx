// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { expect, it, vi } from "vitest";
import { DefaultFilterConflict } from "./default-filter-conflict";

it("prevents applying invalid imported criteria while allowing the current default", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  const onUseImported = vi.fn();
  const onKeepCurrent = vi.fn();
  try {
    const render = async (disabled: boolean) => {
      await act(async () => {
        root.render(
          <IntlProvider
            locale="en"
            messages={{
              "default_filter.import_conflict": "Choose a default",
              "default_filter.invalid_import": "Invalid imported criteria",
              "default_filter.use_imported": "Use imported alternative",
              "default_filter.keep_current": "Keep current default",
            }}
          >
            <DefaultFilterConflict
              disabled={disabled}
              canUseImported={false}
              onUseImported={onUseImported}
              onKeepCurrent={onKeepCurrent}
            />
          </IntlProvider>,
        );
      });
    };
    await render(false);
    expect(container.textContent).toContain("Invalid imported criteria");
    const [useImported, keepCurrent] = container.querySelectorAll("button");
    if (!useImported || !keepCurrent) throw new Error("Missing review actions");
    expect(useImported.disabled).toBe(true);
    expect(keepCurrent.disabled).toBe(false);
    await act(async () => {
      useImported.click();
      keepCurrent.click();
    });
    expect(onUseImported).not.toHaveBeenCalled();
    expect(onKeepCurrent).toHaveBeenCalledTimes(1);
    await render(true);
    expect(keepCurrent.disabled).toBe(true);
  } finally {
    await act(async () => root.unmount());
    container.remove();
    vi.unstubAllGlobals();
  }
});
