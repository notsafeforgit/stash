import { SourceAlbum } from "../albums/ordered";
import { PostThread } from "./thread";
import { PostMerge } from "./merge";
import { LibraryLink } from "./library-link";
import { useCallback, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useMsg } from "@/hooks/message";
import type {
  SourcePostAPI,
  PostSummary,
  PostMedia,
} from "@/core/native-archive/source-post-api";
import type { Account } from "@/core/native-archive/account-review-api";
import { AccountName, AccountOwner, AccountService } from "../accounts/shared";
import {
  CaptureTime,
  LinkState,
  SourceCaptures,
} from "@/components/detail/native-sources/history";
import { Button, buttonVariants } from "@/components/ui/button";
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
import { PostSection, PostReadError, PostEmpty, PostURL } from "./shared";
import { PostRows, usePostRead } from "./read";

function PostIdentifiers({
  post,
  api,
}: {
  post: PostSummary;
  api: SourcePostAPI;
}) {
  const msg = useMsg();
  const [identifiers, setIdentifiers] = useState(post.identifiers);
  const [more, setMore] = useState(post.more_identifiers);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  async function load() {
    setBusy(true);
    setError(undefined);
    try {
      const page = await api.identifiers(post.uuid, identifiers.at(-1));
      setIdentifiers((prior) => [...prior, ...page]);
      setMore(page.length === api.pageLimit);
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <span>{msg("source_posts.archive_id", "Archive ID")}</span>
        <code data-selectable-text className="wrap-anywhere text-sm">
          {post.uuid}
        </code>
      </div>
      {identifiers.map((key) => (
        <div
          className="flex flex-col gap-1"
          key={`${key.namespace}:${key.value}`}
        >
          <AccountService namespace={key.namespace} />
          <code data-selectable-text className="wrap-anywhere text-sm">
            {key.namespace} · {key.value}
          </code>
        </div>
      ))}
      {error !== undefined && (
        <PostReadError error={error} retry={() => void load()} />
      )}
      {more && (
        <Button variant="outline" disabled={busy} onClick={() => void load()}>
          {msg("source_posts.more_identifiers", "Load more identifiers")}
        </Button>
      )}
    </div>
  );
}

function PostURLs({ post, api }: { post: PostSummary; api: SourcePostAPI }) {
  const msg = useMsg();
  const [urls, setURLs] = useState(post.urls);
  const [more, setMore] = useState(post.more_urls);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  async function load() {
    setBusy(true);
    setError(undefined);
    try {
      const page = await api.review.urls(post.uuid, urls.at(-1)?.uuid);
      setURLs((prior) => [...prior, ...page]);
      setMore(page.length === api.pageLimit);
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="flex flex-col gap-3">
      {urls.length ? (
        <ul className="flex flex-col gap-2">
          {urls.map((url) => (
            <li key={url.uuid}>
              <PostURL value={url.url} />
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-sm text-muted-foreground">
          {msg("source_posts.no_urls", "No retained source URLs.")}
        </p>
      )}
      {error !== undefined && (
        <PostReadError error={error} retry={() => void load()} />
      )}
      {more && (
        <Button variant="outline" disabled={busy} onClick={() => void load()}>
          {msg("source_review.more_urls", "Load more source URLs")}
        </Button>
      )}
    </div>
  );
}

function PostPublishers({ id, api }: { id: string; api: SourcePostAPI }) {
  const msg = useMsg();
  const load = useCallback(
    (after?: string, signal?: AbortSignal) => api.publishers(id, after, signal),
    [api, id],
  );
  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted-foreground">
        {msg(
          "source_posts.publishers_help",
          "These accounts published the post. Their owners are separate from the performers depicted in its media.",
        )}
      </p>
      <PostRows
        load={load}
        rowKey={(row: Account) => row.uuid}
        pageLimit={api.pageLimit}
        empty={msg(
          "source_posts.no_publishers",
          "No selected publisher accounts",
        )}
        renderRow={(account) => (
          <Card size="sm">
            <CardHeader>
              <CardTitle className="wrap-anywhere">
                <AccountName account={account} />
              </CardTitle>
              <CardDescription>
                <AccountService namespace={account.namespace} />
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-1">
              <span className="text-sm text-muted-foreground">
                {msg("source_posts.owner", "Account owner")}
              </span>
              <AccountOwner ownership={account.ownership} />
            </CardContent>
            <CardFooter>
              <Link
                className={buttonVariants({ variant: "outline" })}
                to="/account-review"
                search={{
                  account: account.uuid,
                  namespace: account.namespace,
                  q: "",
                  ownership: "all",
                }}
              >
                {msg("source_posts.review_account", "Review account")}
              </Link>
            </CardFooter>
          </Card>
        )}
      />
    </div>
  );
}

function PostMediaList({ id, api }: { id: string; api: SourcePostAPI }) {
  const msg = useMsg();
  const load = useCallback(
    (after?: string, signal?: AbortSignal) => api.media(id, after, signal),
    [api, id],
  );
  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted-foreground">
        {msg(
          "source_posts.media_help",
          "Current links, rejected choices and retained candidates are shown separately. Open an item's Sources section to review its association.",
        )}
      </p>
      <PostRows
        load={load}
        rowKey={(row: PostMedia) => row.media.uuid}
        pageLimit={api.pageLimit}
        empty={msg("source_posts.no_media", "No retained media associations")}
        renderRow={(row) => (
          <Card size="sm">
            <CardHeader>
              <CardTitle data-selectable-text className="wrap-anywhere">
                {row.media.title ||
                  msg("source_posts.untitled_media", "Untitled library item")}
                {row.media.title_truncated ? "…" : ""}
              </CardTitle>
              <CardDescription>
                {row.media.kind === "scene"
                  ? msg("scene", "Scene")
                  : msg("image", "Image")}
                {row.media.local_id !== null ? ` #${row.media.local_id}` : ""}
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              <div className="flex flex-wrap gap-2">
                <Badge
                  variant={
                    row.association.state === "conflict"
                      ? "destructive"
                      : "outline"
                  }
                >
                  <LinkState
                    state={row.association.state}
                    attachments={row.linked_attachments}
                  />
                </Badge>
                {row.media.state === "deleted" && (
                  <Badge variant="secondary">
                    {msg("source_posts.deleted", "Deleted from the library")}
                  </Badge>
                )}
                {row.has_retained_evidence && (
                  <Badge variant="secondary">
                    {msg(
                      "source_posts.retained_evidence",
                      "Retained catalog evidence",
                    )}
                  </Badge>
                )}
              </div>
              {(row.association.state === "unlinked" ||
                row.association.state === "conflict") &&
                row.linked_attachments > 0 && (
                  <p className="text-sm text-muted-foreground">
                    {msg(
                      "source_posts.suppressed_attachments",
                      "Attachment evidence is retained, but these post link choices prevent it from selecting this source.",
                    )}
                  </p>
                )}
            </CardContent>
            {row.media.state === "active" && (
              <CardFooter>
                <LibraryLink item={row.media} sources>
                  {msg("source_posts.review_media", "Open Sources")}
                </LibraryLink>
              </CardFooter>
            )}
          </Card>
        )}
      />
    </div>
  );
}

function PostGallery({ id, api }: { id: string; api: SourcePostAPI }) {
  const msg = useMsg();
  const load = useCallback(
    (signal: AbortSignal) => api.album(id, signal),
    [api, id],
  );
  const result = usePostRead(load);
  const album = result.value;
  if (result.busy)
    return (
      <Spinner
        aria-label={msg("source_posts.loading_details", "Loading post details")}
      />
    );
  if (result.error !== undefined)
    return <PostReadError error={result.error} retry={result.retry} />;
  if (!album)
    return (
      <PostEmpty
        title={msg("source_posts.no_album", "No associated album gallery")}
      >
        {msg(
          "source_posts.no_album_help",
          "This post has no saved gallery association. Opening this view does not create one.",
        )}
      </PostEmpty>
    );
  if (album.state === "disabled")
    return (
      <PostEmpty
        title={msg(
          "source_posts.album_disabled",
          "Automatic album gallery disabled",
        )}
      >
        {album.reason ||
          msg(
            "source_posts.album_disabled_help",
            "A saved choice prevents this post from creating or synchronizing an album gallery.",
          )}
      </PostEmpty>
    );
  const gallery = album.gallery;
  if (!gallery) return null;
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle className="wrap-anywhere" data-selectable-text>
          {gallery.title ||
            msg("source_posts.untitled_gallery", "Untitled gallery")}
          {gallery.title_truncated ? "…" : ""}
        </CardTitle>
        <CardDescription>
          {gallery.state === "deleted"
            ? msg("source_posts.deleted", "Deleted from the library")
            : msg("source_posts.album_linked", "Associated album gallery")}
        </CardDescription>
      </CardHeader>
      {album.reason && (
        <CardContent>
          <p className="whitespace-pre-wrap wrap-anywhere" data-selectable-text>
            {album.reason}
          </p>
        </CardContent>
      )}
      {gallery.state === "active" && (
        <CardFooter>
          <LibraryLink item={gallery}>
            {msg("source_posts.open_gallery", "Open gallery")}
          </LibraryLink>
        </CardFooter>
      )}
    </Card>
  );
}

export function PostDetail({
  api,
  id,
  onBack,
}: {
  api: SourcePostAPI;
  id: string;
  onBack: () => void;
}) {
  const msg = useMsg();
  const [albumVersion, setAlbumVersion] = useState(0);
  const refreshAlbum = useCallback(
    () => setAlbumVersion((value) => value + 1),
    [],
  );
  const load = useCallback(
    (signal: AbortSignal) => api.post(id, signal),
    [api, id],
  );
  const result = usePostRead(load);
  const post = result.value;
  return (
    <div className="flex flex-col gap-4">
      <Button variant="outline" className="w-fit" onClick={onBack}>
        {msg("source_posts.back", "Back to posts")}
      </Button>
      {result.busy && (
        <Spinner
          aria-label={msg(
            "source_posts.loading_details",
            "Loading post details",
          )}
        />
      )}
      {result.error !== undefined && (
        <PostReadError error={result.error} retry={result.retry} />
      )}
      {!result.busy && result.error === undefined && post && (
        <>
          <Card>
            <CardHeader>
              <CardTitle className="wrap-anywhere" data-selectable-text>
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
              {post.state === "forgotten" && (
                <Badge variant="outline">
                  {msg("source_posts.forgotten", "Forgotten source post")}
                </Badge>
              )}
              <PostURLs post={post} api={api} />
            </CardContent>
          </Card>
          <PostSection
            title={msg("source_review.captures", "Post text and captures")}
          >
            <SourceCaptures post={id} api={api.review} />
          </PostSection>
          <PostSection
            title={msg("source_posts.publishers", "Publisher accounts")}
          >
            <PostPublishers id={id} api={api} />
          </PostSection>
          <PostSection title={msg("source_posts.media", "Media associations")}>
            <PostMediaList id={id} api={api} />
          </PostSection>
          <PostSection
            title={msg("source_threads.title", "Thread and replies")}
          >
            <PostThread key={id} id={id} api={api} />
          </PostSection>
          <PostSection title={msg("source_albums.order", "Source order")}>
            <SourceAlbum
              post={id}
              endpoint={api.endpoint}
              onPublished={refreshAlbum}
            />
          </PostSection>
          <PostSection title={msg("source_posts.album", "Album gallery")}>
            <PostGallery key={`${id}:${albumVersion}`} id={id} api={api} />
          </PostSection>
          <PostSection title={msg("source_posts.identifiers", "Identifiers")}>
            <PostIdentifiers post={post} api={api} />
          </PostSection>
        </>
      )}
      {post && (
        <PostSection title={msg("post_merge.title", "Merge source posts")}>
          <PostMerge
            key={`${api.endpoint}:${id}`}
            api={api}
            requested={id}
            post={post}
            onSaved={() => {
              refreshAlbum();
              result.retry();
            }}
          />
        </PostSection>
      )}
    </div>
  );
}
