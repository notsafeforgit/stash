import { Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import type { ActivitySubject } from "@/core/native-archive/activity-schema";
import { Button } from "@/components/ui/button";
import { LibraryLink } from "../posts/library-link";

export function ActivitySubjectLink({ subject }: { subject: ActivitySubject }) {
  const msg = useMsg();
  const intl = useIntl();
  if (subject.kind === "post")
    return (
      <Button
        variant="outline"
        render={
          <Link
            to="/source-posts"
            search={{
              mode: "all",
              value: "",
              namespace: "",
              post: subject.requested_uuid,
            }}
          />
        }
      >
        <span className="whitespace-normal wrap-anywhere">
          {subject.title ||
            msg("source_review.untitled", "Untitled source post")}
          {subject.title_truncated ? "…" : ""}
        </span>
      </Button>
    );
  if (subject.kind === "collection")
    return (
      <div className="flex flex-col gap-1">
        <Button
          variant="outline"
          render={
            <Link
              to="/collections"
              search={{ q: "", kind: "", state: "", collection: subject.uuid }}
            />
          }
        >
          <span className="whitespace-normal wrap-anywhere">
            {subject.title}
          </span>
        </Button>
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
  if (subject.state !== "active")
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
      item={{ kind: subject.kind, state: "active", uuid: subject.uuid }}
    >
      {label}
    </LibraryLink>
  );
}
