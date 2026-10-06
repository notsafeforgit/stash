import { useEffect, useState } from "react";
import { useApolloClient } from "@apollo/client/react";
import { History } from "lucide-react";
import { useMsg } from "@/hooks/message";
import * as GQL from "@/core/generated-graphql";
import { affectedActiveQueries } from "@/core/mutation-invalidation";
import {
  createSourceReviewAPI,
  sourceLinkInputSchema,
  type SourceAssociation,
  type SourceLinkState,
  type SourcePost,
} from "@/core/native-archive/source-review-api";
import {
  createSourceReviewOutbox,
  type SavedSourceReview,
  type SourceReviewTarget,
} from "@/core/native-archive/source-review-outbox";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  Empty,
  EmptyHeader,
  EmptyTitle,
  EmptyDescription,
} from "@/components/ui/empty";
import { ReviewError } from "./native-metadata/shared";
import { SourcePostCard } from "./native-sources/post-card";

/** Key by kind/local ID to isolate navigation. Only an explicit save or recovery
 * writes; opening a Sources tab never applies retained evidence. */
export function NativeSourceReview({ kind, localId }: SourceReviewTarget) {
  const msg = useMsg();
  const client = useApolloClient();
  const [api] = useState(() => createSourceReviewAPI());
  const [outbox] = useState(() => createSourceReviewOutbox(api));
  const [media, setMedia] = useState<string>();
  const [posts, setPosts] = useState<SourcePost[]>([]);
  const [saved, setSaved] = useState<SavedSourceReview | null>(null);
  const [busy, setBusy] = useState(true);
  const [loaded, setLoaded] = useState(false);
  const [more, setMore] = useState(false);
  const [error, setError] = useState<unknown>();
  const [applied, setApplied] = useState(false);
  const [refreshFailed, setRefreshFailed] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const target = { kind, localId };
  // biome-ignore lint/correctness/useExhaustiveDependencies: Explicit retry reloads the list; successful mutations refresh just the affected card.
  useEffect(() => {
    const controller = new AbortController();
    async function load() {
      setBusy(true);
      setError(undefined);
      try {
        const pending = await outbox.read({ kind, localId });
        if (!controller.signal.aborted) setSaved(pending);
        const identity = await api.identity(kind, localId, controller.signal);
        const rows = await api.posts(
          identity.uuid,
          undefined,
          controller.signal,
        );
        if (!controller.signal.aborted) {
          setMedia(identity.uuid);
          setPosts(rows);
          setMore(rows.length === api.pageLimit);
          setLoaded(true);
        }
      } catch (error) {
        if (!controller.signal.aborted) setError(error);
      } finally {
        if (!controller.signal.aborted) setBusy(false);
      }
    }
    void load();
    return () => controller.abort();
  }, [api, outbox, kind, localId, refresh]);

  async function reconcile(post: string, entity: string) {
    setRefreshFailed(false);
    try {
      const current = await api.review(post, entity);
      setPosts((prior) =>
        prior.map((row) =>
          row.association.post_uuid === post ? current : row,
        ),
      );
      await client.refetchQueries({
        include: affectedActiveQueries(
          client,
          kind === "scene" ? GQL.SceneUpdateDocument : GQL.ImageUpdateDocument,
        ),
      });
    } catch {
      setRefreshFailed(true);
    }
  }
  async function apply(
    association?: SourceAssociation,
    state?: SourceLinkState,
    reason = "",
  ) {
    setBusy(true);
    setError(undefined);
    setApplied(false);
    try {
      if (association && state)
        setSaved(await outbox.prepare(target, association, state, reason));
      const result = await outbox.deliver(target);
      setApplied(true);
      await reconcile(result.post_uuid, result.media_uuid);
    } finally {
      try {
        setSaved(await outbox.read(target));
      } finally {
        setBusy(false);
      }
    }
  }
  async function recover() {
    try {
      if (saved?.state === "rejected") {
        await outbox.forgetRejected(
          target,
          sourceLinkInputSchema.parse(JSON.parse(saved.body)).uuid,
        );
        setSaved(await outbox.read(target));
        setRefresh((value) => value + 1);
      } else await apply();
    } catch (error) {
      setError(error);
    }
  }
  async function loadMore() {
    if (!media) return;
    setBusy(true);
    setError(undefined);
    try {
      const rows = await api.posts(media, posts.at(-1)?.association.post_uuid);
      setPosts((prior) => [...prior, ...rows]);
      setMore(rows.length === api.pageLimit);
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section
      aria-label={msg("source_review.title", "Sources")}
      className="flex flex-col gap-4"
    >
      <p className="text-sm text-muted-foreground">
        {msg(
          "source_review.intro",
          "Inspect posts associated with this item and review their links. Source publishers and depicted performers are separate choices.",
        )}
      </p>
      {busy && (
        <Spinner aria-label={msg("source_review.loading", "Loading sources")} />
      )}
      {error !== undefined && (
        <ReviewError
          error={error}
          retry={() => setRefresh((value) => value + 1)}
        />
      )}
      {applied && (
        <Alert>
          <AlertTitle>
            {msg("source_review.saved", "Source link saved")}
          </AlertTitle>
          <AlertDescription>
            {msg(
              "source_review.saved_help",
              "The original evidence and earlier link choices remain in the archive.",
            )}
          </AlertDescription>
        </Alert>
      )}
      {refreshFailed && (
        <Alert>
          <AlertTitle>
            {msg(
              "source_review.refresh_failed",
              "Saved, but the view needs refreshing",
            )}
          </AlertTitle>
          <AlertDescription>
            <Button
              variant="outline"
              onClick={() => {
                setRefreshFailed(false);
                setRefresh((value) => value + 1);
              }}
            >
              {msg("actions.retry", "Retry")}
            </Button>
          </AlertDescription>
        </Alert>
      )}
      {saved && (
        <Alert>
          <History />
          <AlertTitle>
            {saved.state === "rejected"
              ? msg(
                  "source_review.rejected",
                  "The saved link needs a new review",
                )
              : msg(
                  "source_review.pending",
                  "A saved source link needs confirmation",
                )}
          </AlertTitle>
          <AlertDescription>
            <p>
              {msg(
                "source_review.pending_help",
                "Recover this browser's saved request before making another link choice. Recovery checks for the original result first.",
              )}
            </p>
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => void recover()}
            >
              {saved.state === "rejected"
                ? msg("archive_review.fresh_preview", "Review again")
                : msg("archive_review.recover", "Check and retry saved change")}
            </Button>
          </AlertDescription>
        </Alert>
      )}
      {loaded && posts.length === 0 && (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>
              {msg("source_review.empty", "No source posts for this item")}
            </EmptyTitle>
            <EmptyDescription>
              {msg(
                "source_review.empty_help",
                "Directly scanned files can exist without a scraped post. Source evidence and reviewed links appear here when available.",
              )}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}
      {posts.map((post) => (
        <SourcePostCard
          key={`${post.association.post_uuid}:${post.association.post_revision}`}
          post={post}
          api={api}
          blocked={busy || !!saved || refreshFailed}
          apply={apply}
        />
      ))}
      {more && (
        <Button
          variant="outline"
          disabled={busy}
          onClick={() => void loadMore()}
        >
          {msg("source_review.more_posts", "Load more source posts")}
        </Button>
      )}
    </section>
  );
}
