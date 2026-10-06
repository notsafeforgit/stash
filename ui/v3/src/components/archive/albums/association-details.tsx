import { useCallback } from "react";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import type {
  GalleryAssociationAPI,
  AttachmentMediaAPI,
} from "@/core/native-archive/association-review-api";
import type { PostLibraryItem } from "@/core/native-archive/source-post-api";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { ReviewError } from "@/components/detail/native-metadata/shared";
import { LibraryLink } from "../posts/library-link";
import { PostEmpty, PostSection } from "../posts/shared";
import { useAlbumPages } from "./read";

export function useAssociationLabels() {
  const msg = useMsg();
  return {
    linked: msg("association_review.linked", "Linked"),
    disabled: msg(
      "association_review.disabled",
      "Gallery association disabled",
    ),
    unlinked: msg("association_review.unlinked", "Attachment link rejected"),
    undecided: msg("association_review.undecided", "Automatic linking allowed"),
  };
}
export function AssociationChoice({
  title,
  state,
  item,
}: {
  title: string;
  state: "linked" | "disabled" | "unlinked" | "undecided" | undefined;
  item: PostLibraryItem | null;
}) {
  const msg = useMsg(),
    labels = useAssociationLabels();
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-2 text-sm">
        <p>
          {state
            ? labels[state]
            : msg(
                "association_review.no_choice",
                "No saved association choice",
              )}
        </p>
        {item && (
          <>
            <p className="wrap-anywhere" data-selectable-text>
              {item.title || msg("metadata_policy.untitled", "Untitled")}
              {item.title_truncated ? "…" : ""}
              {item.local_id !== null ? ` (#${item.local_id})` : ""}
            </p>
            {item.state === "active" ? (
              <LibraryLink item={item}>
                {msg("association_review.open_item", "Open library item")}
              </LibraryLink>
            ) : (
              <p>
                {msg(
                  "association_review.deleted_item",
                  "This library item was deleted.",
                )}
              </p>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}
type HistoryProps =
  | { family: "gallery"; api: GalleryAssociationAPI; scope: string }
  | { family: "attachment"; api: AttachmentMediaAPI; scope: string };
type HistoryRow = {
  uuid: string;
  revision: number;
  state: "linked" | "disabled" | "unlinked" | "undecided";
  target: string | null;
  reason: string;
  created_at: string;
};
export function AssociationHistory(props: HistoryProps) {
  const msg = useMsg(),
    intl = useIntl(),
    labels = useAssociationLabels();
  const { family, api, scope } = props;
  const load = useCallback(
    async (after: number | undefined, signal: AbortSignal) => {
      const rows: HistoryRow[] =
        family === "gallery"
          ? (await api.history(scope, after, signal)).map((item) => ({
              uuid: item.decision_uuid,
              revision: item.revision,
              state: item.state,
              target: item.gallery_uuid,
              reason: item.reason,
              created_at: item.created_at,
            }))
          : (await api.history(scope, after, signal)).map((item) => ({
              uuid: item.uuid,
              revision: item.revision,
              state: item.state,
              target: item.media_uuid,
              reason: item.reason,
              created_at: item.created_at,
            }));
      return {
        items: rows,
        signature: scope,
        header: null,
        next:
          rows.length === api.pageLimit
            ? (rows.at(-1)?.revision ?? null)
            : null,
      };
    },
    [family, api, scope],
  );
  const result = useAlbumPages(load);
  return (
    <div className="flex flex-col gap-3">
      {result.error !== undefined && (
        <ReviewError error={result.error} retry={result.reload} />
      )}
      {result.busy && <Spinner />}
      {result.data?.items.map((item) => (
        <Card key={item.uuid} size="sm">
          <CardHeader>
            <CardTitle>{labels[item.state]}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-2 text-sm">
            <p>
              {intl.formatDate(item.created_at)} ·{" "}
              {intl.formatTime(item.created_at)}
            </p>
            {item.reason && (
              <p className="wrap-anywhere" data-selectable-text>
                {item.reason}
              </p>
            )}
            <PostSection
              title={msg(
                "association_review.identifiers",
                "Decision identifiers",
              )}
            >
              <div className="flex flex-col gap-2 text-xs">
                <code className="wrap-anywhere" data-selectable-text>
                  {item.uuid}
                </code>
                {item.target && (
                  <code className="wrap-anywhere" data-selectable-text>
                    {item.target}
                  </code>
                )}
              </div>
            </PostSection>
          </CardContent>
        </Card>
      ))}
      {result.data?.items.length === 0 && (
        <PostEmpty
          title={msg(
            "association_review.no_history",
            "No saved association choices",
          )}
        />
      )}
      {result.data?.next !== undefined && result.data.next !== null && (
        <Button
          type="button"
          variant="outline"
          disabled={result.busy}
          onClick={() => void result.more()}
        >
          {msg("archive_review.load_more", "Load more")}
        </Button>
      )}
    </div>
  );
}
