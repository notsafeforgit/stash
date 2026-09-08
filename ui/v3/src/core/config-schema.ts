import { taskDefaultsSchema } from "./task-defaults";
import { z } from "zod";
import { FilterMode, GenderEnum, SortDirectionEnum } from "./generated-graphql";
import type { IUIConfig } from "./config";
import { ImageWallDirection } from "@/utils/image-wall";
import { RatingStarPrecision, RatingSystemType } from "@/utils/rating";
import { initialConfig } from "@/components/tagger/constants";

const strings = z.array(z.string());
const optionalBoolean = z.boolean().optional().catch(undefined);
const optionalString = z.string().optional().catch(undefined);

export const frontPageContentSchema = z.array(
  z.discriminatedUnion("__typename", [
    z.looseObject({
      __typename: z.literal("SavedFilter"),
      // Existing v2.5/v3 configurations persist IDs as numbers or decimal
      // strings. Retain their representation so reading never rewrites them.
      savedFilterId: z.union([
        z.number().int().positive(),
        z.string().regex(/^[1-9]\d*$/),
      ]),
    }),
    z.looseObject({
      __typename: z.literal("CustomFilter"),
      message: z
        .looseObject({
          id: z.string(),
          values: z.record(z.string(), z.string()),
        })
        .optional(),
      title: z.string().optional(),
      mode: z.enum(FilterMode),
      sortBy: z.string(),
      direction: z.enum(SortDirectionEnum),
    }),
  ]),
);

const savedFilterSchema = z.looseObject({
  mode: z.enum(FilterMode).optional(),
  find_filter: z
    .looseObject({
      q: z.string().nullish(),
      page: z.number().int().nullish(),
      per_page: z.number().int().nullish(),
      sort: z.string().nullish(),
      direction: z.enum(SortDirectionEnum).nullish(),
    })
    .nullish(),
  // These formats are intentionally decoded by ListFilterModel's legacy/AST
  // adapters, which understand the criterion-specific values.
  object_filter: z.unknown().optional(),
  filter_ast: z.unknown().optional(),
});

const taggerSchema = z.looseObject({
  blacklist: strings.catch(initialConfig.blacklist),
  performerGenders: z.array(z.enum(GenderEnum)).optional().catch(undefined),
  mode: z.enum(["auto", "filename", "dir", "path", "metadata"]).catch("auto"),
  setCoverImage: z.boolean().catch(true),
  setTags: z.boolean().catch(true),
  tagOperation: z.enum(["merge", "overwrite"]).catch("merge"),
  selectedEndpoint: optionalString,
  fingerprintQueue: z.record(z.string(), strings).catch({}),
  excludedPerformerFields: strings.optional().catch(undefined),
  markSceneAsOrganizedOnSave: optionalBoolean,
  excludedStudioFields: strings.optional().catch(undefined),
  excludedTagFields: strings.optional().catch(undefined),
  createParentStudios: z.boolean().catch(true),
  createParentTags: z.boolean().catch(true),
});

const shape = {
  frontPageContent: frontPageContentSchema.optional().catch(undefined),
  showChildTagContent: optionalBoolean,
  showChildStudioContent: optionalBoolean,
  showLinksOnPerformerCard: optionalBoolean,
  showTagCardOnHover: optionalBoolean,
  showStudioText: optionalBoolean,
  previewVolume: z.number().min(0).max(100).optional().catch(undefined),
  autostartGallerySlideshow: optionalBoolean,
  abbreviateCounters: optionalBoolean,
  ratingSystemOptions: z
    .looseObject({
      type: z.enum(RatingSystemType),
      starPrecision: z.enum(RatingStarPrecision).optional(),
    })
    .optional()
    .catch(undefined),
  enableMovieBackgroundImage: optionalBoolean,
  enablePerformerBackgroundImage: optionalBoolean,
  enableStudioBackgroundImage: optionalBoolean,
  enableTagBackgroundImage: optionalBoolean,
  compactExpandedDetails: optionalBoolean,
  showAllDetails: optionalBoolean,
  enableChromecast: optionalBoolean,
  disableMobileMediaAutoRotateEnabled: optionalBoolean,
  showRangeMarkers: optionalBoolean,
  alwaysStartFromBeginning: optionalBoolean,
  trackActivity: optionalBoolean,
  minimumPlayPercent: z.number().min(0).max(100).optional().catch(undefined),
  showAbLoopControls: optionalBoolean,
  maxOptionsShown: z
    .number()
    .int()
    .min(1)
    .max(1000)
    .optional()
    .catch(undefined),
  imageWallOptions: z
    .looseObject({
      margin: z.number().nonnegative(),
      direction: z.enum(ImageWallDirection),
    })
    .optional()
    .catch(undefined),
  lastNoteSeen: z.number().int().nonnegative().optional().catch(undefined),
  vrTag: optionalString,
  pinnedFilters: z.record(z.string(), strings).optional().catch(undefined),
  tableColumns: z.record(z.string(), strings).optional().catch(undefined),
  advancedMode: optionalBoolean,
  taskDefaults: taskDefaultsSchema.optional().catch(undefined),
  defaultFilters: z
    .record(z.string(), savedFilterSchema.optional().catch(undefined))
    .optional()
    .catch(undefined),
  taggerConfig: taggerSchema.optional().catch(undefined),
  title: optionalString,
} satisfies { [Key in keyof IUIConfig]-?: z.ZodType<IUIConfig[Key]> };

const uiConfigSchema = z.looseObject(shape).catch({});

/** Scalars arrive as untrusted JSON even when codegen supplies a TypeScript
 * name. Preserve extension keys so v3 edits round-trip other clients' fields. */
export function parseUIConfig(value: unknown): IUIConfig {
  return uiConfigSchema.parse(value);
}
