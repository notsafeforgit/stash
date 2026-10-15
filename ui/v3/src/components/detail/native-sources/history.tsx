import { useEffect, useState } from "react";
import { useIntl } from "react-intl";
import { ChevronDown } from "lucide-react";
import { useMsg } from "@/hooks/message";
import type {
  SourceDecision,
  PostContent as PostMetadata,
  SourceReviewAPI,
} from "@/core/native-archive/source-review-api";
import { AccountOrigin } from "@/components/archive/accounts/shared";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Spinner } from "@/components/ui/spinner";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import {
  Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
} from "@/components/ui/collapsible";
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

export function PostPublishedDate({
  date,
}: {
  date: string | null | undefined;
}) {
  const msg = useMsg();
  const intl = useIntl();
  return date
    ? intl.formatMessage(
        { id: "source_review.published", defaultMessage: "Published {date}" },
        { date },
      )
    : msg("source_review.published_unknown", "Publication date unknown");
}

export function PostContent({
  api,
  post,
}: {
  api: SourceReviewAPI;
  post: string;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const [metadata, setMetadata] = useState<PostMetadata>(null);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<unknown>();
  const [attempt, setAttempt] = useState(0);
  const title = metadata?.display?.title ?? metadata?.title;
  const description = metadata?.display?.description ?? metadata?.original_text;
  // biome-ignore lint/correctness/useExhaustiveDependencies: Retry reloads this post without paging historical snapshots.
  useEffect(() => {
    const controller = new AbortController();
    setBusy(true);
    setMetadata(null);
    setError(undefined);
    void api
      .postContent(post, controller.signal)
      .then((value) => {
        if (!controller.signal.aborted) setMetadata(value);
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setError(error);
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => controller.abort();
  }, [api, post, attempt]);
  return (
    <div className="flex flex-col gap-3">
      {busy && (
        <Spinner aria-label={msg("source_review.loading", "Loading sources")} />
      )}
      {error !== undefined && (
        <ReviewError
          error={error}
          retry={() => setAttempt((value) => value + 1)}
        />
      )}
      {!busy && error === undefined && metadata === null && (
        <p>
          {msg("source_review.no_content", "No saved content for this post.")}
        </p>
      )}
      {metadata !== null && (
        <Card size="sm">
          {title && (
            <CardHeader>
              <CardTitle className="wrap-anywhere" data-selectable-text>
                {title}
              </CardTitle>
            </CardHeader>
          )}
          <CardContent className="flex flex-col gap-3">
            {description && description !== title && (
              <p
                data-selectable-text
                className="whitespace-pre-wrap wrap-anywhere"
              >
                {description}
              </p>
            )}
            {metadata.display?.translated && (
              <Collapsible>
                <CollapsibleTrigger render={<Button variant="outline" />}>
                  {msg("source_review.original_text", "Original text")}
                  <ChevronDown data-icon="inline-end" />
                </CollapsibleTrigger>
                <CollapsibleContent className="pt-3">
                  <p
                    data-selectable-text
                    className="whitespace-pre-wrap wrap-anywhere"
                  >
                    {metadata.original_text || metadata.title}
                  </p>
                </CollapsibleContent>
              </Collapsible>
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
          </CardContent>
        </Card>
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
