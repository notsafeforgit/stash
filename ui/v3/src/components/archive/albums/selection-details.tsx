import { useCallback, useState } from "react";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import type {
  AttachmentSelectionAPI,
  SelectionDecision,
  SelectionList,
} from "@/core/native-archive/attachment-selection-api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Spinner } from "@/components/ui/spinner";
import { PostSection, PostEmpty } from "../posts/shared";
import { ReviewError } from "@/components/detail/native-metadata/shared";
import { useAlbumPages } from "./read";

export function useSelectionLabels() {
  const msg = useMsg();
  return {
    pinned: msg("attachment_selection.pinned", "Keep this order"),
    automatic: msg(
      "attachment_selection.automatic",
      "Allow compatible updates",
    ),
    disabled: msg("attachment_selection.disabled", "Disable source selection"),
  };
}

export function SelectionListDetails({
  value,
  title,
}: {
  value: SelectionList | null;
  title: string;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const labels = useSelectionLabels();
  const [visible, setVisible] = useState(25);
  return (
    <Card size="sm" data-selection-details={title}>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {value ? (
          <>
            <div className="flex flex-wrap gap-2">
              <Badge variant="outline">{labels[value.mode]}</Badge>
              {value.mode !== "disabled" && (
                <Badge variant="secondary">
                  {value.complete
                    ? msg("source_albums.complete", "Complete source list")
                    : msg("source_albums.partial", "Partial source list")}
                </Badge>
              )}
            </div>
            <p className="text-sm">
              {intl.formatMessage(
                {
                  id: "attachment_selection.positions",
                  defaultMessage:
                    "{count, plural, one {# known position} other {# known positions}}",
                },
                { count: value.entries.length },
              )}
              {value.expected_count !== undefined && (
                <>
                  {" "}
                  ·{" "}
                  {intl.formatMessage(
                    {
                      id: "attachment_selection.expected",
                      defaultMessage: "{count, number} expected",
                    },
                    { count: value.expected_count },
                  )}
                </>
              )}
            </p>
            {value.entries.length > 0 && (
              <PostSection
                title={msg(
                  "attachment_selection.inspect_order",
                  "Inspect source positions",
                )}
              >
                <div className="flex flex-col gap-3">
                  {value.entries.slice(0, visible).map((entry) => (
                    <div
                      key={entry.position}
                      className="rounded-md border p-3 text-sm"
                    >
                      <p>
                        {intl.formatMessage(
                          {
                            id: "source_albums.position",
                            defaultMessage: "Position {position}",
                          },
                          { position: entry.position + 1 },
                        )}{" "}
                        ·{" "}
                        {entry.media_kind === "image"
                          ? msg("source_albums.image", "Image")
                          : entry.media_kind === "video"
                            ? msg("source_albums.video", "Video")
                            : msg(
                                "source_albums.attachment",
                                "Source attachment",
                              )}
                      </p>
                      <code
                        className="wrap-anywhere text-xs"
                        data-selectable-text
                      >
                        {entry.reference.namespace}:{entry.reference.value}
                      </code>
                    </div>
                  ))}
                  {visible < value.entries.length && (
                    <Button
                      type="button"
                      variant="outline"
                      onClick={() => setVisible((count) => count + 25)}
                    >
                      {msg("archive_review.load_more", "Load more")}
                    </Button>
                  )}
                  <p className="text-xs text-muted-foreground">
                    {msg(
                      "attachment_selection.positions_help",
                      "Position numbers preserve source order and gaps. Repeated IDs can occupy several positions; these are source references, not download status.",
                    )}
                  </p>
                </div>
              </PostSection>
            )}
          </>
        ) : (
          <p className="text-sm text-muted-foreground">
            {msg(
              "attachment_selection.no_previous",
              "No previous source-list choice.",
            )}
          </p>
        )}
      </CardContent>
    </Card>
  );
}

export function SelectionHistory({
  api,
  post,
}: {
  api: AttachmentSelectionAPI;
  post: string;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const labels = useSelectionLabels();
  const load = useCallback(
    async (after: number | undefined, signal: AbortSignal) => {
      const items = await api.history(post, after, signal);
      return {
        items,
        signature: post,
        header: null,
        next:
          items.length === api.pageLimit
            ? (items.at(-1)?.revision ?? null)
            : null,
      };
    },
    [api, post],
  );
  const state = useAlbumPages(load);
  return (
    <div className="flex flex-col gap-3">
      {state.error !== undefined && (
        <ReviewError error={state.error} retry={state.reload} />
      )}
      {state.busy && <Spinner />}
      {state.data?.items.map((item) => (
        <Card key={item.uuid} size="sm">
          <CardHeader>
            <CardTitle>{labels[item.mode]}</CardTitle>
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
                "attachment_selection.source_references",
                "Source references",
              )}
            >
              <SelectionReferences decision={item} />
            </PostSection>
          </CardContent>
        </Card>
      ))}
      {state.data?.items.length === 0 && (
        <PostEmpty
          title={msg(
            "attachment_selection.no_history",
            "No saved source-list choices",
          )}
        />
      )}
      {state.data?.next !== null && state.data?.next !== undefined && (
        <Button
          type="button"
          variant="outline"
          disabled={state.busy}
          onClick={() => void state.more()}
        >
          {msg("archive_review.load_more", "Load more")}
        </Button>
      )}
    </div>
  );
}

function SelectionReferences({ decision }: { decision: SelectionDecision }) {
  const msg = useMsg();
  const [visible, setVisible] = useState(25);
  return (
    <div className="flex flex-col gap-2 text-xs">
      {decision.capture_uuid && (
        <code className="wrap-anywhere" data-selectable-text>
          {decision.capture_uuid}
        </code>
      )}
      {decision.manifest_uuids.slice(0, visible).map((id) => (
        <code key={id} className="wrap-anywhere" data-selectable-text>
          {id}
        </code>
      ))}
      {visible < decision.manifest_uuids.length && (
        <Button
          type="button"
          variant="outline"
          onClick={() => setVisible((count) => count + 25)}
        >
          {msg("archive_review.load_more", "Load more")}
        </Button>
      )}
    </div>
  );
}
