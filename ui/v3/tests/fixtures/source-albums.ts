import type {
  AlbumPage,
  AlbumSlot,
} from "../../src/core/native-archive/source-album-api";
import { postAlbum, postIds } from "./source-posts";

export function albumUUID(value: number) {
  return `10000000-0000-4000-8000-${String(value).padStart(12, "0")}`;
}
export function albumSlot(position: number): AlbumSlot {
  return {
    position,
    through: position,
    attachment: {
      uuid: albumUUID(100 + position),
      revision: 2,
      reference: { namespace: "native:reddit", value: `media-${position}` },
    },
    media_kind: "image",
    selection_state: "linked",
    decision_uuid: albumUUID(200 + position),
    media: {
      uuid: albumUUID(1),
      kind: "image",
      state: "active",
      revision: 2,
      local_id: 7,
      title: "First image",
      title_truncated: false,
    },
    post_link_state: "undecided",
    gallery_membership: "included",
    registered_files: 1,
  };
}
export function albumPage(): AlbumPage {
  const image = albumSlot(0);
  const video = albumSlot(1);
  video.media_kind = "video";
  video.media = {
    ...video.media!,
    uuid: albumUUID(2),
    kind: "scene",
    local_id: 8,
    title: "Second video",
  };
  const repeated = { ...image, position: 2, through: 2 };
  const gap: AlbumSlot = {
    position: 3,
    through: 5,
    attachment: null,
    media_kind: "unknown",
    selection_state: "unknown",
    decision_uuid: null,
    media: null,
    post_link_state: "",
    gallery_membership: "no_gallery",
    registered_files: 0,
  };
  const missing: AlbumSlot = {
    ...albumSlot(6),
    selection_state: "unselected",
    decision_uuid: null,
    media: null,
    post_link_state: "",
    gallery_membership: "absent",
    registered_files: 0,
  };
  return {
    post_uuid: postIds.post,
    post_revision: 10,
    post_state: "active",
    signature: "a".repeat(64),
    selection: {
      uuid: albumUUID(3),
      revision: 2,
      mode: "automatic",
      complete: false,
      declared_album: true,
      expected_count: 7,
      entry_count: 4,
    },
    album: postAlbum(),
    slots: [image, video, repeated, gap, missing],
    next_after: null,
  };
}
