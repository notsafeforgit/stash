import { useCallback, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useMsg } from "@/hooks/message";
import {
  createReviewQueueAPI,
  reviewQueueSearchSchema,
  type ReviewQueueItem,
  type ReviewQueueSearch,
} from "@/core/native-archive/review-queue-api";
import { useListScrollRestoration } from "@/components/list/use-list-scroll-restoration";
import { Button, buttonVariants } from "@/components/ui/button";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { Spinner } from "@/components/ui/spinner";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ReviewError } from "@/components/detail/native-metadata/shared";
import { AccountName, AccountService } from "./accounts/shared";
import { useActivityRead } from "./activity/shared";
import { PostEmpty, PostSection } from "./posts/shared";

function ReviewLink({ item }: { item: ReviewQueueItem }) {
  const msg = useMsg();
  const className = buttonVariants({ variant: "outline" });
  if (item.account)
    return (
      <Link
        className={className}
        to="/account-review"
        search={{ account: item.uuid, ownership: "all", q: "", namespace: "" }}
      >
        {msg("review_queue.review_account", "Review account owner")}
      </Link>
    );
  if (item.post)
    return (
      <Link
        className={className}
        to="/source-posts"
        search={{ post: item.uuid, mode: "all", value: "", namespace: "" }}
      >
        {msg("review_queue.review_post", "Review post media")}
      </Link>
    );
  if (item.media?.local_id) {
    const label = msg(
      "review_queue.review_metadata",
      "Review retained metadata",
    );
    return item.media.kind === "scene" ? (
      <Link
        className={className}
        to="/scenes/$sceneId"
        params={{ sceneId: String(item.media.local_id) }}
        search={{ tab: "metadata-review" }}
      >
        {label}
      </Link>
    ) : (
      <Link
        className={className}
        to="/images/$imageId"
        params={{ imageId: String(item.media.local_id) }}
        search={{ tab: "metadata-review" }}
      >
        {label}
      </Link>
    );
  }
  return null;
}

function ReviewCard({ item }: { item: ReviewQueueItem }) {
  const msg = useMsg();
  const reasons = {
    account_owner_undecided: msg(
      "review_queue.owner_undecided",
      "Account owner has not been reviewed",
    ),
    media_unselected: msg(
      "review_queue.media_unselected",
      "Retained media evidence needs an association decision",
    ),
    post_link_conflict: msg(
      "review_queue.post_conflict",
      "Merged identities have conflicting media associations",
    ),
    attachment_link_undecided: msg(
      "review_queue.attachment_undecided",
      "An attachment association needs review",
    ),
    attachment_link_conflict: msg(
      "review_queue.attachment_conflict",
      "Attachment associations conflict",
    ),
    review_limit: msg(
      "review_queue.limit",
      "This post has too many associations to review together; inspect individual media sources",
    ),
    retained_metadata: msg(
      "review_queue.retained_metadata",
      "Retained catalog edits have not been reviewed",
    ),
  };
  const title = item.account ? (
    <AccountName account={item.account} />
  ) : item.post ? (
    item.post.latest_capture?.title ||
    msg("review_queue.untitled_post", "Untitled post")
  ) : (
    item.media?.title ||
    (item.media?.kind === "scene"
      ? msg("review_queue.untitled_scene", "Untitled scene")
      : msg("review_queue.untitled_image", "Untitled image"))
  );
  return (
    <Card>
      <CardHeader>
        <CardTitle className="wrap-anywhere">{title}</CardTitle>
        <CardDescription>
          {item.account ? (
            <AccountService namespace={item.account.namespace} />
          ) : item.post ? (
            msg("review_queue.post", "Source post")
          ) : item.media?.kind === "scene" ? (
            msg("review_queue.scene", "Scene")
          ) : (
            msg("review_queue.image", "Image")
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <ul className="flex list-disc flex-col gap-1 pl-5 text-sm">
          {item.reasons.map((reason) => (
            <li key={reason}>{reasons[reason]}</li>
          ))}
        </ul>
        <PostSection
          title={msg("review_queue.references", "Technical references")}
        >
          <dl className="flex flex-col gap-1 text-sm">
            <dt>{msg("review_queue.archive_id", "Archive ID")}</dt>
            <dd data-selectable-text className="wrap-anywhere">
              {item.uuid}
            </dd>
          </dl>
        </PostSection>
      </CardContent>
      <CardFooter>
        <ReviewLink item={item} />
      </CardFooter>
    </Card>
  );
}

export function ReviewQueue({
  search,
  onChange,
}: {
  search: ReviewQueueSearch;
  onChange: (value: ReviewQueueSearch) => void;
}) {
  const msg = useMsg();
  const [api] = useState(() => createReviewQueueAPI());
  const [refresh, setRefresh] = useState(0);
  const [scroller, setScroller] = useState<HTMLDivElement | null>(null);
  const { kind } = search;
  const after = search.after ?? "";
  const [cursors, setCursors] = useState([after]);
  if (cursors.at(-1) !== after) {
    const position = cursors.indexOf(after);
    setCursors(
      position < 0 ? [...cursors, after] : cursors.slice(0, position + 1),
    );
  }
  const key = JSON.stringify([kind, after]);
  const load = useCallback(
    (signal: AbortSignal) => api.page(kind, after, signal),
    [api, kind, after],
  );
  const read = useActivityRead(key, load, refresh);
  useListScrollRestoration(
    "review-queue",
    scroller,
    !!read.current && !read.busy,
    key,
  );
  const descriptions = {
    accounts: msg(
      "review_queue.accounts_help",
      "Choose an account owner, or explicitly leave an aggregator account without one. Account ownership does not assign depicted performers.",
    ),
    media: msg(
      "review_queue.media_help",
      "Review which scenes and images belong to source posts. Conflicting associations remain here until reviewed.",
    ),
    metadata: msg(
      "review_queue.metadata_help",
      "Review retained catalog edits by applying a choice or keeping the current field value. Import does not overwrite your library metadata automatically.",
    ),
  };
  return (
    <div
      ref={setScroller}
      className="min-h-0 flex-1 overflow-y-auto"
      data-scroll-restoration-id="review-queue"
    >
      <div className="mx-auto flex w-full max-w-4xl flex-col gap-6 p-4 md:p-6">
        <header className="flex flex-col gap-2">
          <h1 className="text-2xl font-semibold">
            {msg("review_queue.title", "Review queue")}
          </h1>
          <p className="text-muted-foreground">
            {msg(
              "review_queue.description",
              "Review current account, media and metadata decisions in the archive.",
            )}
          </p>
        </header>
        <ToggleGroup
          aria-label={msg("review_queue.kind", "Review type")}
          variant="outline"
          value={[kind]}
          onValueChange={(values) => {
            if (values[0])
              onChange(reviewQueueSearchSchema.parse({ kind: values[0] }));
          }}
        >
          <ToggleGroupItem value="accounts">
            {msg("review_queue.accounts", "Accounts")}
          </ToggleGroupItem>
          <ToggleGroupItem value="media">
            {msg("review_queue.media", "Media")}
          </ToggleGroupItem>
          <ToggleGroupItem value="metadata">
            {msg("review_queue.metadata", "Metadata")}
          </ToggleGroupItem>
        </ToggleGroup>
        <p className="text-sm text-muted-foreground">{descriptions[kind]}</p>
        <div>
          <Button
            variant="outline"
            disabled={read.busy}
            onClick={() => setRefresh((n) => n + 1)}
          >
            {msg("actions.refresh", "Refresh")}
          </Button>
        </div>
        {read.busy && (
          <Spinner
            aria-label={msg("review_queue.loading", "Loading review queue")}
          />
        )}
        {read.error !== undefined && (
          <ReviewError
            error={read.error}
            retry={() => setRefresh((n) => n + 1)}
          />
        )}
        {read.current?.items.map((item) => (
          <ReviewCard key={item.uuid} item={item} />
        ))}
        {read.current?.items.length === 0 && (
          <PostEmpty
            title={
              read.current.next
                ? msg("review_queue.more_to_check", "More posts to check")
                : msg("review_queue.empty", "Nothing to review on this page")
            }
          >
            {read.current.next
              ? msg(
                  "review_queue.more_help",
                  "These posts have no unresolved associations. Continue checking the next posts.",
                )
              : undefined}
          </PostEmpty>
        )}
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            disabled={read.busy || !after}
            onClick={() => {
              onChange({ kind, after: cursors.at(-2) || undefined });
            }}
          >
            {cursors.length > 1
              ? msg("review_queue.previous", "Previous page")
              : msg("review_queue.first", "First page")}
          </Button>
          <Button
            variant="outline"
            disabled={read.busy || !read.current?.next}
            onClick={() => {
              if (read.current?.next) {
                onChange({ kind, after: read.current.next });
              }
            }}
          >
            {read.current?.items.length === 0 && read.current.next
              ? msg("review_queue.continue", "Continue checking")
              : msg("review_queue.next", "Next page")}
          </Button>
        </div>
      </div>
    </div>
  );
}
