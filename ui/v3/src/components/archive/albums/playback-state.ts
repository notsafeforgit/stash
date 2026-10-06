import type { AlbumSlot } from "@/core/native-archive/source-album-api";

export type AlbumPlaybackState =
  | { kind: "image" | "scene"; id: string; uuid: string }
  | {
      kind: "unavailable";
      reason:
        | "gap"
        | "rejected"
        | "conflict"
        | "excluded"
        | "deleted"
        | "unselected"
        | "no_files";
    };

// Source type is a hint: a converted image may select a library video. A
// registered file permits an explicit playback attempt, not an online claim.
export function albumPlaybackState(slot: AlbumSlot): AlbumPlaybackState {
  const unavailable = (
    reason: Extract<AlbumPlaybackState, { kind: "unavailable" }>["reason"],
  ): AlbumPlaybackState => ({ kind: "unavailable", reason });
  if (!slot.attachment) return unavailable("gap");
  if (
    slot.selection_state === "unlinked" ||
    slot.post_link_state === "unlinked"
  )
    return unavailable("rejected");
  if (slot.post_link_state === "conflict") return unavailable("conflict");
  if (slot.gallery_membership === "excluded") return unavailable("excluded");
  if (slot.media?.state === "deleted") return unavailable("deleted");
  if (
    slot.selection_state !== "linked" ||
    !slot.media?.local_id ||
    slot.media.kind === "gallery"
  )
    return unavailable("unselected");
  if (!slot.registered_files) return unavailable("no_files");
  return {
    kind: slot.media.kind,
    id: String(slot.media.local_id),
    uuid: slot.media.uuid,
  };
}
