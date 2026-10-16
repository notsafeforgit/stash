import { useEffect, useState } from "react";
import { ChevronLeft, ChevronRight, ZoomIn, ZoomOut } from "lucide-react";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { FieldSet } from "@/components/ui/field";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Slider } from "@/components/ui/slider";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { getPlatformURL } from "@/core/platform-url";
import { useMsg } from "@/hooks/message";
import { formatDurationMs } from "@/components/player/position-scrubber";

const framesSchema = z
  .array(
    z.object({
      at: z.number().finite().nonnegative(),
      jpeg: z.string().regex(/^[A-Za-z0-9+/]+=*$/),
    }),
  )
  .min(1)
  .max(25);
type Frame = z.infer<typeof framesSchema>[number];
type Range = {
  start: number;
  end: number;
  mode: "samples" | "frames";
  focus: number;
};

interface PickerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  sceneId: string;
  duration: number;
  initialAt: number;
  onSelect: (at: number) => void;
}

export function SceneThumbnailPicker({ open, ...props }: PickerProps) {
  // Closing removes every preview and aborts extraction, including a request
  // that has not completed. Reopening starts with the current playback time.
  return open ? <ThumbnailSession {...props} key={props.sceneId} /> : null;
}

function ThumbnailSession({
  sceneId,
  duration,
  initialAt,
  onOpenChange,
  onSelect,
}: Omit<PickerProps, "open">) {
  const msg = useMsg();
  const [range, setRange] = useState<Range>(() => ({
    start: Math.max(0, Math.min(initialAt, duration - 0.001) - 0.2),
    end: duration,
    mode: "frames",
    focus: initialAt,
  }));
  const [result, setResult] = useState<{
    range: Range;
    frames?: Frame[];
    error?: string;
    index: number;
  } | null>(null);
  const current = result?.range === range ? result : null;
  const frames = current?.frames ?? [];
  const index = current?.index ?? 0;
  const selected = frames[index];
  const first = frames[0];
  const last = frames.at(-1);
  const span =
    range.mode === "frames" && first && last
      ? Math.max(0.1, last.at - first.at)
      : range.end - range.start;
  const busy = !current;
  const loadingLabel = msg(
    "scene_cover.picker_loading",
    "Preparing frame previews…",
  );

  useEffect(() => {
    const controller = new AbortController();
    const url = getPlatformURL(
      `scene/${encodeURIComponent(sceneId)}/frame-previews`,
    );
    url.search = new URLSearchParams({
      start: String(range.start),
      end: String(range.end),
      mode: range.mode,
    }).toString();
    // Defer until this effect survives setup/cleanup replay, avoiding a wasted
    // decode on React development remounts or an immediately closed dialog.
    void Promise.resolve()
      .then(async () => {
        if (controller.signal.aborted) return;
        const response = await fetch(url, {
          credentials: "include",
          cache: "no-store",
          signal: controller.signal,
        });
        if (!response.ok) throw new Error(await response.text());
        const loaded = framesSchema.parse(await response.json());
        if (controller.signal.aborted) return;
        const closest = loaded.reduce(
          (best, frame, i) =>
            Math.abs(frame.at - range.focus) <
            Math.abs((loaded[best]?.at ?? 0) - range.focus)
              ? i
              : best,
          0,
        );
        setResult({ range, frames: loaded, index: closest });
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted)
          setResult({
            range,
            error: error instanceof Error ? error.message : String(error),
            index: 0,
          });
      });
    return () => controller.abort();
  }, [sceneId, range]);

  function select(index: number) {
    if (current) setResult({ ...current, index });
  }
  function windowAround(at: number, seconds: number) {
    const width = Math.min(duration, seconds);
    const start = Math.max(0, Math.min(duration - width, at - width / 2));
    setRange({
      start,
      end: Math.min(duration, start + width),
      mode: "samples",
      focus: at,
    });
  }
  function refine() {
    if (!selected) return;
    if (span <= 8) {
      setRange({
        start: Math.max(0, selected.at - 0.2),
        end: duration,
        mode: "frames",
        focus: selected.at,
      });
    } else windowAround(selected.at, span / 4);
  }

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {msg("scene_cover.picker", "Choose thumbnail")}
          </DialogTitle>
          <DialogDescription>
            {msg(
              "scene_cover.picker_description",
              "Explore a time range, then zoom in to choose an individual frame. Only your chosen cover is saved.",
            )}
          </DialogDescription>
        </DialogHeader>
        <div
          className="flex h-[min(32dvh,20rem)] min-h-28 items-center justify-center overflow-hidden rounded-md bg-muted"
          aria-busy={busy}
        >
          {selected ? (
            <img
              src={`data:image/jpeg;base64,${selected.jpeg}`}
              alt={msg("scene_cover.picker_preview", "Selected frame preview")}
              className="h-full w-full object-contain"
            />
          ) : busy ? (
            <Skeleton className="h-full w-full" aria-label={loadingLabel} />
          ) : null}
        </div>
        <div
          className="flex min-h-5 justify-center text-sm tabular-nums"
          role="status"
        >
          {busy
            ? loadingLabel
            : selected
              ? formatDurationMs(selected.at)
              : null}
        </div>
        {current?.error ? (
          <Alert variant="destructive">
            <AlertDescription>
              {current.error}
              <Button variant="outline" onClick={() => setRange({ ...range })}>
                {msg("actions.retry", "Retry")}
              </Button>
            </AlertDescription>
          </Alert>
        ) : null}
        <FieldSet
          className="min-w-0 flex-row gap-2 overflow-x-auto pb-1"
          aria-label={msg("scene_cover.picker_frames", "Frame previews")}
        >
          {frames.map((frame, i) => (
            <Button
              key={frame.at}
              variant={i === index ? "default" : "outline"}
              className="h-auto w-20 shrink-0 flex-col gap-1 p-1"
              aria-pressed={i === index}
              aria-label={formatDurationMs(frame.at)}
              onClick={() => select(i)}
            >
              <img
                src={`data:image/jpeg;base64,${frame.jpeg}`}
                alt=""
                className="h-12 w-full object-contain"
                draggable={false}
              />
              <span className="text-xs tabular-nums">
                {formatDurationMs(frame.at)}
              </span>
            </Button>
          ))}
        </FieldSet>
        <Slider
          aria-label={msg("scene_cover.picker_frames", "Frame previews")}
          value={[index]}
          min={0}
          max={Math.max(1, frames.length - 1)}
          step={1}
          disabled={frames.length < 2}
          onValueChange={(value) =>
            select(Array.isArray(value) ? (value[0] ?? 0) : value)
          }
        />
        <div className="flex flex-wrap items-center justify-center gap-2">
          <Button
            variant="outline"
            disabled={busy || !selected || range.start <= 0}
            onClick={() => {
              if (range.mode === "frames")
                setRange({
                  start: Math.max(0, range.start - span),
                  end: duration,
                  mode: "frames",
                  focus: Math.max(0, range.start - span / 2),
                });
              else windowAround(range.start - span / 2, span);
            }}
            aria-label={msg("scene_cover.picker_earlier", "Earlier interval")}
          >
            <ChevronLeft />
          </Button>
          <Button
            variant="outline"
            disabled={busy || !selected || range.mode === "frames"}
            onClick={refine}
          >
            <ZoomIn data-icon="inline-start" />
            {msg("scene_cover.picker_refine", "Zoom in")}
          </Button>
          <Button
            variant="outline"
            disabled={
              busy ||
              !selected ||
              (range.mode === "samples" && span >= duration)
            }
            onClick={() =>
              selected &&
              windowAround(selected.at, range.mode === "frames" ? 8 : span * 4)
            }
          >
            <ZoomOut data-icon="inline-start" />
            {msg("scene_cover.picker_wider", "Wider view")}
          </Button>
          <Button
            variant="outline"
            disabled={
              busy ||
              !last ||
              (range.mode === "samples" && range.end >= duration) ||
              (range.mode === "frames" && frames.length < 25)
            }
            onClick={() => {
              if (!last) return;
              if (range.mode === "frames")
                setRange({
                  start: Math.min(duration - 0.00001, last.at + 0.00001),
                  end: duration,
                  mode: "frames",
                  focus: last.at + 0.00001,
                });
              else windowAround(range.end + span / 2, span);
            }}
            aria-label={msg("scene_cover.picker_later", "Later interval")}
          >
            <ChevronRight />
          </Button>
          <Button
            variant="ghost"
            disabled={busy}
            onClick={() => windowAround(duration / 2, duration)}
          >
            {msg("scene_cover.picker_whole", "Whole video")}
          </Button>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {msg("actions.cancel", "Cancel")}
          </Button>
          <Button
            disabled={!selected || busy}
            onClick={() => {
              if (selected) {
                onSelect(selected.at);
                onOpenChange(false);
              }
            }}
          >
            {msg("scene_cover.picker_use", "Use this frame")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
