import { Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Spinner } from "@/components/ui/spinner";
import type { GallerySources } from "./gallery-sources";

export function GalleryOrigin({
  gallery,
  sources,
  onViewAlbums,
}: {
  gallery: {
    folder?: { path: string } | null;
    files: readonly { path: string }[];
  };
  sources: GallerySources;
  onViewAlbums: () => void;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const posts = sources.data?.items;
  const hasPosts = posts !== undefined && posts.length > 0;
  const folder = gallery.folder?.path;
  const files = [...new Set(gallery.files.map((file) => file.path))];
  const resolved = sources.data !== undefined;
  const label = hasPosts
    ? msg("gallery_origin.source_album", "Source-post album")
    : folder
      ? msg("gallery_origin.folder_gallery", "Folder gallery")
      : files.length > 0
        ? msg("gallery_origin.zip_gallery", "ZIP gallery")
        : resolved
          ? msg("gallery_origin.manual_gallery", "Manual gallery")
          : undefined;

  return (
    <Card size="sm" data-gallery-origin>
      <CardHeader>
        <CardTitle>{msg("gallery_origin.title", "Gallery source")}</CardTitle>
        {hasPosts && (
          <CardDescription>
            {msg(
              "gallery_origin.album_help",
              "Linked posts explain this album's grouping. Shared images and videos can retain different titles.",
            )}
          </CardDescription>
        )}
      </CardHeader>
      <CardContent className="flex min-w-0 flex-col gap-3">
        {label && <Badge variant="secondary">{label}</Badge>}
        {!resolved && sources.busy && (
          <div
            className="flex items-center gap-2 text-sm text-muted-foreground"
            role="status"
          >
            <Spinner />
            {msg("gallery_origin.loading", "Checking gallery source")}
          </div>
        )}
        {sources.error !== undefined && (
          <Alert variant="destructive">
            <AlertTitle>
              {msg("gallery_origin.failed", "Could not check linked posts")}
            </AlertTitle>
            <AlertDescription>
              <Button variant="outline" size="sm" onClick={sources.reload}>
                {msg("gallery_origin.retry", "Retry source lookup")}
              </Button>
            </AlertDescription>
          </Alert>
        )}
        {hasPosts && (
          <>
            <div className="flex min-w-0 flex-col gap-2">
              {posts.slice(0, 3).map((post) => {
                const identifier =
                  post.identifiers.find(
                    (id) => !id.namespace.startsWith("legacy:"),
                  ) ?? post.identifiers[0];
                const title = post.latest_capture?.title?.trim();
                return (
                  <Link
                    key={post.uuid}
                    className="text-primary hover:underline wrap-anywhere"
                    to="/source-posts"
                    search={{
                      post: post.uuid,
                      mode: "all",
                      value: "",
                      namespace: "",
                    }}
                  >
                    {title ||
                      intl.formatMessage(
                        {
                          id: "gallery_origin.post_id",
                          defaultMessage: "Post {id}",
                        },
                        { id: identifier?.value ?? post.uuid },
                      )}
                    {title && post.latest_capture?.title_truncated ? "…" : ""}
                  </Link>
                );
              })}
            </div>
            <Button
              variant="outline"
              size="sm"
              className="self-start"
              onClick={onViewAlbums}
            >
              {msg("gallery_origin.view_albums", "View source albums")}
            </Button>
          </>
        )}
        {folder && (
          <div className="flex min-w-0 flex-col gap-1">
            <span className="text-muted-foreground">
              {msg("folder", "Folder")}
            </span>
            <span className="wrap-anywhere" data-selectable-text>
              {folder}
            </span>
          </div>
        )}
        {files.map((path) => (
          <div key={path} className="flex min-w-0 flex-col gap-1">
            <span className="text-muted-foreground">
              {msg("gallery_origin.archive_file", "Archive file")}
            </span>
            <span className="wrap-anywhere" data-selectable-text>
              {path}
            </span>
          </div>
        ))}
        {resolved && !hasPosts && !folder && files.length === 0 && (
          <p className="text-sm text-muted-foreground">
            {msg(
              "gallery_origin.manual_help",
              "Images and videos grouped without a linked source post or backing folder or archive.",
            )}
          </p>
        )}
      </CardContent>
    </Card>
  );
}
