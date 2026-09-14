import type { IntlShape } from "react-intl";
import { FilterMode } from "@/core/generated-graphql";
import type { ISortByOption } from "./filter-options";

const modeMessageIds: Record<FilterMode, string> = {
  [FilterMode.Scenes]: "scenes",
  [FilterMode.SceneMarkers]: "markers",
  [FilterMode.Images]: "images",
  [FilterMode.Galleries]: "galleries",
  [FilterMode.Performers]: "performers",
  [FilterMode.Studios]: "studios",
  [FilterMode.Movies]: "groups",
  [FilterMode.Groups]: "groups",
  [FilterMode.Tags]: "tags",
};

export function formatFilterModeLabel(intl: IntlShape, mode: FilterMode) {
  return intl.formatMessage({ id: modeMessageIds[mode] });
}

export function formatSortLabel(
  intl: IntlShape,
  option: ISortByOption | undefined,
) {
  const unavailable = intl.formatMessage({
    id: "search_filter.unavailable_sort",
    defaultMessage: "Unavailable sort",
  });
  return option
    ? intl.formatMessage({ id: option.messageID, defaultMessage: unavailable })
    : unavailable;
}

export function formatSortOptions(
  intl: IntlShape,
  options: readonly ISortByOption[],
) {
  return options.map((option) => ({
    value: option.value,
    label: formatSortLabel(intl, option),
  }));
}
