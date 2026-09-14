import { z } from "zod";
import type { ApolloClient } from "@apollo/client";
import * as GQL from "../generated-graphql";
import {
  ListFilterModel,
  type SavedFilterLike,
} from "@/models/list-filter/filter";
import type { TvFilterChoice, TvMode, TvSettings } from "./settings";
import type { FilterASTNode } from "@/models/list-filter/filter-ast";

export interface TvFeedQuery {
  seed: number;
  mode: TvMode;
  filter: GQL.FindFilterType;
  ast?: GQL.FilterAstInput;
  pageSize: number;
  prefetch: number;
  itemLimit: number | null;
}
export const tvFilterMode = (mode: TvMode) =>
  mode === "scenes" ? GQL.FilterMode.Scenes : GQL.FilterMode.SceneMarkers;
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
  mode: TvMode,
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

export async function resolveTvQuery(
  client: ApolloClient,
  configuration: GQL.ConfigDataFragment,
  settings: TvSettings,
  mode: TvMode,
  choice: TvFilterChoice,
  seed: number,
  orientation: "portrait" | "landscape",
): Promise<TvFeedQuery> {
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
  if (choice.kind === "default") {
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
    choice.kind === "saved"
      ? await getSaved(choice.id)
      : choice.kind === "default"
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
  if (settings.shuffle) model.sortBy = "random";
  model.randomSeed = seed;
  model.itemsPerPage = settings.pageSize;
  const roots: GQL.FilterAstNodeInput[] = [];
  const base = model.makeFilterAST();
  if (base) roots.push(base.root);
  const extra = await Promise.all(
    settings.rules
      .filter((rule) => rule.mode === mode)
      .map((rule) => getSaved(rule.filterId)),
  );
  for (const rule of extra) {
    const extraModel = filterModel(mode, configuration, rule);
    if (extraModel.searchTerm)
      throw new Error(
        "Additional filter rules must use criteria; put text search in the main feed filter",
      );
    const ast = extraModel.makeFilterAST();
    if (ast) roots.push(ast.root);
  }
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
    pageSize: settings.pageSize,
    prefetch: settings.prefetch,
    itemLimit: settings.itemLimit,
  };
}

export function tvQueryIdentity(query: TvFeedQuery): string {
  return JSON.stringify([
    query.seed,
    query.mode,
    query.filter,
    query.ast,
    query.pageSize,
    query.itemLimit,
  ]);
}
