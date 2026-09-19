// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MediaColorMetaRows } from "./media-color-meta-rows";

let container: HTMLDivElement;
let root: Root;
beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});
afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  vi.unstubAllGlobals();
});

it.each([
  ["smpte2084", "HDR 10-bit", "PQ (SMPTE ST 2084)"],
  ["arib-std-b67", "HDR 10-bit", "HLG (ARIB STD-B67)"],
  ["bt709", "SDR 10-bit", "bt709"],
  [null, "Unknown 10-bit", null],
  ["unspecified", "Unknown 10-bit", null],
  ["unknown", "Unknown 10-bit", null],
])(
  "displays %s from the transfer, never bit depth alone",
  async (transfer, label, detail) => {
    await act(async () =>
      root.render(
        <IntlProvider locale="en">
          <dl>
            <MediaColorMetaRows
              file={{ bit_depth: 10, color_transfer: transfer }}
              expanded
            />
          </dl>
        </IntlProvider>,
      ),
    );
    expect(container.querySelector("dd")?.textContent).toBe(label);
    const rows = Array.from(container.querySelectorAll("dl > div"));
    const transferRow = rows.find(
      (row) => row.querySelector("dt")?.textContent === "Transfer",
    );
    expect(transferRow?.querySelector("dd")?.textContent ?? null).toBe(detail);
  },
);

it("localizes the dynamic range and bit-depth labels", async () => {
  await act(async () =>
    root.render(
      <IntlProvider
        locale="fr"
        messages={{
          "media_info.dynamic_range": "Plage dynamique",
          "media_info.bit_depth_value": "{depth} bits",
          unknown: "Inconnue",
        }}
      >
        <dl>
          <MediaColorMetaRows file={{ bit_depth: 10 }} />
        </dl>
      </IntlProvider>,
    ),
  );
  expect(container.querySelector("dt")?.textContent).toBe("Plage dynamique");
  expect(container.querySelector("dd")?.textContent).toBe("Inconnue 10 bits");
});
