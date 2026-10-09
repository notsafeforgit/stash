import { useEffect, useState } from "react";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import type {
  SourceCapture,
  SourceDecision,
  CaptureHistory,
  SourceReviewAPI,
} from "@/core/native-archive/source-review-api";
import { AccountOrigin } from "@/components/archive/accounts/shared";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Spinner } from "@/components/ui/spinner";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { ReviewError } from "../native-metadata/shared";

export function LinkState({
  state,
  attachments = 0,
}: {
  state: string;
  attachments?: number;
}) {
  const msg = useMsg();
  switch (state) {
    case "linked":
      return msg("source_review.linked", "Linked to this item");
    case "unlinked":
      return msg("source_review.unlinked", "Explicitly unlinked");
    case "conflict":
      return msg("source_review.conflict", "Conflicting link choices");
    default:
      return attachments > 0
        ? msg("source_review.attachment_linked", "Linked through attachments")
        : msg("source_review.undecided", "No selected link");
  }
}

export function CaptureTime({
  capture,
}: {
  capture: Pick<SourceCapture, "captured_at" | "recorded_at">;
}) {
  const intl = useIntl();
  return capture.captured_at
    ? intl.formatMessage(
        { id: "source_review.observed", defaultMessage: "Observed {date}" },
        {
          date: intl.formatDate(capture.captured_at, {
            dateStyle: "medium",
            timeStyle: "short",
          }),
        },
      )
    : intl.formatMessage(
        {
          id: "source_review.recorded",
          defaultMessage: "Observation time unknown · stored {date}",
        },
        {
          date: intl.formatDate(capture.recorded_at ?? "", {
            dateStyle: "medium",
            timeStyle: "short",
          }),
        },
      );
}

export function SourceCaptures({
  api,
  post,
}: {
  api: SourceReviewAPI;
  post: string;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const [revisions, setRevisions] = useState<CaptureHistory[]>([]);
  const [more, setMore] = useState(false);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<unknown>();
  useEffect(() => {
    const controller = new AbortController();
    void api
      .captureHistory(post, undefined, controller.signal)
      .then((page) => {
        if (controller.signal.aborted) return;
        setRevisions(page);
        setMore(page.length === api.pageLimit);
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setError(error);
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => controller.abort();
  }, [api, post]);
  async function loadMore() {
    setBusy(true);
    setError(undefined);
    try {
      const page = await api.captureHistory(post, revisions.at(-1)?.uuid);
      setRevisions((prior) => [
        ...prior,
        ...page.filter((r) => !prior.some((p) => p.uuid === r.uuid)),
      ]);
      setMore(page.length === api.pageLimit);
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="flex flex-col gap-3">
      <p className="text-sm text-muted-foreground">
        {msg(
          "source_review.captures_help",
          "Each version stores the post content once. Repeat observations share that content; counts can include separate media from the same scrape.",
        )}
      </p>
      {busy && (
        <Spinner aria-label={msg("source_review.loading", "Loading sources")} />
      )}
      {error !== undefined && (
        <ReviewError error={error} retry={() => void loadMore()} />
      )}
      {!busy && !error && revisions.length === 0 && (
        <p>
          {msg(
            "source_review.no_captures",
            "No retained captures for this post.",
          )}
        </p>
      )}
      {revisions.map((revision) => {
        const metadata = revision.metadata;
        return (
          <Card key={revision.uuid} size="sm">
            <CardHeader>
              <CardTitle className="wrap-anywhere">
                {metadata.title ||
                  msg("source_review.untitled", "Untitled source post")}
              </CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              {metadata.original_text &&
                metadata.original_text !== metadata.title && (
                  <p
                    data-selectable-text
                    className="whitespace-pre-wrap wrap-anywhere"
                  >
                    {metadata.original_text}
                  </p>
                )}
              {metadata.published_at && (
                <p className="text-sm" data-selectable-text>
                  {intl.formatMessage(
                    {
                      id: "source_review.source_date",
                      defaultMessage: "Source date: {date}",
                    },
                    { date: metadata.published_at },
                  )}
                </p>
              )}
              {metadata.date_basis === "legacy-nfo-unverified" && (
                <Badge variant="outline">
                  {msg(
                    "source_review.unverified_date",
                    "Unverified historical date",
                  )}
                </Badge>
              )}
              <p className="text-sm text-muted-foreground">
                {intl.formatMessage(
                  {
                    id: "source_review.sighting_count",
                    defaultMessage:
                      "{count, plural, one {# observation} other {# observations}}",
                  },
                  { count: revision.count },
                )}
              </p>
              {revision.first_seen && revision.last_seen && (
                <p className="text-sm" data-selectable-text>
                  {intl.formatMessage(
                    {
                      id: "source_review.sighting_range",
                      defaultMessage: "First seen {first} · Last seen {last}",
                    },
                    {
                      first: intl.formatDate(revision.first_seen, {
                        dateStyle: "medium",
                        timeStyle: "short",
                      }),
                      last: intl.formatDate(revision.last_seen, {
                        dateStyle: "medium",
                        timeStyle: "short",
                      }),
                    },
                  )}
                </p>
              )}
              {revision.unknown_count > 0 && (
                <p className="text-sm text-muted-foreground">
                  {intl.formatMessage(
                    {
                      id: "source_review.unknown_sighting_count",
                      defaultMessage:
                        "Observation time unknown for {count, plural, one {# imported record} other {# imported records}}.",
                    },
                    { count: revision.unknown_count },
                  )}
                </p>
              )}
            </CardContent>
          </Card>
        );
      })}
      {more && (
        <Button
          variant="outline"
          disabled={busy}
          onClick={() => void loadMore()}
        >
          {msg("source_review.more_versions", "Load more versions")}
        </Button>
      )}
    </div>
  );
}

export function SourceLinkHistory({
  api,
  post,
  media,
}: {
  api: SourceReviewAPI;
  post: string;
  media: string;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const [rows, setRows] = useState<SourceDecision[]>([]);
  const [busy, setBusy] = useState(true);
  const [more, setMore] = useState(false);
  const [error, setError] = useState<unknown>();
  useEffect(() => {
    const controller = new AbortController();
    void api
      .history(post, media, 0, controller.signal)
      .then((page) => {
        if (controller.signal.aborted) return;
        setRows(page);
        setMore(page.length === api.pageLimit);
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setError(error);
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => controller.abort();
  }, [api, post, media]);
  async function loadMore() {
    setBusy(true);
    setError(undefined);
    try {
      const page = await api.history(post, media, rows.at(-1)?.post_revision);
      setRows((prior) => [...prior, ...page]);
      setMore(page.length === api.pageLimit);
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="flex flex-col gap-3">
      {busy && (
        <Spinner aria-label={msg("source_review.loading", "Loading sources")} />
      )}
      {error !== undefined && (
        <ReviewError error={error} retry={() => void loadMore()} />
      )}
      {!busy && !error && rows.length === 0 && (
        <p>
          {msg("source_review.no_history", "No explicit post link choices.")}
        </p>
      )}
      <ol className="flex flex-col gap-3">
        {rows.map((row) => (
          <li key={row.uuid} className="flex flex-col gap-1 text-sm">
            <span>
              {row.state === "undecided" ? (
                msg("source_review.attachments", "Use attachments")
              ) : (
                <LinkState state={row.state} />
              )}
            </span>
            <span className="text-muted-foreground">
              {intl.formatDate(row.created_at, {
                dateStyle: "medium",
                timeStyle: "short",
              })}{" "}
              · <AccountOrigin origin={row.origin} />
            </span>
            {row.reason && (
              <p
                className="whitespace-pre-wrap wrap-anywhere"
                data-selectable-text
              >
                {row.reason}
              </p>
            )}
          </li>
        ))}
      </ol>
      {more && (
        <Button
          variant="outline"
          disabled={busy}
          onClick={() => void loadMore()}
        >
          {msg("source_review.more_history", "Load more link history")}
        </Button>
      )}
    </div>
  );
}
