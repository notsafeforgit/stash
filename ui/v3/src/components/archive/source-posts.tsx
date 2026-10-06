import { useEffect, useState } from "react";
import { useMsg } from "@/hooks/message";
import { cn } from "@/lib/utils";
import {
  createSourcePostAPI,
  type PostFilter,
  type PostSummary,
} from "@/core/native-archive/source-post-api";
import { useListScrollRestoration } from "@/components/list/use-list-scroll-restoration";
import { AccountService } from "./accounts/shared";
import { CaptureTime } from "@/components/detail/native-sources/history";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Spinner } from "@/components/ui/spinner";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { PostSearch } from "./posts/search";
import { PostDetail } from "./posts/detail";
import { PostEmpty, PostReadError, PostURL } from "./posts/shared";

export function SourcePosts({
  filter,
  selected,
  onFilterChange,
  onSelect,
}: {
  filter: PostFilter;
  selected?: string;
  onFilterChange: (value: PostFilter) => void;
  onSelect: (value?: string) => void;
}) {
  const msg = useMsg();
  const [api] = useState(() => createSourcePostAPI());
  const [cursor, setCursor] = useState("");
  const [previous, setPrevious] = useState<string[]>([]);
  const [page, setPage] = useState<{ key: string; rows: PostSummary[] }>();
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<unknown>();
  const [refresh, setRefresh] = useState(0);
  const [scroller, setScroller] = useState<HTMLDivElement | null>(null);
  const { mode, value, namespace } = filter;
  const queryKey = JSON.stringify([mode, value, namespace, cursor]);
  // biome-ignore lint/correctness/useExhaustiveDependencies: Retry reloads the same bounded page.
  useEffect(() => {
    if (selected) return;
    const controller = new AbortController();
    setBusy(true);
    setError(undefined);
    void api
      .posts({ mode, value, namespace }, cursor, controller.signal)
      .then((rows) => {
        if (!controller.signal.aborted) setPage({ key: queryKey, rows });
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setError(error);
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => controller.abort();
  }, [api, mode, value, namespace, cursor, queryKey, refresh, selected]);
  const current = page?.key === queryKey ? page : undefined;
  useListScrollRestoration(
    "source-posts",
    scroller,
    !selected && !!current && !busy,
    queryKey,
  );
  return (
    <div
      ref={setScroller}
      className="min-h-0 flex-1 overflow-y-auto"
      data-scroll-restoration-id="source-posts"
    >
      <div className="mx-auto flex w-full max-w-4xl flex-col gap-6 p-4 md:p-6">
        <header className="flex flex-col gap-2">
          <h1 className="text-2xl font-semibold">
            {msg("source_posts.title", "Source posts")}
          </h1>
          <p className="text-muted-foreground">
            {msg(
              "source_posts.description",
              "Browse shared post metadata, publisher accounts and associated media.",
            )}
          </p>
        </header>
        {selected && (
          <PostDetail
            key={selected}
            api={api}
            id={selected}
            onBack={() => onSelect()}
          />
        )}
        <div className={cn("flex flex-col gap-6", selected && "hidden")}>
          <PostSearch filter={filter} onChange={onFilterChange} />
          {busy && (
            <Spinner
              aria-label={msg("source_posts.loading", "Loading posts")}
            />
          )}
          {error !== undefined && (
            <PostReadError
              error={error}
              retry={() => setRefresh((n) => n + 1)}
            />
          )}
          {!busy &&
            error === undefined &&
            current &&
            (current.rows.length ? (
              current.rows.map((post) => (
                <Card key={post.uuid}>
                  <CardHeader>
                    <CardTitle data-selectable-text className="wrap-anywhere">
                      {post.latest_capture?.title ||
                        msg("source_review.untitled", "Untitled source post")}
                      {post.latest_capture?.title_truncated ? "…" : ""}
                    </CardTitle>
                    <CardDescription>
                      {post.latest_capture ? (
                        <CaptureTime capture={post.latest_capture} />
                      ) : (
                        msg(
                          "source_review.no_captures",
                          "No retained captures for this post.",
                        )
                      )}
                    </CardDescription>
                  </CardHeader>
                  <CardContent className="flex flex-col gap-3">
                    <div className="flex flex-wrap gap-2">
                      {post.identifiers[0] && (
                        <AccountService
                          namespace={post.identifiers[0].namespace}
                        />
                      )}
                      {post.state === "forgotten" && (
                        <Badge variant="outline">
                          {msg(
                            "source_posts.forgotten",
                            "Forgotten source post",
                          )}
                        </Badge>
                      )}
                    </div>
                    {post.urls[0] && <PostURL value={post.urls[0].url} />}
                  </CardContent>
                  <CardFooter>
                    <Button
                      variant="outline"
                      onClick={() => onSelect(post.uuid)}
                    >
                      {msg("source_posts.open", "Open post")}
                    </Button>
                  </CardFooter>
                </Card>
              ))
            ) : (
              <PostEmpty title={msg("source_posts.empty", "No matching posts")}>
                {msg(
                  "source_posts.empty_help",
                  "Try another exact URL or identifier, or browse all retained posts.",
                )}
              </PostEmpty>
            ))}
          <div className="flex gap-2">
            <Button
              variant="outline"
              disabled={busy || !previous.length}
              onClick={() => {
                setCursor(previous.at(-1) ?? "");
                setPrevious((values) => values.slice(0, -1));
              }}
            >
              {msg("actions.previous", "Previous")}
            </Button>
            <Button
              variant="outline"
              disabled={
                busy ||
                error !== undefined ||
                current?.rows.length !== api.pageLimit
              }
              onClick={() => {
                const last = current?.rows.at(-1);
                if (last) {
                  setPrevious((values) => [...values, cursor]);
                  setCursor(last.uuid);
                }
              }}
            >
              {msg("actions.next", "Next")}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
