import { useCallback, useState } from "react";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import type {
  SourcePostAPI,
  PostSummary,
} from "@/core/native-archive/source-post-api";
import {
  createPostConsolidationAPI,
  type PostConsolidationAPI,
} from "@/core/native-archive/post-consolidation-api";
import {
  createPostConsolidationOutbox,
  type SavedPostMerge,
} from "@/core/native-archive/post-consolidation-outbox";
import {
  postMergeApplySchema,
  type PostMergeInput,
  type PostMergePreview,
  type PostMergeReceipt,
} from "@/core/native-archive/post-consolidation-schema";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import { PostSection } from "../shared";
import { usePostRead, PostRows } from "../read";
import { MergeError, MergePostLink } from "./shared";
import { MergeEditor } from "./form";
import { MergeReceipt } from "./summary";
import { MergeNotifications } from "./notifications";

function SavedReview({ api, id }: { api: PostConsolidationAPI; id: string }) {
  const msg = useMsg();
  const load = useCallback(
    (signal: AbortSignal) => api.review(id, signal),
    [api, id],
  );
  const result = usePostRead(load);
  return (
    <>
      {result.busy && (
        <Spinner
          aria-label={msg("post_merge.loading", "Loading post merge review")}
        />
      )}
      {result.error !== undefined && (
        <MergeError error={result.error} retry={result.retry} />
      )}
      {result.value && (
        <>
          <MergeReceipt receipt={result.value} />
          {result.value.result.notification_job_uuid && (
            <MergeNotifications key={id} api={api} review={id} />
          )}
        </>
      )}
    </>
  );
}

function MergeHistory({
  api,
  post,
}: {
  api: PostConsolidationAPI;
  post: string;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const load = useCallback(
    (after?: string, signal?: AbortSignal) =>
      api.history(post, after ? Number(after) : 0, signal),
    [api, post],
  );
  return (
    <div className="flex flex-col gap-3">
      <p>
        {msg(
          "post_merge.history_help",
          "Saved merges involving this original post record. Each receipt retains the choices made at that time, including after a later merge.",
        )}
      </p>
      <PostRows
        load={load}
        rowKey={(record) => String(record.sequence)}
        pageLimit={api.pageLimit}
        empty={msg("post_merge.no_history", "No saved post merges")}
        renderRow={(record) => (
          <Card>
            <CardHeader>
              <CardTitle>
                {intl.formatDate(record.created_at, {
                  dateStyle: "medium",
                  timeStyle: "short",
                })}
              </CardTitle>
              <CardDescription>
                {record.reason ||
                  msg("post_merge.no_reason", "No reason recorded")}
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              <div className="flex flex-wrap gap-2">
                <MergePostLink id={record.source_uuid}>
                  {msg("post_merge.open_original", "Open original post")}
                </MergePostLink>
                <MergePostLink id={record.destination_uuid}>
                  {msg("post_merge.open_result", "Open resulting post")}
                </MergePostLink>
              </div>
              {record.origin === "review" ? (
                <PostSection
                  title={msg(
                    "post_merge.inspect_receipt",
                    "Inspect saved merge",
                  )}
                >
                  <SavedReview api={api} id={record.uuid} />
                </PostSection>
              ) : (
                <p>
                  {msg(
                    "post_merge.imported_merge",
                    "Imported identity history",
                  )}
                </p>
              )}
            </CardContent>
          </Card>
        )}
      />
    </div>
  );
}

export function PostMerge({
  api: posts,
  requested,
  post,
  onSaved,
}: {
  api: SourcePostAPI;
  requested: string;
  post: PostSummary;
  onSaved: () => void;
}) {
  const msg = useMsg();
  const [api] = useState(() => createPostConsolidationAPI(posts.endpoint));
  const [box] = useState(() => createPostConsolidationOutbox(api));
  const [saved, setSaved] = useState<SavedPostMerge | null>(null);
  const [receipt, setReceipt] = useState<PostMergeReceipt>();
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState(false);
  const [historyVersion, setHistoryVersion] = useState(0);
  const read = useCallback(() => box.read(requested), [box, requested]);
  const recovery = usePostRead(read);
  const pending = saved ?? recovery.value;
  async function deliver(input?: PostMergeInput, preview?: PostMergePreview) {
    if (busy) return;
    setBusy(true);
    setError(undefined);
    try {
      if (input && preview)
        setSaved(await box.prepare(requested, input, preview));
      const result = await box.deliver(requested);
      setReceipt(result);
      setSaved(null);
      recovery.retry();
      setHistoryVersion((n) => n + 1);
      onSaved();
    } catch (error) {
      setError(error);
      // A failed transport leaves the original request in durable local storage.
      recovery.retry();
      try {
        setSaved(await box.read(requested));
      } catch {
        /* The visible recovery error keeps editing locked. */
      }
    } finally {
      setBusy(false);
    }
  }
  async function discardRejected() {
    if (pending?.state !== "rejected" || busy) return;
    setBusy(true);
    setError(undefined);
    try {
      await box.forgetRejected(
        requested,
        postMergeApplySchema.parse(JSON.parse(pending.body)).request_uuid,
      );
      setSaved(null);
      recovery.retry();
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="flex flex-col gap-4">
      {recovery.busy && (
        <Spinner
          aria-label={msg("post_merge.loading", "Loading post merge review")}
        />
      )}
      {recovery.error !== undefined && (
        <MergeError error={recovery.error} retry={recovery.retry} />
      )}
      {error !== undefined && <MergeError error={error} />}
      {receipt && (
        <>
          <MergeReceipt receipt={receipt} />
          {receipt.result.notification_job_uuid && (
            <MergeNotifications
              key={receipt.request.request_uuid}
              api={api}
              review={receipt.request.request_uuid}
            />
          )}
        </>
      )}
      {!receipt && pending && (
        <Alert>
          <AlertTitle>
            {pending.state === "rejected"
              ? msg("post_merge.rejected", "Review this merge again")
              : msg(
                  "post_merge.pending",
                  "A saved post merge needs confirmation",
                )}
          </AlertTitle>
          <AlertDescription>
            <p>
              {pending.state === "rejected"
                ? msg(
                    "post_merge.rejected_help",
                    "This saved request was rejected because its preview changed. Reload the current choices and make a new preview.",
                  )
                : msg(
                    "post_merge.pending_help",
                    "Recover the original result before making another choice. Recovery checks whether the merge was saved, then retries the same request only if necessary.",
                  )}
            </p>
            <Button
              variant="outline"
              disabled={busy || recovery.busy || recovery.error !== undefined}
              onClick={() =>
                pending.state === "rejected"
                  ? void discardRejected()
                  : void deliver()
              }
            >
              {pending.state === "rejected"
                ? msg("association_review.review_again", "Review again")
                : msg("post_merge.recover", "Check and retry saved merge")}
            </Button>
          </AlertDescription>
        </Alert>
      )}
      {!receipt &&
        !pending &&
        !recovery.busy &&
        recovery.error === undefined &&
        (post.uuid !== requested ? (
          <Alert>
            <AlertTitle>
              {msg(
                "post_merge.redirected",
                "This post has already been merged",
              )}
            </AlertTitle>
            <AlertDescription>
              <MergePostLink id={post.uuid}>
                {msg("post_merge.open_result", "Open resulting post")}
              </MergePostLink>
            </AlertDescription>
          </Alert>
        ) : post.state === "active" ? (
          <MergeEditor
            api={api}
            posts={posts}
            source={requested}
            onApply={deliver}
          />
        ) : (
          <p>{msg("source_posts.forgotten", "Forgotten source post")}</p>
        ))}
      <PostSection title={msg("post_merge.history", "Post merge history")}>
        <MergeHistory key={historyVersion} api={api} post={requested} />
      </PostSection>
    </div>
  );
}
