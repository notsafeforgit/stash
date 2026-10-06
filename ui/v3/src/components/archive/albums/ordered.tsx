import { useCallback, useMemo } from "react";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import {
  createSourceAlbumAPI,
  type AlbumSlot,
} from "@/core/native-archive/source-album-api";
import { NativeArchiveError } from "@/core/native-archive/client";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { PostEmpty, PostReadError } from "../posts/shared";
import { LibraryLink } from "../posts/library-link";
import { useAlbumPages } from "./read";

export function AlbumReadError({
  error,
  retry,
}: {
  error: unknown;
  retry: () => void;
}) {
  const msg = useMsg();
  if (!(error instanceof NativeArchiveError) || error.code !== "album_changed")
    return <PostReadError error={error} retry={retry} />;
  return (
    <Alert variant="destructive">
      <AlertTitle>
        {msg("source_albums.changed", "This album changed")}
      </AlertTitle>
      <AlertDescription>
        <p>
          {msg(
            "source_albums.changed_help",
            "Reload to view the current source order and library associations.",
          )}
        </p>
        <Button variant="outline" size="sm" onClick={retry}>
          {msg("source_albums.reload", "Reload album")}
        </Button>
      </AlertDescription>
    </Alert>
  );
}

function Slot({ slot }: { slot: AlbumSlot }) {
  const msg = useMsg();
  const intl = useIntl();
  const position =
    slot.position === slot.through
      ? intl.formatMessage(
          {
            id: "source_albums.position",
            defaultMessage: "Position {position}",
          },
          { position: slot.position + 1 },
        )
      : intl.formatMessage(
          {
            id: "source_albums.positions",
            defaultMessage: "Positions {first}–{last}",
          },
          { first: slot.position + 1, last: slot.through + 1 },
        );
  const media = slot.media;
  return (
    <Card size="sm" data-source-position={slot.position}>
      <CardHeader>
        <CardDescription>{position}</CardDescription>
        <CardTitle className="wrap-anywhere" data-selectable-text>
          {!slot.attachment
            ? msg("source_albums.unknown_slots", "Source details unavailable")
            : media?.title ||
              (slot.media_kind === "video"
                ? msg("source_albums.video", "Video")
                : slot.media_kind === "image"
                  ? msg("source_albums.image", "Image")
                  : msg("source_albums.attachment", "Source attachment"))}
          {media?.title_truncated ? "…" : ""}
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-2 text-sm">
        {!slot.attachment ? (
          <p className="text-muted-foreground">
            {msg(
              "source_albums.gap_help",
              "The source list does not describe these positions yet. Their download state is unknown.",
            )}
          </p>
        ) : (
          <>
            <div className="flex flex-wrap gap-2">
              {slot.media_kind !== "unknown" && (
                <Badge variant="outline">
                  {slot.media_kind === "video"
                    ? msg("source_albums.video", "Video")
                    : msg("source_albums.image", "Image")}
                </Badge>
              )}
              {slot.selection_state === "unlinked" && (
                <Badge variant="outline">
                  {msg("source_albums.unlinked", "Attachment link rejected")}
                </Badge>
              )}
              {slot.selection_state === "undecided" && (
                <Badge variant="outline">
                  {msg("source_albums.undecided", "Attachment needs review")}
                </Badge>
              )}
              {slot.selection_state === "unselected" && (
                <Badge variant="secondary">
                  {msg("source_albums.unselected", "No selected library item")}
                </Badge>
              )}
              {media?.state === "deleted" && (
                <Badge variant="secondary">
                  {msg("source_posts.deleted", "Deleted from the library")}
                </Badge>
              )}
              {slot.post_link_state === "unlinked" && (
                <Badge variant="outline">
                  {msg("source_albums.post_unlinked", "Post link rejected")}
                </Badge>
              )}
              {slot.post_link_state === "conflict" && (
                <Badge variant="destructive">
                  {msg("source_albums.post_conflict", "Conflicting post links")}
                </Badge>
              )}
              {slot.gallery_membership === "included" && (
                <Badge variant="secondary">
                  {msg("source_albums.included", "In this gallery")}
                </Badge>
              )}
              {slot.gallery_membership === "excluded" && (
                <Badge variant="outline">
                  {msg(
                    "source_albums.excluded",
                    "Manually excluded from gallery",
                  )}
                </Badge>
              )}
              {media?.state === "active" &&
                slot.gallery_membership === "absent" && (
                  <Badge variant="outline">
                    {msg("source_albums.absent", "Not in this gallery")}
                  </Badge>
                )}
            </div>
            {media?.state === "active" && (
              <p className="text-muted-foreground">
                {intl.formatMessage(
                  {
                    id: "source_albums.files",
                    defaultMessage:
                      "{count, plural, =0 {No files registered in Stash} one {# file registered in Stash} other {# files registered in Stash}}",
                  },
                  { count: slot.registered_files },
                )}
              </p>
            )}
            <code className="wrap-anywhere" data-selectable-text>
              {slot.attachment.reference.namespace}:
              {slot.attachment.reference.value}
            </code>
          </>
        )}
      </CardContent>
      {media?.state === "active" && (
        <CardFooter className="flex flex-wrap gap-2">
          <LibraryLink item={media}>
            {msg("source_albums.open_media", "Open media")}
          </LibraryLink>
          <LibraryLink item={media} sources>
            {msg("source_posts.review_media", "Open Sources")}
          </LibraryLink>
        </CardFooter>
      )}
    </Card>
  );
}

export function SourceAlbum({
  post,
  endpoint,
}: {
  post: string;
  endpoint?: string;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const api = useMemo(() => createSourceAlbumAPI(endpoint), [endpoint]);
  const load = useCallback(
    async (after: number | undefined, signal: AbortSignal) => {
      const page = await api.album(post, after, signal);
      return {
        signature: page.signature,
        items: page.slots,
        next: page.next_after,
        header: page,
      };
    },
    [api, post],
  );
  const result = useAlbumPages(load);
  const page = result.data?.header;
  const selection = page?.selection;
  return (
    <div className="flex flex-col gap-4">
      {result.error !== undefined && (
        <AlbumReadError error={result.error} retry={result.reload} />
      )}
      {result.busy && (
        <Spinner
          aria-label={msg("source_albums.loading", "Loading source album")}
        />
      )}
      {page &&
        (!selection ? (
          <PostEmpty
            title={msg("source_albums.no_list", "No selected source list")}
          >
            {msg(
              "source_albums.no_list_help",
              "File names and folder membership alone do not establish source order.",
            )}
          </PostEmpty>
        ) : (
          <>
            <div className="flex flex-wrap gap-2">
              <Badge variant="outline">
                {selection.complete
                  ? msg("source_albums.complete", "Complete source list")
                  : msg("source_albums.partial", "Partial source list")}
              </Badge>
              {selection.mode === "disabled" && (
                <Badge variant="secondary">
                  {msg(
                    "source_albums.selection_disabled",
                    "Source selection disabled",
                  )}
                </Badge>
              )}
              {page.album?.state === "disabled" && (
                <Badge variant="secondary">
                  {msg(
                    "source_posts.album_disabled",
                    "Automatic album gallery disabled",
                  )}
                </Badge>
              )}
              {page.album?.gallery?.state === "deleted" && (
                <Badge variant="secondary">
                  {msg(
                    "source_albums.gallery_deleted",
                    "Associated gallery deleted",
                  )}
                </Badge>
              )}
            </div>
            <p className="text-sm text-muted-foreground">
              {intl.formatMessage(
                {
                  id: "source_albums.known",
                  defaultMessage:
                    "{count, plural, one {# described source position} other {# described source positions}}",
                },
                { count: selection.entry_count },
              )}
              {selection.expected_count !== null && (
                <>
                  {" "}
                  ·{" "}
                  {intl.formatMessage(
                    {
                      id: "source_albums.expected",
                      defaultMessage:
                        "{count, number} positions declared by the source",
                    },
                    { count: selection.expected_count },
                  )}
                </>
              )}
            </p>
            <p className="text-sm text-muted-foreground">
              {msg(
                "source_albums.availability_help",
                "Source-list completeness is separate from file availability. Registered files may be offline; unselected items may need downloading or linking.",
              )}
            </p>
            {result.data?.items.map((slot) => (
              <Slot key={slot.position} slot={slot} />
            ))}
            {!result.data?.items.length && (
              <PostEmpty
                title={msg(
                  "source_albums.empty",
                  "The selected source list has no positions",
                )}
              />
            )}
          </>
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
