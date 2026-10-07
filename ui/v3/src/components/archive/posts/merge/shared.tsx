import { useState, type ReactNode } from "react";
import { useMsg } from "@/hooks/message";
import type {
  PostMergeEntity,
  PostMergePost,
} from "@/core/native-archive/post-consolidation-schema";
import { Button, buttonVariants } from "@/components/ui/button";
import { Link } from "@tanstack/react-router";
import { LibraryLink } from "../library-link";
import { NativeArchiveError } from "@/core/native-archive/client";
import { ReviewError } from "@/components/detail/native-metadata/shared";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";

export function MergeError({
  error,
  retry,
}: {
  error: unknown;
  retry?: () => void;
}) {
  const msg = useMsg();
  const messages = {
    post_identity_conflict: msg(
      "post_merge.identity_conflict",
      "These records have different identifiers for posts on the same service. Keep them as separate posts.",
    ),
    post_comparison_limit: msg(
      "post_merge.limit",
      "These posts exceed the review size limit. No evidence or choices have been omitted or changed.",
    ),
    notification_job_changed: msg(
      "post_merge.job_changed",
      "Notification delivery changed. Refresh its status before trying again.",
    ),
  };
  const detail =
    error instanceof NativeArchiveError && Object.hasOwn(messages, error.code)
      ? messages[error.code as keyof typeof messages]
      : undefined;
  if (!detail) return <ReviewError error={error} retry={retry} />;
  return (
    <Alert variant="destructive">
      <AlertTitle>
        {msg("archive_review.failed", "Could not complete this step")}
      </AlertTitle>
      <AlertDescription>
        <p>{detail}</p>
        {retry && (
          <Button variant="outline" type="button" onClick={retry}>
            {msg("actions.retry", "Retry")}
          </Button>
        )}
      </AlertDescription>
    </Alert>
  );
}

export function useMergeLabels() {
  const msg = useMsg();
  return {
    name: (post: Pick<PostMergePost, "latest_capture" | "identifiers">) =>
      post.latest_capture?.title ||
      post.identifiers[0]?.value ||
      msg("source_review.untitled", "Untitled source post"),
    selection: {
      automatic: msg(
        "attachment_selection.automatic",
        "Allow compatible updates",
      ),
      pinned: msg("attachment_selection.pinned", "Keep this order"),
      disabled: msg(
        "attachment_selection.disabled",
        "Disable source selection",
      ),
    },
    state: {
      linked: msg("association_review.linked", "Linked"),
      unlinked: msg("source_review.unlinked", "Explicitly unlinked"),
      undecided: msg("source_review.undecided", "No selected link"),
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
    kind: {
      scene: msg("source_albums.video", "Video"),
      image: msg("source_albums.image", "Image"),
      gallery: msg("gallery", "Gallery"),
    },
  };
}

export function MergeList<T>({
  items,
  rowKey,
  children,
}: {
  items: T[];
  rowKey: (item: T) => string;
  children: (item: T) => ReactNode;
}) {
  const [limit, setLimit] = useState(30);
  const msg = useMsg();
  return (
    <div className="flex flex-col gap-3">
      {items.slice(0, limit).map((item) => (
        <div key={rowKey(item)}>{children(item)}</div>
      ))}
      {limit < items.length && (
        <Button
          type="button"
          variant="outline"
          onClick={() => setLimit((n) => n + 30)}
        >
          {msg("source_posts.load_more", "Load more")}
        </Button>
      )}
    </div>
  );
}

export function MergeEntity({ item }: { item: PostMergeEntity }) {
  const msg = useMsg();
  const labels = useMergeLabels();
  return (
    <div className="flex flex-wrap items-center gap-2">
      <span>
        {labels.kind[item.kind]}
        {item.local_id !== null && ` #${item.local_id}`}
      </span>
      {item.state === "deleted" ? (
        <span>
          {msg(
            "association_review.deleted_item",
            "This library item was deleted.",
          )}
        </span>
      ) : (
        <LibraryLink item={item}>
          {msg("association_review.open_item", "Open library item")}
        </LibraryLink>
      )}
      <code data-selectable-text className="w-full wrap-anywhere text-xs">
        {item.uuid}
      </code>
    </div>
  );
}

export function MergePostLink({
  id,
  children,
}: {
  id: string;
  children: ReactNode;
}) {
  return (
    <Link
      className={buttonVariants({ variant: "outline" })}
      to="/source-posts"
      search={{ mode: "all", value: "", namespace: "", post: id }}
    >
      {children}
    </Link>
  );
}

export const attachmentKey = (item: { namespace: string; value: string }) =>
  JSON.stringify([item.namespace, item.value]);
