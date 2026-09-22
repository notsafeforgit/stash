import { z } from "zod";
import type { ApolloClient } from "@apollo/client";
import * as GQL from "../generated-graphql";
import {
  ListFilterModel,
  type SavedFilterLike,
} from "@/models/list-filter/filter";
import type {
  TvFilterChoice,
  TvMode,
  TvSourceMode,
  TvSettings,
} from "./settings";
import type { FilterASTNode } from "@/models/list-filter/filter-ast";
import { getFilterOptions } from "@/models/list-filter/factory";

// Internal metadata-fetch policy. Keep request size stable for the feed's
// lifetime because the server uses page-based offsets. Media is never preloaded.
const PAGE_SIZE = 20;
const PREFETCH_REMAINING = 2;

interface TvQueryPolicy {
  seed: number;
  pageSize: number;
  prefetch: number;
}
interface TvSourceQuery {
  filter: GQL.FindFilterType;
  ast?: GQL.FilterAstInput;
}
export interface TvSingleFeedQuery extends TvQueryPolicy, TvSourceQuery {
  mode: TvSourceMode;
}
export type TvFeedQuery =
  | TvSingleFeedQuery
  | (TvQueryPolicy & {
      mode: "both";
      scenes: TvSourceQuery;
      markers: TvSourceQuery;
    });
export const tvFilterMode = (mode: TvSourceMode) =>
  mode === "scenes" ? GQL.FilterMode.Scenes : GQL.FilterMode.SceneMarkers;

export function tvSortOptions(mode: TvMode) {
  if (mode !== "both")
    return getFilterOptions(tvFilterMode(mode)).sortByOptions;
  const markers = getFilterOptions(GQL.FilterMode.SceneMarkers).sortByOptions;
  return getFilterOptions(GQL.FilterMode.Scenes).sortByOptions.filter(
    (option) => markers.some((marker) => marker.value === option.value),
  );
}
const conflictSchema = z.object({
  forkDefaultFilterState: z
    .record(
      z.string(),
      z
        .object({ pending_legacy_object_filter: z.unknown().optional() })
        .passthrough(),
    )
    .optional(),
});
const savedNodeSchema: z.ZodType<GQL.FilterAstNodeInput> = z.lazy(() =>
  z.union([
    z.strictObject({
      condition: z.strictObject({
        field: z.string().min(1),
        value: z.unknown(),
      }),
    }),
    z.strictObject({
      group: z.strictObject({
        operator: z.enum(GQL.FilterGroupOperator),
        children: z.array(savedNodeSchema),
      }),
    }),
  ]),
);
const savedAstSchema = z.strictObject({ root: savedNodeSchema });
function validCriteria(node: FilterASTNode): boolean {
  return node.kind === "condition"
    ? node.criterion.isValid()
    : node.children.every(validCriteria);
}

function filterModel(
  mode: TvSourceMode,
  config: GQL.ConfigDataFragment,
  saved?: SavedFilterLike,
) {
  const model = new ListFilterModel(tvFilterMode(mode), config);
  if (saved) {
    if (
      saved.filter_ast != null &&
      !savedAstSchema.safeParse(saved.filter_ast).success
    )
      throw new Error("This saved filter has an invalid criteria tree");
    model.configureFromSavedFilter(saved);
    if (
      model.criteria.some((criterion) => !criterion.isValid()) ||
      (model.filterAst && !validCriteria(model.filterAst))
    )
      throw new Error(
        "This saved filter has invalid criteria; repair it in the library before using it in TV",
      );
  }
  return model;
}

export function resolveTvQuery(
  client: ApolloClient,
  configuration: GQL.ConfigDataFragment,
  settings: TvSettings,
  mode: TvSourceMode,
  choice: TvFilterChoice | undefined,
  seed: number,
  orientation: "portrait" | "landscape",
): Promise<TvSingleFeedQuery>;
export function resolveTvQuery(
  client: ApolloClient,
  configuration: GQL.ConfigDataFragment,
  settings: TvSettings,
  mode: TvMode,
  choice: TvFilterChoice | undefined,
  seed: number,
  orientation: "portrait" | "landscape",
): Promise<TvFeedQuery>;
export async function resolveTvQuery(
  client: ApolloClient,
  configuration: GQL.ConfigDataFragment,
  requestedSettings: TvSettings,
  mode: TvMode,
  choice: TvFilterChoice | undefined,
  seed: number,
  orientation: "portrait" | "landscape",
): Promise<TvFeedQuery> {
  // A rail switch keeps the saved default intact. A source-specific sort can
  // fall back to the destination's saved order for this viewing session.
  const settings =
    mode !== requestedSettings.mode &&
    requestedSettings.sort &&
    !tvSortOptions(mode).some(
      (option) => option.value === requestedSettings.sort,
    )
      ? { ...requestedSettings, sort: null }
      : requestedSettings;
  if (mode === "both") {
    if (choice?.kind === "saved")
      throw new Error(
        "Choose scene and marker filters separately in TV settings",
      );
    const [scenes, markers] = await Promise.all([
      resolveTvQuery(
        client,
        configuration,
        settings,
        "scenes",
        choice ?? settings.sceneFilter,
        seed,
        orientation,
      ),
      resolveTvQuery(
        client,
        configuration,
        settings,
        "markers",
        choice ?? settings.markerFilter,
        seed,
        orientation,
      ),
    ]);
    return {
      mode,
      seed,
      scenes: { filter: scenes.filter, ast: scenes.ast },
      markers: { filter: markers.filter, ast: markers.ast },
      pageSize: PAGE_SIZE,
      prefetch: PREFETCH_REMAINING,
    };
  }
  const filterChoice =
    choice ??
    (mode === "scenes" ? settings.sceneFilter : settings.markerFilter);
  const view = mode === "scenes" ? "scenes" : "scene_markers";
  const getSaved = async (id: string) => {
    const response = await client.query({
      query: GQL.FindSavedFilterDocument,
      variables: { id },
      fetchPolicy: "cache-first",
    });
    const saved = response.data?.findSavedFilter;
    if (!saved || saved.mode !== tvFilterMode(mode))
      throw new Error(
        "The selected filter is missing or belongs to another feed",
      );
    return saved;
  };
  if (filterChoice.kind === "default") {
    const state = conflictSchema.safeParse(configuration.ui);
    if (
      state.success &&
      state.data.forkDefaultFilterState?.[view] &&
      "pending_legacy_object_filter" in state.data.forkDefaultFilterState[view]
    )
      throw new Error(
        "Resolve the default filter conflict in the library, or select another TV filter",
      );
  }
  const saved =
    filterChoice.kind === "saved"
      ? await getSaved(filterChoice.id)
      : filterChoice.kind === "default"
        ? configuration.ui.defaultFilters?.[view]
        : undefined;
  const model = filterModel(mode, configuration, saved);
  if (settings.sort) {
    if (
      !model.options.sortByOptions.some(
        (option) => option.value === settings.sort,
      )
    )
      throw new Error("This sort is unavailable for the selected feed");
    model.sortBy = settings.sort;
    model.sortDirection =
      settings.direction === "ASC"
        ? GQL.SortDirectionEnum.Asc
        : GQL.SortDirectionEnum.Desc;
  }
  model.randomSeed = seed;
  model.itemsPerPage = PAGE_SIZE;
  const roots: GQL.FilterAstNodeInput[] = [];
  const base = model.makeFilterAST();
  if (base) roots.push(base.root);
  const effectiveOrientation =
    settings.orientation === "match" ? orientation : settings.orientation;
  if (effectiveOrientation !== "all")
    roots.push({
      condition: {
        field: "orientation",
        value: {
          value: [
            GQL.OrientationEnum.Square,
            effectiveOrientation === "portrait"
              ? GQL.OrientationEnum.Portrait
              : GQL.OrientationEnum.Landscape,
          ],
        },
      },
    });
  return {
    seed,
    mode,
    filter: model.makeFindFilter(),
    ast: roots.length
      ? {
          root: {
            group: { operator: GQL.FilterGroupOperator.And, children: roots },
          },
        }
      : undefined,
    pageSize: PAGE_SIZE,
    prefetch: PREFETCH_REMAINING,
  };
}

export function tvQueryIdentity(query: TvFeedQuery): string {
  return JSON.stringify([
    query.seed,
    query.mode,
    query.mode === "both"
      ? [query.scenes, query.markers]
      : [query.filter, query.ast],
    query.pageSize,
  ]);
}
