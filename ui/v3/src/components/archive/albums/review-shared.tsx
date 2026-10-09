import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import type { AlbumIdentity } from "@/core/native-archive/album-review-api";
import { NativeArchiveError } from "@/core/native-archive/client";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { LibraryLink } from "../posts/library-link";

export function useAlbumLabels() {
  const msg = useMsg();
  return {
    policy: {
      "source-identifiers-v1": msg("album_review.source_ids", "Source IDs"),
      "legacy-reddit-filename-v1": msg(
        "album_review.reddit_names",
        "Reddit file names",
      ),
      "legacy-twitter-filename-v1": msg(
        "album_review.twitter_names",
        "Twitter file names",
      ),
    },
    action: {
      create: msg("album_review.create", "Create album gallery"),
      sync: msg("album_review.sync", "Update album gallery"),
      disabled: msg(
        "album_review.disabled",
        "Album synchronization is disabled",
      ),
      ineligible: msg(
        "album_review.ineligible",
        "No eligible album source list",
      ),
      review: msg(
        "album_review.review",
        "Resolve existing album conflicts first",
      ),
    },
    match: {
      matched: msg("album_review.matched", "Verified match"),
      preserved: msg("album_review.preserved", "Keep saved choice"),
      ambiguous: msg("album_review.ambiguous", "Multiple candidates"),
      review: msg("album_review.needs_review", "Needs review"),
      unavailable: msg("album_review.unavailable", "No current match"),
    },
    reason: {
      "multiple-media-candidates": msg(
        "album_review.multiple_candidates",
        "More than one library item matches. No item will be selected automatically.",
      ),
      "no-current-file-proof": msg(
        "album_review.no_file_proof",
        "Retained evidence does not verify a current file belonging to this item.",
      ),
      "post-media-unlinked": msg(
        "album_review.post_unlinked",
        "A saved choice rejects this post's link to the media.",
      ),
      "post-media-conflict": msg(
        "album_review.post_conflict",
        "The post has conflicting saved media links.",
      ),
      "media-kind-conflict": msg(
        "album_review.kind_conflict",
        "The library item's media type does not match the source attachment.",
      ),
    },
    proof: {
      valid: msg("album_review.proof_valid", "Current file verified"),
      "file-changed": msg("album_review.file_changed", "File identity changed"),
      "owner-changed": msg(
        "album_review.owner_changed",
        "File ownership changed",
      ),
      "media-unavailable": msg(
        "album_review.media_unavailable",
        "Library item unavailable",
      ),
      "evidence-only": msg(
        "album_review.evidence_only",
        "Retained evidence only",
      ),
    },
    basis: {
      "source-id": msg(
        "album_review.source_id_basis",
        "Source media identifier",
      ),
      "legacy-reddit-filename": msg(
        "album_review.filename_basis",
        "Original Reddit file name",
      ),
      "legacy-twitter-filename": msg(
        "album_review.twitter_filename_basis",
        "Original numbered Twitter file name",
      ),
      "attachment-evidence": msg(
        "album_review.attachment_basis",
        "Attachment evidence",
      ),
    },
    state: {
      queued: msg("album_review.queued", "Queued"),
      running: msg("album_review.running", "Running"),
      succeeded: msg("album_review.succeeded", "Finished"),
      failed: msg("album_review.failed", "Failed"),
      cancelled: msg("album_review.cancelled", "Cancelled"),
      retry: msg("album_review.retry_wait", "Retry scheduled"),
      expired: msg("album_review.expired", "Worker lease expired"),
    },
  };
}

export function AlbumReviewError({
  error,
  retry,
}: {
  error: unknown;
  retry?: () => void;
}) {
  const msg = useMsg();
  const code = error instanceof NativeArchiveError ? error.code : "";
  const changed =
    code === "album_preview_changed" || code === "album_cancel_changed";
  return (
    <Alert variant="destructive">
      <AlertTitle>
        {changed
          ? msg("album_review.changed", "This album operation changed")
          : msg(
              "album_review.step_failed",
              "Could not complete this album step",
            )}
      </AlertTitle>
      <AlertDescription>
        <p>
          {code === "album_review_limit"
            ? msg(
                "album_review.limit",
                "This post exceeds the bounded matching limit. No partial candidate list will be applied.",
              )
            : changed
              ? msg(
                  "album_review.changed_help",
                  "The saved request was rejected. Review the current state before submitting a new request.",
                )
              : msg(
                  "album_review.failure_help",
                  "Check your connection and access to Stash. Any unconfirmed request stays saved in this browser; use its recovery action before submitting another change.",
                )}
        </p>
        {retry && (
          <Button type="button" variant="outline" size="sm" onClick={retry}>
            {msg("album_review.refresh", "Refresh album status")}
          </Button>
        )}
      </AlertDescription>
    </Alert>
  );
}

export function AlbumItemLink({ item }: { item: AlbumIdentity }) {
  const intl = useIntl();
  const msg = useMsg();
  if (item.state !== "active" || item.local_id === undefined)
    return (
      <code className="wrap-anywhere text-xs" data-selectable-text>
        {item.uuid}
      </code>
    );
  const kind =
    item.kind === "scene"
      ? msg("source_albums.video", "Video")
      : item.kind === "image"
        ? msg("source_albums.image", "Image")
        : msg("gallery", "Gallery");
  return (
    <LibraryLink
      item={{
        ...item,
        state: "active",
        local_id: item.local_id,
        title: "",
        title_truncated: false,
      }}
    >
      {intl.formatMessage(
        { id: "album_review.item", defaultMessage: "{kind} #{id}" },
        { kind, id: item.local_id },
      )}
    </LibraryLink>
  );
}
