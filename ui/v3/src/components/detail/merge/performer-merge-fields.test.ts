import { describe, expect, it } from "vitest";
import { PERFORMER_MERGE_FIELDS } from "./performer-merge-fields";

function mergeField(key: string) {
  const field = PERFORMER_MERGE_FIELDS.find(
    (candidate) => candidate.key === key,
  );
  if (!field) throw new Error(`missing performer merge field ${key}`);
  return field;
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
    const combine = mergeField("stash_ids").combine;
    expect(combine).toBeDefined();

    const result = combine?.([
      [
        {
          endpoint: "https://shared.example/graphql",
          stash_id: "destination-id",
          updated_at: "2026-01-01T00:00:00Z",
        },
      ],
      [
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
    ]) as Array<{ endpoint: string; stash_id: string }>;

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
    const combine = mergeField("custom_fields").combine;
    expect(combine).toBeDefined();

    const result = combine?.([
      { shared: "destination", destination_only: 1 },
      { shared: "source", source_only: 2 },
    ]);

    expect(result).toEqual({
      shared: "destination",
      destination_only: 1,
      source_only: 2,
    });
  });
});
