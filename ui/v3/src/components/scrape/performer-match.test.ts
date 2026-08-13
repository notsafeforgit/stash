import { describe, expect, it } from "vitest";
import * as GQL from "src/core/generated-graphql";
import { getPerformerCollisions } from "./performer-match";

const local = {
  stash_ids: [{ endpoint: "https://example.test/graphql", stash_id: "local" }],
  birthdate: "1990-01-02",
  country: "US",
  ethnicity: "Caucasian",
  gender: GQL.GenderEnum.Female,
};

describe("getPerformerCollisions", () => {
  it("reports differing identity fields and a stash-id mismatch", () => {
    expect(
      getPerformerCollisions(
        {
          remote_site_id: "remote",
          birthdate: "1991-01-02",
          country: "CA",
          ethnicity: "Asian",
          gender: "MALE",
        },
        local,
        "https://example.test/graphql",
      ),
    ).toEqual([
      "stash_mismatch",
      "birthdate",
      "country",
      "gender",
      "ethnicity",
    ]);
  });

  it("ignores empty remote fields and case-only differences", () => {
    expect(
      getPerformerCollisions(
        {
          remote_site_id: null,
          birthdate: null,
          country: "us",
          ethnicity: "caucasian",
          gender: "female",
        },
        local,
        "https://example.test/graphql",
      ),
    ).toEqual([]);
  });
});
