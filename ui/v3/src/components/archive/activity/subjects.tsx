import { Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import type { ActivitySubject } from "@/core/native-archive/activity-schema";
import { buttonVariants } from "@/components/ui/button";
import { LibraryLink } from "../posts/library-link";

export function ActivitySubjectLink({ subject }: { subject: ActivitySubject }) {
  const msg = useMsg();
  const intl = useIntl();
  const className = buttonVariants({ variant: "outline" });
  if (subject.kind === "post")
    return (
      <Link
        className={className}
        to="/source-posts"
        search={{
          mode: "all",
          value: "",
          namespace: "",
          post: subject.requested_uuid,
        }}
      >
        <span className="whitespace-normal wrap-anywhere">
          {subject.title ||
            msg("source_review.untitled", "Untitled source post")}
          {subject.title_truncated ? "…" : ""}
        </span>
      </Link>
    );
  if (subject.kind === "collection")
    return (
      <div className="flex flex-col gap-1">
        <Link
          className={className}
          to="/collections"
          search={{ q: "", kind: "", state: "", collection: subject.uuid }}
        >
          <span className="whitespace-normal wrap-anywhere">
            {subject.title}
          </span>
        </Link>
        <span className="text-sm text-muted-foreground">
          {intl.formatMessage(
            {
              id: "archive_activity.original_collection",
              defaultMessage: "Collection as scheduled · revision {revision}",
            },
            { revision: subject.revision },
          )}
        </span>
      </div>
    );
  const label =
    subject.kind === "scene"
      ? msg("archive_activity.open_scene", "Open scene")
      : subject.kind === "image"
        ? msg("archive_activity.open_image", "Open image")
        : msg("archive_activity.open_gallery", "Open gallery");
  if (subject.state !== "active" || subject.local_id === null)
    return (
      <p className="text-sm text-muted-foreground">
        {msg(
          "archive_activity.removed_media",
          "The associated library item has been removed.",
        )}
      </p>
    );
  return (
    <LibraryLink
      item={{ kind: subject.kind, state: "active", local_id: subject.local_id }}
    >
      {label}
    </LibraryLink>
  );
}
