import * as GQL from "src/core/generated-graphql";

export const SCRAPER_PREFIX = "scraper:";
export const STASH_BOX_PREFIX = "stashbox:";
export const AUTOTAG_SCRAPER_ID = "builtin_autotag";

export interface IScraperSource {
  id: string;
  displayName: string;
  stash_box_endpoint?: string;
  scraper_id?: string;
  options?: GQL.IdentifyMetadataOptionsInput;
}

export const sceneFields = [
  "title",
  "code",
  "date",
  "director",
  "details",
  "url",
  "studio",
  "performers",
  "tags",
  "stash_ids",
] as const;
export type SceneField = (typeof sceneFields)[number];

export const multiValueSceneFields: SceneField[] = [
  "studio",
  "performers",
  "tags",
];

export function sceneFieldMessageID(field: SceneField) {
  if (field === "code") return "scene_code";
  if (field === "studio") return "studio_and_parent";
  return field;
}

export const ALL_GENDERS: GQL.GenderEnum[] = [
  GQL.GenderEnum.Male,
  GQL.GenderEnum.Female,
  GQL.GenderEnum.TransgenderMale,
  GQL.GenderEnum.TransgenderFemale,
  GQL.GenderEnum.Intersex,
  GQL.GenderEnum.NonBinary,
];

export const STRATEGIES: GQL.IdentifyFieldStrategy[] = [
  GQL.IdentifyFieldStrategy.Ignore,
  GQL.IdentifyFieldStrategy.Merge,
  GQL.IdentifyFieldStrategy.Overwrite,
];

export function strategyLabel(s: GQL.IdentifyFieldStrategy): string {
  switch (s) {
    case GQL.IdentifyFieldStrategy.Ignore:
      return "Ignore";
    case GQL.IdentifyFieldStrategy.Merge:
      return "Merge";
    case GQL.IdentifyFieldStrategy.Overwrite:
      return "Overwrite";
  }
}

export function getDefaultOptions(): GQL.IdentifyMetadataOptionsInput {
  return {
    fieldOptions: [
      { field: "title", strategy: GQL.IdentifyFieldStrategy.Overwrite },
      {
        field: "studio",
        strategy: GQL.IdentifyFieldStrategy.Merge,
        createMissing: true,
      },
      {
        field: "performers",
        strategy: GQL.IdentifyFieldStrategy.Merge,
        createMissing: true,
      },
      {
        field: "tags",
        strategy: GQL.IdentifyFieldStrategy.Merge,
        createMissing: true,
      },
    ],
    performerGenders: undefined,
    setCoverImage: true,
    setOrganized: false,
    skipMultipleMatches: true,
    skipMultipleMatchTag: undefined,
    skipSingleNamePerformers: true,
    skipSingleNamePerformerTag: undefined,
  };
}
