import { z } from "zod";

export const tvActionKinds = [
  "settings",
  "visibility",
  "info",
  "rating",
  "counter",
  "organized",
  "tags",
  "quick-tag",
  "marker",
  "quick-marker",
  "delete",
  "rotation",
  "fullscreen",
  "volume",
  "fit",
  "completion",
  "speed",
  "subtitles",
  "quality",
  "help",
] as const;
export const tvIconIds = [
  "default",
  "heart",
  "star",
  "tag",
  "bookmark",
  "flame",
  "sparkles",
  "check",
  "plus",
] as const;
const shared = {
  id: z.string().min(1).max(100),
  icon: z.enum(tvIconIds).default("default"),
  label: z.string().max(60).default(""),
};
const tagId = z.string().regex(/^[1-9]\d*$/);
export const tvActionSchema = z.discriminatedUnion("kind", [
  z.object({
    ...shared,
    kind: z.enum([
      "settings",
      "visibility",
      "info",
      "rating",
      "counter",
      "organized",
      "tags",
      "marker",
      "delete",
      "rotation",
      "fullscreen",
      "volume",
      "fit",
      "completion",
      "speed",
      "subtitles",
      "quality",
      "help",
    ]),
  }),
  z.object({
    ...shared,
    kind: z.literal("quick-tag"),
    tagId,
    target: z.enum(["additional", "primary"]).default("additional"),
  }),
  z.object({
    ...shared,
    kind: z.literal("quick-marker"),
    title: z.string().min(1).max(200),
    primaryTagId: tagId,
    tagIds: z.array(tagId).max(50),
    duration: z.number().positive().max(86400).nullable(),
  }),
]);
export type TvAction = z.infer<typeof tvActionSchema>;
export type TvActionKind = (typeof tvActionKinds)[number];
export const tvRailSchema = z
  .array(
    z.union([
      z.object({
        type: z.literal("action"),
        pinned: z.boolean(),
        action: tvActionSchema,
      }),
      z.object({
        type: z.literal("folder"),
        id: shared.id,
        label: z.string().min(1).max(60),
        icon: z.enum(tvIconIds),
        pinned: z.boolean(),
        actions: z.array(tvActionSchema).max(30),
      }),
    ]),
  )
  .max(40)
  .superRefine((entries, context) => {
    const ids = new Set<string>();
    const kinds = new Set<TvActionKind>();
    for (const entry of entries) {
      if (entry.type === "folder") {
        if (ids.has(entry.id))
          context.addIssue({
            code: "custom",
            message: "Action and folder IDs must be unique",
          });
        ids.add(entry.id);
      }
      for (const action of entry.type === "folder"
        ? entry.actions
        : [entry.action]) {
        if (ids.has(action.id))
          context.addIssue({
            code: "custom",
            message: "Action and folder IDs must be unique",
          });
        ids.add(action.id);
        if (
          kinds.has(action.kind) &&
          action.kind !== "quick-tag" &&
          action.kind !== "quick-marker"
        )
          context.addIssue({
            code: "custom",
            message: "Only tag and marker presets may repeat",
          });
        kinds.add(action.kind);
        if (entry.type === "folder" && action.kind === "visibility")
          context.addIssue({
            code: "custom",
            message: "Visibility must stay at the top level",
          });
      }
    }
    if (!kinds.has("visibility"))
      context.addIssue({
        code: "custom",
        message: "Keep the visibility action",
      });
  });
export type TvRailEntry = z.infer<typeof tvRailSchema>[number];

export function createTvAction(kind: TvActionKind, id: string): TvAction {
  const common = { id, icon: "default" as const, label: "" };
  switch (kind) {
    case "quick-tag":
      return { ...common, kind, tagId: "", target: "additional" };
    case "quick-marker":
      return {
        ...common,
        kind,
        title: "Marker",
        primaryTagId: "",
        tagIds: [],
        duration: null,
      };
    default:
      return { ...common, kind };
  }
}

export const defaultTvRail: TvRailEntry[] = [
  {
    type: "action",
    pinned: true,
    action: createTvAction("visibility", "visibility"),
  },
  { type: "action", pinned: false, action: createTvAction("info", "info") },
  {
    type: "action",
    pinned: false,
    action: createTvAction("counter", "counter"),
  },
  {
    type: "folder",
    id: "edit",
    label: "Edit",
    icon: "tag",
    pinned: false,
    actions: ["rating", "organized", "tags", "marker", "delete"].map((kind) =>
      tvActionSchema.parse({ id: kind, kind, icon: "default", label: "" }),
    ),
  },
  {
    type: "folder",
    id: "playback",
    label: "Playback",
    icon: "default",
    pinned: false,
    actions: [
      "volume",
      "speed",
      "subtitles",
      "quality",
      "completion",
      "fit",
      "rotation",
      "fullscreen",
      "help",
    ].map((kind) =>
      tvActionSchema.parse({ id: kind, kind, icon: "default", label: "" }),
    ),
  },
];

export function railEntryId(entry: TvRailEntry): string {
  return entry.type === "folder" ? entry.id : entry.action.id;
}
