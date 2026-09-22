import { z } from "zod";
import { playerQualitySchema } from "../player-quality";
import { defaultTvRail, tvRailSchema } from "./action-config";

export const tvModeSchema = z.enum(["scenes", "markers", "both"]);
export type TvMode = z.infer<typeof tvModeSchema>;
export type TvSourceMode = Exclude<TvMode, "both">;
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
  version: z.literal(5),
  mode: tvModeSchema,
  sceneFilter: tvFilterSchema,
  markerFilter: tvFilterSchema,
  orientation: z.enum(["all", "match", "portrait", "landscape"]),
  sort: z.string().max(60).nullable(),
  direction: z.enum(["ASC", "DESC"]),
  autoplay: z.boolean(),
  // Additive preference: existing saved settings retain their muted startup.
  startMuted: z.boolean().default(true),
  preloadCount: z.union([z.literal(1), z.literal(3), z.literal(5)]).default(5),
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

// Zod strips retired feed rules, paging preferences and session limits.
// Version 1 also had a shuffle override. Preserve its effective sort choice.
const persistedTvSettingsSchema = z.union([
  tvSettingsSchema,
  z
    .union([
      tvSettingsSchema.extend({
        version: z.union([z.literal(2), z.literal(3), z.literal(4)]),
      }),
      tvSettingsSchema
        .extend({ version: z.literal(1), shuffle: z.boolean() })
        .transform(({ shuffle, ...settings }) => ({
          ...settings,
          sort: shuffle ? "random" : settings.sort,
        })),
    ])
    .transform(
      (settings): TvSettings => ({
        ...settings,
        version: 5,
        // Retire the formerly required default gear once. Preserve customized
        // shortcuts, and allow new version-4 settings actions in any position.
        rail:
          settings.version === 4
            ? settings.rail
            : settings.rail.filter(
                (entry) =>
                  !(
                    entry.type === "action" &&
                    entry.pinned &&
                    entry.action.kind === "settings" &&
                    entry.action.id === "settings" &&
                    entry.action.icon === "default" &&
                    entry.action.label === ""
                  ),
              ),
      }),
    ),
]);

export const defaultTvSettings: TvSettings = {
  version: 5,
  mode: "scenes",
  sceneFilter: { kind: "default" },
  markerFilter: { kind: "default" },
  orientation: "all",
  sort: null,
  direction: "ASC",
  autoplay: true,
  startMuted: true,
  preloadCount: 5,
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
