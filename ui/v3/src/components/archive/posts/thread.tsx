import { useCallback, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useMsg } from "@/hooks/message";
import type {
  SourcePostAPI,
  ThreadLink,
} from "@/core/native-archive/source-post-api";
import { Button, buttonVariants } from "@/components/ui/button";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Spinner } from "@/components/ui/spinner";
import { usePostRead } from "./read";
import { PostReadError } from "./shared";

function ThreadPostLink({
  value,
  current,
}: {
  value: ThreadLink;
  current: string;
}) {
  const msg = useMsg();
  const label = value.post?.latest_capture?.title || value.source_id;
  return (
    <div className="flex flex-col items-start gap-1">
      {value.post ? (
        <Link
          className={buttonVariants({
            variant: "link",
            className:
              "h-auto max-w-full whitespace-normal text-start wrap-anywhere",
          })}
          aria-current={value.post.uuid === current ? "page" : undefined}
          to="/source-posts"
          search={{
            post: value.post.uuid,
            mode: "all",
            value: "",
            namespace: "",
          }}
        >
          {label}
        </Link>
      ) : (
        <a
          className={buttonVariants({ variant: "link" })}
          href={value.url}
          target="_blank"
          rel="noreferrer"
        >
          {value.source_id}
        </a>
      )}
      {!value.post && (
        <p className="text-sm text-muted-foreground">
          {msg(
            "source_threads.missing",
            "Not in this archive; opens the source website.",
          )}
        </p>
      )}
    </div>
  );
}

export function PostThread({ id, api }: { id: string; api: SourcePostAPI }) {
  const msg = useMsg();
  const load = useCallback(
    (signal: AbortSignal) => api.thread(id, undefined, signal),
    [api, id],
  );
  const read = usePostRead(load);
  const [extra, setExtra] = useState<ThreadLink[]>([]);
  const [next, setNext] = useState<string>();
  const [loaded, setLoaded] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const thread = read.value;
  const cursor = loaded ? next : thread?.next;
  async function more() {
    if (!cursor) return;
    setBusy(true);
    setError(undefined);
    try {
      const page = await api.thread(id, cursor);
      setExtra((rows) => [...rows, ...page.posts]);
      setNext(page.next);
      setLoaded(true);
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="flex flex-col gap-4">
      {read.busy && (
        <Spinner aria-label={msg("source_threads.loading", "Loading thread")} />
      )}
      {read.error !== undefined && (
        <PostReadError error={read.error} retry={read.retry} />
      )}
      {!read.busy &&
        read.error === undefined &&
        thread &&
        (!thread.facts ? (
          <p className="text-sm text-muted-foreground">
            {msg(
              "source_threads.none",
              "No captured thread relationship. Older imports may not contain reply metadata.",
            )}
          </p>
        ) : (
          <>
            <p className="text-sm text-muted-foreground">
              {msg(
                "source_threads.help",
                "Posts are shown oldest first. Captured self-replies from the same account share an album gallery; each post retains its own attachment order.",
              )}
            </p>
            {thread.conflict && (
              <Alert variant="destructive">
                <AlertDescription>
                  {msg(
                    "source_threads.conflict",
                    "Captured thread relationships disagree. Automatic gallery grouping is paused for this post.",
                  )}
                </AlertDescription>
              </Alert>
            )}
            {thread.root && (
              <div>
                <p>{msg("source_threads.root", "Thread start")}</p>
                <ThreadPostLink
                  value={thread.root}
                  current={thread.post_uuid}
                />
              </div>
            )}
            {thread.parent && (
              <div>
                <p>{msg("source_threads.parent", "In reply to")}</p>
                <ThreadPostLink
                  value={thread.parent}
                  current={thread.post_uuid}
                />
              </div>
            )}
            <ol className="flex flex-col gap-2">
              {[...thread.posts, ...extra].map((post) => (
                <li key={post.source_id}>
                  <ThreadPostLink value={post} current={thread.post_uuid} />
                </li>
              ))}
            </ol>
            {error !== undefined && (
              <PostReadError error={error} retry={() => void more()} />
            )}
            {cursor && (
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => void more()}
              >
                {msg("source_threads.more", "Load more thread posts")}
              </Button>
            )}
          </>
        ))}
    </div>
  );
}
