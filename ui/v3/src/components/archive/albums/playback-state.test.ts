import { describe, expect, it } from "vitest";
import { albumPage, albumSlot } from "../../../../tests/fixtures/source-albums";
import { albumPlaybackState } from "./playback-state";

describe("source album playback eligibility", () => {
  it("keeps repeated positions and source gaps without inventing playable items", () => {
    const slots = albumPage().slots;
    expect(slots.map(albumPlaybackState).map((state) => state.kind)).toEqual([
      "image",
      "scene",
      "image",
      "unavailable",
      "unavailable",
    ]);
    expect(albumPlaybackState(slots[0]!)).toEqual(
      albumPlaybackState(slots[2]!),
    );
    expect(albumPlaybackState(slots[3]!)).toEqual({
      kind: "unavailable",
      reason: "gap",
    });
    expect(slots[3]!.through).toBe(5);
  });
  it("uses the actual selected library kind after conversion", () => {
    const slot = albumPage().slots[1]!;
    slot.media_kind = "image";
    expect(albumPlaybackState(slot)).toMatchObject({ kind: "scene", id: "8" });
  });
  it.each([
    ["attachment rejection", { selection_state: "unlinked" }, "rejected"],
    ["post rejection", { post_link_state: "unlinked" }, "rejected"],
    ["post conflict", { post_link_state: "conflict" }, "conflict"],
    ["manual exclusion", { gallery_membership: "excluded" }, "excluded"],
    ["no registered files", { registered_files: 0 }, "no_files"],
  ] as const)("does not attempt playback for %s", (_name, changes, reason) => {
    expect(albumPlaybackState({ ...albumSlot(0), ...changes })).toEqual({
      kind: "unavailable",
      reason,
    });
  });
  it("does not use a deleted entity's previous local ID", () => {
    const slot = albumSlot(0);
    slot.media = { ...slot.media!, state: "deleted", local_id: null };
    expect(albumPlaybackState(slot)).toEqual({
      kind: "unavailable",
      reason: "deleted",
    });
  });
});
