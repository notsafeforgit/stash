import { Link } from "@tanstack/react-router";
import { useMsg } from "@/hooks/message";
import { cn } from "@/lib/utils";
import { Button, buttonVariants } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { PostEmpty, PostSection } from "../posts/shared";
import { AlbumReadError, SourceAlbum } from "./ordered";
import type { GallerySources } from "./gallery-sources";

export function GallerySourceAlbums({
  sources: result,
  onPublished,
}: {
  sources: GallerySources;
  onPublished?: () => void | Promise<void>;
}) {
  const msg = useMsg();
  return (
    <div className="flex min-h-0 flex-col gap-4 p-4 md:flex-1 md:overflow-y-auto">
      <p className="text-sm text-muted-foreground">
        {msg(
          "source_albums.gallery_help",
          "Open a source post to view its images and videos in source order. Each post keeps its own order, including repeated and unavailable positions.",
        )}
      </p>
      {result.busy && (
        <Spinner
          aria-label={msg("source_albums.loading", "Loading source album")}
        />
      )}
      {result.error !== undefined && (
        <AlbumReadError error={result.error} retry={result.reload} />
      )}
      {!result.busy &&
        result.error === undefined &&
        result.data?.items.length === 0 && (
          <PostEmpty
            title={msg("source_albums.no_posts", "No associated source posts")}
          >
            {msg(
              "source_albums.manual_help",
              "Manual, folder and ZIP galleries do not need a source post. Their existing membership is preserved.",
            )}
          </PostEmpty>
        )}
      {result.data?.items.map((post) => (
        <PostSection
          key={post.uuid}
          title={
            post.latest_capture?.title ||
            msg("source_review.untitled", "Untitled source post")
          }
        >
          <div className="flex flex-col gap-4">
            <Link
              className={cn(
                buttonVariants({
                  variant: "outline",
                  className: "self-start",
                }),
              )}
              to="/source-posts"
              search={{
                post: post.uuid,
                mode: "all",
                value: "",
                namespace: "",
              }}
            >
              {msg("source_albums.open_post", "Open source post")}
            </Link>
            <SourceAlbum
              post={post.uuid}
              endpoint={result.endpoint}
              onPublished={onPublished}
            />
          </div>
        </PostSection>
      ))}
      {result.data?.next !== null && result.data?.next !== undefined && (
        <Button
          variant="outline"
          disabled={result.busy || result.error !== undefined}
          onClick={() => void result.more()}
        >
          {msg("source_posts.load_more", "Load more")}
        </Button>
      )}
    </div>
  );
}
