import { useMsg } from "@/hooks/message";

export function useSavedActionLabels() {
  const msg = useMsg();
  return {
    families: {
      account_ownership: msg(
        "saved_actions.account_ownership",
        "Account ownership",
      ),
      account_merge: msg("saved_actions.account_merge", "Account merges"),
      metadata: msg("saved_actions.metadata", "Metadata edits"),
      source_link: msg("saved_actions.source_link", "Media source links"),
      album: msg("saved_actions.album", "Album matching"),
      attachment_selection: msg(
        "saved_actions.attachment_selection",
        "Post attachment selections",
      ),
      gallery_association: msg(
        "saved_actions.gallery_association",
        "Post gallery links",
      ),
      attachment_media: msg(
        "saved_actions.attachment_media",
        "Attachment media links",
      ),
      post_merge: msg("saved_actions.post_merge", "Post merges"),
      merge_notification: msg(
        "saved_actions.merge_notification",
        "Post-merge notifications",
      ),
      collection: msg("saved_actions.collection", "Source collection changes"),
      media_root: msg("saved_actions.media_root", "Media root changes"),
      metadata_policy: msg(
        "saved_actions.metadata_policy",
        "Import policy changes",
      ),
      manual_intake: msg("saved_actions.manual_intake", "Local file batches"),
    },
    states: {
      pending: msg("saved_actions.pending", "Awaiting confirmation"),
      rejected: msg("saved_actions.rejected", "Needs a new review"),
      admitted: msg("saved_actions.admitted", "Work admitted"),
      batch: msg("saved_actions.batch", "Saved file batch"),
      unreadable: msg("saved_actions.unreadable", "Cannot read saved action"),
    },
  };
}
