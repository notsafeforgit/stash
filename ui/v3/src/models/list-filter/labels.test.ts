import { createIntl } from "react-intl";
import { describe, expect, it } from "vitest";
import { FilterMode } from "@/core/generated-graphql";
import enGB from "@/locales/en-GB.json";
import flattenMessages from "@/utils/flatten-messages";
import { getFilterOptions } from "./factory";
import {
  formatFilterModeLabel,
  formatSortLabel,
  formatSortOptions,
} from "./labels";

const messages = flattenMessages(enGB);
const intl = createIntl({ locale: "en-GB", messages });

describe("list filter labels", () => {
  it("has base translations for every mode and advertised sort option", () => {
    for (const mode of Object.values(FilterMode)) {
      expect(formatFilterModeLabel(intl, mode)).not.toBe(mode);
      for (const option of getFilterOptions(mode).sortByOptions) {
        expect(messages[option.messageID], option.messageID).toBeTruthy();
      }
    }
  });

  it("uses canonical message IDs in the active locale while preserving stored values", () => {
    const overridden = createIntl({
      locale: "en-GB",
      messages: {
        ...messages,
        created_at: "Added",
        random: "Shuffle",
        scene_code: "Code",
        scenes: "Videos",
      },
    });
    const options = getFilterOptions(FilterMode.Scenes).sortByOptions;
    expect(formatSortOptions(intl, options)).toEqual(
      expect.arrayContaining([
        { value: "created_at", label: "Created At" },
        { value: "o_counter", label: "O Count" },
        { value: "code", label: "Studio Code" },
      ]),
    );
    expect(formatSortOptions(overridden, options)).toEqual(
      expect.arrayContaining([
        { value: "created_at", label: "Added" },
        { value: "random", label: "Shuffle" },
        { value: "code", label: "Code" },
      ]),
    );
    expect(formatFilterModeLabel(overridden, FilterMode.Scenes)).toBe("Videos");
  });

  it("uses a readable fallback for an unavailable sort", () => {
    expect(formatSortLabel(intl, undefined)).toBe("Unavailable sort");
  });
});
