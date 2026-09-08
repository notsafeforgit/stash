import type {
  PerformerDataFragment,
  PerformerUpdateInput,
} from "@/core/generated-graphql";
import { describe, expect, it } from "vitest";
import { PERFORMER_MERGE_FIELDS } from "./performer-merge-fields";

function mergeField(key: string) {
  const field = PERFORMER_MERGE_FIELDS.find(
    (candidate) => candidate.key === key,
  );
  if (!field) throw new Error(`missing performer merge field ${key}`);
  return field;
}

const performer: PerformerDataFragment = {
  id: "1",
  name: "Name",
  favorite: false,
  ignore_auto_tag: false,
  ignore_primary_name_auto_tag: false,
  scene_count: 0,
  image_count: 0,
  gallery_count: 0,
  group_count: 0,
  performer_count: 0,
  created_at: "2026-01-01",
  updated_at: "2026-01-01",
  custom_fields: {},
  aliases: [],
  tags: [],
  stash_ids: [],
};
function combine(
  key: string,
  destination: Partial<PerformerDataFragment>,
  source: Partial<PerformerDataFragment>,
) {
  const row = mergeField(key).resolve(
    { ...performer, ...destination },
    [{ id: "2", label: "Source", entity: { ...performer, ...source } }],
    true,
  );
  if (!row) throw new Error("Expected a conflict");
  expect(row.canCombine).toBe(true);
  const update: PerformerUpdateInput = { id: "1" };
  row.apply(update, "combine");
  return update;
}

describe("performer merge fields", () => {
  it("covers every value protected by the safe merge API", () => {
    const keys = new Set(PERFORMER_MERGE_FIELDS.map((field) => field.key));
    for (const key of [
      "disambiguation",
      "gender",
      "birthdate",
      "death_date",
      "ethnicity",
      "country",
      "eye_color",
      "hair_color",
      "height_cm",
      "weight",
      "measurements",
      "fake_tits",
      "penis_length",
      "circumcised",
      "career_start",
      "career_end",
      "tattoos",
      "piercings",
      "favorite",
      "rating100",
      "details",
      "ignore_auto_tag",
      "urls",
      "aliases",
      "stash_ids",
      "custom_fields",
      "image_path",
    ]) {
      expect(keys.has(key), key).toBe(true);
    }
  });

  it("combines stash IDs without dropping distinct source endpoints", () => {
    const result = combine(
      "stash_ids",
      {
        stash_ids: [
          {
            endpoint: "https://shared.example/graphql",
            stash_id: "destination-id",
            updated_at: "2026-01-01T00:00:00Z",
          },
        ],
      },
      {
        stash_ids: [
          {
            endpoint: "https://shared.example/graphql",
            stash_id: "source-conflict",
            updated_at: "2026-01-02T00:00:00Z",
          },
          {
            endpoint: "https://source.example/graphql",
            stash_id: "source-id",
            updated_at: "2026-01-03T00:00:00Z",
          },
        ],
      },
    ).stash_ids;

    expect(result).toMatchObject([
      {
        endpoint: "https://shared.example/graphql",
        stash_id: "destination-id",
      },
      {
        endpoint: "https://source.example/graphql",
        stash_id: "source-id",
      },
    ]);
  });

  it("combines custom fields while retaining the destination on key conflicts", () => {
    const result = combine(
      "custom_fields",
      { custom_fields: { shared: "destination", destination_only: 1 } },
      { custom_fields: { shared: "source", source_only: 2 } },
    ).custom_fields?.full;

    expect(result).toEqual({
      shared: "destination",
      destination_only: 1,
      source_only: 2,
    });
  });
});
