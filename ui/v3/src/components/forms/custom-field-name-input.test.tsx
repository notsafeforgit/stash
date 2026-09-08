// @vitest-environment jsdom
import { act, useState, type ComponentProps } from "react";
import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { expect, it, vi } from "vitest";
import type { Combobox } from "@/components/ui/combobox";
import { CustomFieldNameInput } from "./custom-field-name-input";

type InputChangeDetails = Parameters<
  NonNullable<ComponentProps<typeof Combobox<string>>["onInputValueChange"]>
>[1];

function inputDetails(
  reason: "input-change" | "input-clear",
): InputChangeDetails {
  return {
    reason,
    event: new Event("input"),
    trigger: undefined,
    cancel() {},
    allowPropagation() {},
    isCanceled: false,
    isPropagationAllowed: false,
  };
}

// Exercise the controlled combobox contract without popup layout/animations.
// Base UI's real locale-aware filter is retained.
vi.mock("@/components/ui/combobox", async () => {
  const actual = await vi.importActual<
    typeof import("@/components/ui/combobox")
  >("@/components/ui/combobox");
  return {
    ...actual,
    Combobox: ({
      inputValue,
      value,
      filteredItems,
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
        <output data-testid="suggestions">
          {JSON.stringify(filteredItems)}
        </output>
        <button
          type="button"
          onClick={() => onInputValueChange?.("", inputDetails("input-clear"))}
        >
          Close query
        </button>
        <button
          type="button"
          onClick={() =>
            onValueChange?.("obsolete", inputDetails("input-change"))
          }
        >
          Choose suggestion
        </button>
      </>
    ),
  };
});

it("preserves free names through query resets and filters from the latest typed name", async () => {
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
    expect(
      container.querySelector('[data-testid="suggestions"]')?.textContent,
    ).toBe('["obsolete"]');
    await act(async () => container.querySelectorAll("button")[1]?.click());
    expect(input.value).toBe("obsolete");
    expect(
      container.querySelector('[data-testid="selection"]')?.textContent,
    ).toBe("obsolete");
    await type("");
    expect(input.value).toBe("");
    expect(
      container.querySelector('[data-testid="suggestions"]')?.textContent,
    ).toBe('["score","obsolete"]');
  } finally {
    await act(async () => root.unmount());
    container.remove();
    vi.unstubAllGlobals();
  }
});
