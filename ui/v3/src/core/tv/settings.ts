import { z } from "zod";
import { playerQualitySchema } from "../player-quality";
import { defaultTvRail, tvRailSchema } from "./action-config";

export const tvModeSchema = z.enum(["scenes", "markers"]);
export type TvMode = z.infer<typeof tvModeSchema>;
export const tvFilterSchema = z.discriminatedUnion("kind", [
  z.object({ kind: z.literal("default") }),
  z.object({ kind: z.literal("all") }),
  z.object({ kind: z.literal("saved"), id: z.string().regex(/^[1-9]\d*$/) }),
]);
export type TvFilterChoice = z.infer<typeof tvFilterSchema>;
export const tvWindowSchema = z.discriminatedUnion("kind", [
  z.object({ kind: z.literal("full") }),
  z.object({
    kind: z.literal("fixed"),
    seconds: z.number().positive().max(86400),
  }),
  z
    .object({
      kind: z.literal("random"),
      min: z.number().positive().max(86400),
      max: z.number().positive().max(86400),
    })
    .refine(
      (value) => value.min <= value.max,
      "Minimum duration must not exceed maximum",
    ),
]);
export const tvSettingsSchema = z.object({
  version: z.literal(2),
  mode: tvModeSchema,
  sceneFilter: tvFilterSchema,
  markerFilter: tvFilterSchema,
  orientation: z.enum(["all", "match", "portrait", "landscape"]),
  sort: z.string().max(60).nullable(),
  direction: z.enum(["ASC", "DESC"]),
  /** Extra saved filters are AND-ed using the existing nested AST, including
   * its include/exclude modifiers. There is no second expression language. */
  rules: z
    .array(
      z.object({
        kind: z.literal("filter"),
        mode: tvModeSchema,
        filterId: z.string().regex(/^[1-9]\d*$/),
      }),
    )
    .max(10),
  pageSize: z.number().int().min(5).max(50),
  prefetch: z.number().int().min(1).max(5),
  itemLimit: z.number().int().positive().max(100000).nullable(),
  autoplay: z.boolean(),
  start: z.enum(["resume", "beginning", "random-marker", "random-position"]),
  window: tvWindowSchema,
  completion: z.enum(["normal", "advance", "loop"]),
  defaultQuality: playerQualitySchema,
  fit: z.enum(["contain", "cover"]),
  leftHanded: z.boolean(),
  uiVisible: z.boolean(),
  rail: tvRailSchema,
});
export type TvSettings = z.infer<typeof tvSettingsSchema>;

// Version 1 exposed two controls for random ordering. Preserve the effective
// choice when reading it; forms and new saves use only the canonical sort.
const persistedTvSettingsSchema = z.union([
  tvSettingsSchema,
  tvSettingsSchema
    .extend({ version: z.literal(1), shuffle: z.boolean() })
    .transform(
      ({ shuffle, ...settings }): TvSettings => ({
        ...settings,
        version: 2,
        sort: shuffle ? "random" : settings.sort,
      }),
    ),
]);

export const defaultTvSettings: TvSettings = {
  version: 2,
  mode: "scenes",
  sceneFilter: { kind: "default" },
  markerFilter: { kind: "default" },
  orientation: "all",
  sort: null,
  direction: "ASC",
  rules: [],
  pageSize: 20,
  prefetch: 2,
  itemLimit: null,
  autoplay: true,
  start: "resume",
  window: { kind: "full" },
  completion: "advance",
  defaultQuality: { kind: "best" },
  fit: "contain",
  leftHanded: false,
  uiVisible: true,
  rail: defaultTvRail,
};

export type TvSettingsResult =
  | { kind: "ready"; settings: TvSettings }
  | { kind: "invalid"; message: string; raw: unknown };
export function decodeTvSettings(raw: unknown): TvSettingsResult {
  if (raw === undefined || raw === null)
    return { kind: "ready", settings: defaultTvSettings };
  const result = persistedTvSettingsSchema.safeParse(raw);
  return result.success
    ? { kind: "ready", settings: result.data }
    : {
        kind: "invalid",
        message: result.error.issues.map((issue) => issue.message).join("; "),
        raw,
      };
}

export const tvRotationSchema = z.enum([
  "normal",
  "clockwise",
  "counterclockwise",
]);
export type TvRotation = z.infer<typeof tvRotationSchema>;
export const tvSearchSchema = z.object({
  mode: tvModeSchema.optional(),
  filter: z
    .string()
    .regex(/^(default|all|[1-9]\d*)$/)
    .optional(),
  seed: z.coerce.number().int().min(0).max(2147483646).optional(),
  item: z
    .string()
    .regex(/^(scene|marker):[1-9]\d*$/)
    .optional(),
});
export type TvSearch = z.infer<typeof tvSearchSchema>;
