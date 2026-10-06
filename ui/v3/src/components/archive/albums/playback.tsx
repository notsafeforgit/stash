import { useState } from "react";
import { useIntl } from "react-intl";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { useMsg } from "@/hooks/message";
import { useCommittedRef } from "@/hooks/use-committed-ref";
import type {
  AlbumPage,
  AlbumSlot,
} from "@/core/native-archive/source-album-api";
import { NativeArchiveError } from "@/core/native-archive/client";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { PostReadError } from "../posts/shared";
import { LibraryLink } from "../posts/library-link";
import type { AlbumReadPage } from "./read";
import { albumPlaybackState, type AlbumPlaybackState } from "./playback-state";
import { AlbumImage, AlbumVideo } from "./playback-media";

function Unplayable({
  reason,
}: {
  reason: Extract<AlbumPlaybackState, { kind: "unavailable" }>["reason"];
}) {
  const msg = useMsg();
  const labels = {
    gap: msg(
      "album_playback.gap",
      "The source has not described these positions yet.",
    ),
    rejected: msg(
      "album_playback.rejected",
      "This source link was explicitly rejected.",
    ),
    conflict: msg(
      "album_playback.conflict",
      "Resolve the conflicting source links before playing this position.",
    ),
    excluded: msg(
      "album_playback.excluded",
      "This media was manually excluded from the gallery.",
    ),
    deleted: msg(
      "album_playback.deleted",
      "The linked media was deleted from the library.",
    ),
    unselected: msg(
      "album_playback.unselected",
      "No library media is selected for this source attachment.",
    ),
    no_files: msg(
      "album_playback.no_files",
      "The selected media has no registered files.",
    ),
  };
  return (
    <Alert className="max-w-md">
      <AlertTitle>
        {msg("album_playback.no_media", "No playable media at this position")}
      </AlertTitle>
      <AlertDescription>{labels[reason]}</AlertDescription>
    </Alert>
  );
}

export function AlbumPlayback({
  data,
  position: initialPosition,
  signature,
  busy,
  error,
  onMore,
  onClose,
}: {
  data?: AlbumReadPage<AlbumSlot, number, AlbumPage>;
  position: number;
  signature: string;
  busy: boolean;
  error: unknown;
  onMore: () => Promise<void>;
  onClose: () => void;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const [position, setPosition] = useState(initialPosition);
  const latestPosition = useCommittedRef(position);
  const slots = data?.items ?? [];
  const index = slots.findIndex((slot) => slot.position === position);
  const slot = slots[index];
  const changed =
    (data !== undefined && data.signature !== signature) ||
    (error instanceof NativeArchiveError && error.code === "album_changed");
  const previous = slots.findLast((item) => item.position < position)?.position;
  const next =
    index < 0
      ? undefined
      : (slots[index + 1]?.position ??
        (data?.next !== null ? slot!.through + 1 : undefined));
  const state = slot && !changed ? albumPlaybackState(slot) : undefined;
  function advance() {
    // Ignore an outgoing video's late completion after explicit navigation.
    if (
      latestPosition.current !== position ||
      changed ||
      busy ||
      next === undefined
    )
      return;
    setPosition(next);
    if (!slots[index + 1]) void onMore();
  }
  const label =
    slot && slot.through > slot.position
      ? intl.formatMessage(
          {
            id: "source_albums.positions",
            defaultMessage: "Positions {first}–{last}",
          },
          { first: slot.position + 1, last: slot.through + 1 },
        )
      : intl.formatMessage(
          {
            id: "source_albums.position",
            defaultMessage: "Position {position}",
          },
          { position: position + 1 },
        );
  const title = slot?.media?.title;
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="flex h-[90dvh] max-w-[calc(100%-1rem)] flex-col sm:max-w-6xl">
        <DialogHeader className="shrink-0 pr-8">
          <DialogTitle>
            {msg("album_playback.title", "Source album viewer")}
          </DialogTitle>
          <DialogDescription>
            {msg(
              "album_playback.order",
              "Images and videos follow the source order, including repeated and unavailable positions.",
            )}
          </DialogDescription>
        </DialogHeader>
        {changed ? (
          <Alert>
            <AlertTitle>
              {msg("source_albums.changed", "This album changed")}
            </AlertTitle>
            <AlertDescription>
              <p>
                {msg(
                  "album_playback.changed_help",
                  "Return to the album and reload its current order and links before continuing playback.",
                )}
              </p>
              <Button variant="outline" onClick={onClose}>
                {msg("album_playback.return", "Return to album")}
              </Button>
            </AlertDescription>
          </Alert>
        ) : (
          <>
            <div
              className="flex min-h-0 flex-1 items-center justify-center overflow-hidden rounded-lg bg-muted"
              data-album-viewer-position={position}
            >
              {state?.kind === "image" ? (
                <AlbumImage key={`${position}:${state.uuid}`} id={state.id} />
              ) : state?.kind === "scene" ? (
                <AlbumVideo
                  id={state.id}
                  playbackKey={`${data?.header.post_uuid}:${signature}:${position}:${state.uuid}`}
                  onNext={next !== undefined ? advance : undefined}
                />
              ) : state?.kind === "unavailable" ? (
                <Unplayable reason={state.reason} />
              ) : busy ? (
                <Spinner />
              ) : error !== undefined ? (
                <PostReadError error={error} retry={() => void onMore()} />
              ) : null}
            </div>
            <div className="flex shrink-0 flex-col gap-2">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p
                  className="min-w-0 flex-1 wrap-anywhere text-sm"
                  aria-live="polite"
                >
                  {label}
                  {title ? ` · ${title}` : ""}
                  {slot?.media?.title_truncated ? "…" : ""}
                </p>
                {slot?.media?.state === "active" && (
                  <LibraryLink item={slot.media}>
                    {msg("source_albums.open_media", "Open media")}
                  </LibraryLink>
                )}
              </div>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <Button
                  variant="outline"
                  disabled={previous === undefined || !data}
                  onClick={() => {
                    if (previous !== undefined) setPosition(previous);
                  }}
                >
                  <ChevronLeft data-icon="inline-start" />
                  {msg("album_playback.previous", "Previous position")}
                </Button>
                <Button
                  variant="outline"
                  disabled={next === undefined || busy}
                  onClick={advance}
                >
                  {msg("album_playback.next", "Next position")}
                  <ChevronRight data-icon="inline-end" />
                </Button>
              </div>
            </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
