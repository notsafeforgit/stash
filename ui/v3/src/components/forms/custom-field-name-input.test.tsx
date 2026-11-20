// @vitest-environment jsdom
import { act, useState, type ComponentProps } from "react";
import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { expect, it, vi } from "vitest";
import type {
  Combobox,
  ComboboxInputValueChangeDetails,
} from "@/components/ui/combobox";
import { CustomFieldNameInput } from "./custom-field-name-input";

type ValueChangeDetails = Parameters<
  NonNullable<ComponentProps<typeof Combobox<string>>["onValueChange"]>
>[1];

function inputDetails(
  reason: "input-change" | "input-clear",
): ComboboxInputValueChangeDetails {
  return {
    reason,
    event: new Event("input"),
    source: "programmatic",
    cancel() {},
    canceled: false,
  };
}

function valueDetails(): ValueChangeDetails {
  return {
    reason: "item-press",
    source: "programmatic",
    cancel() {},
    canceled: false,
  };
}

// Exercise the controlled combobox contract without popup layout/animations.
// Filtering is ui-sdk's own and runs against the query, which must be the
// typed name, so the mock reports the query and the full option list.
vi.mock("@/components/ui/combobox", async () => {
  const actual = await vi.importActual<
    typeof import("@/components/ui/combobox")
  >("@/components/ui/combobox");
  return {
    ...actual,
    Combobox: ({
      inputValue,
      value,
      items,
      onInputValueChange,
      onValueChange,
    }: ComponentProps<typeof Combobox<string>>) => (
      <>
        <input
          aria-label="Name"
          value={inputValue}
          onChange={(event) =>
            onInputValueChange?.(
              event.target.value,
              inputDetails("input-change"),
            )
          }
        />
        <output data-testid="selection">{value ?? "none"}</output>
        <output data-testid="query">{inputValue}</output>
        <output data-testid="options">{JSON.stringify(items)}</output>
        <button
          type="button"
          onClick={() => onInputValueChange?.("", inputDetails("input-clear"))}
        >
          Close query
        </button>
        <button
          type="button"
          onClick={() => onValueChange?.("obsolete", valueDetails())}
        >
          Choose suggestion
        </button>
      </>
    ),
  };
});

it("preserves free names through query resets and queries from the latest typed name", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  const options = ["score", "obsolete"];
  function Editor() {
    const [value, setValue] = useState("score");
    return (
      <CustomFieldNameInput
        id="name"
        value={value}
        onChange={setValue}
        options={options}
      />
    );
  }
  try {
    await act(async () =>
      root.render(
        <IntlProvider
          locale="en"
          messages={{
            "custom_fields.bulk.name_placeholder": "Name",
            "custom_fields.bulk.type_name": "Enter a name",
          }}
        >
          <Editor />
        </IntlProvider>,
      ),
    );
    const input = container.querySelector("input");
    if (!input) throw new Error("Name input missing");
    async function type(value: string) {
      await act(async () => {
        Object.getOwnPropertyDescriptor(
          HTMLInputElement.prototype,
          "value",
        )?.set?.call(input, value);
        input?.dispatchEvent(new Event("input", { bubbles: true }));
      });
    }
    await type("a new field");
    expect(
      container.querySelector('[data-testid="selection"]')?.textContent,
    ).toBe("none");
    await act(async () => container.querySelectorAll("button")[0]?.click());
    expect(input.value).toBe("a new field");
    await type("obso");
    expect(container.querySelector('[data-testid="query"]')?.textContent).toBe(
      "obso",
    );
    await act(async () => container.querySelectorAll("button")[1]?.click());
    expect(input.value).toBe("obsolete");
    expect(
      container.querySelector('[data-testid="selection"]')?.textContent,
    ).toBe("obsolete");
    await type("");
    expect(input.value).toBe("");
    expect(container.querySelector('[data-testid="query"]')?.textContent).toBe(
      "",
    );
    expect(
      container.querySelector('[data-testid="options"]')?.textContent,
    ).toBe('["score","obsolete"]');
  } finally {
    await act(async () => root.unmount());
    container.remove();
    vi.unstubAllGlobals();
  }
});
