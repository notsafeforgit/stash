import { useMsg } from "@/hooks/message";
import { useMemo, useState } from "react";
import {
  useScenePlayerControls,
  useScenePlayerValue,
} from "@/components/player/scene-player-controls";
import { TvSlider } from "./tv-slider";
import { useSpriteInfo } from "@/hooks/use-sprite-info";
import type { PlaybackRange } from "@/core/marker-range";
import type { TvScene } from "./use-tv-mutations";

export function tvTime(seconds: number): string {
  const value = Math.max(0, Math.floor(seconds));
  const hours = Math.floor(value / 3600);
  return `${hours ? `${hours}:` : ""}${String(Math.floor(value / 60) % 60).padStart(hours ? 2 : 1, "0")}:${String(value % 60).padStart(2, "0")}`;
}

export function TvTimeline({
  scene,
  range,
}: {
  scene: TvScene;
  range: PlaybackRange;
}) {
  const msg = useMsg();
  const position = useScenePlayerValue("position");
  const controls = useScenePlayerControls();
  const [draft, setDraft] = useState<number | null>(null);
  const [previewOpened, setPreviewOpened] = useState(false);
  const [previewVisible, setPreviewVisible] = useState(false);
  const sprites = useSpriteInfo(
    previewOpened ? (scene.paths.vtt ?? undefined) : undefined,
  );
  const value = Math.max(range.start, Math.min(range.end, draft ?? position));
  const sprite =
    sprites?.[
      Math.min(
        sprites.length - 1,
        Math.floor((value / (scene.files[0]?.duration || 1)) * sprites.length),
      )
    ];
  const markers = useMemo(
    () => [...scene.scene_markers].sort((a, b) => a.seconds - b.seconds),
    [scene.scene_markers],
  );
  const markerPath = useMemo(() => {
    const x = (time: number) =>
      Math.max(
        0,
        Math.min(
          1000,
          ((time - range.start) / (range.end - range.start)) * 1000,
        ),
      );
    return markers
      .filter(
        (marker) => marker.seconds >= range.start && marker.seconds < range.end,
      )
      .map(
        (marker) =>
          `M${x(marker.seconds)},0v8${marker.end_seconds ? `M${x(marker.seconds)},4H${x(marker.end_seconds)}` : ""}`,
      )
      .join(" ");
  }, [markers, range.start, range.end]);
  let currentMarker: TvScene["scene_markers"][number] | undefined;
  for (const marker of markers) {
    if (marker.seconds > value) break;
    currentMarker = marker;
  }
  return (
    <div
      className="pointer-events-auto relative flex min-w-0 flex-col gap-1"
      data-tv-interactive
    >
      {previewVisible && sprite && (
        <div
          className="pointer-events-none absolute bottom-full left-1/2 mb-3 -translate-x-1/2 overflow-hidden rounded-lg border bg-background"
          aria-hidden="true"
          style={{
            width: sprite.w,
            height: sprite.h,
            backgroundImage: `url(${JSON.stringify(sprite.url)})`,
            backgroundPosition: `-${sprite.x}px -${sprite.y}px`,
          }}
        />
      )}
      <div className="flex min-w-0 justify-between gap-3 text-xs">
        <span className="truncate">
          {currentMarker
            ? `${currentMarker.title} · ${currentMarker.primary_tag.name}`
            : ""}
        </span>
        <span className="shrink-0 tabular-nums">
          {tvTime(value)} / {tvTime(range.end)}
        </span>
      </div>
      <div className="relative">
        <svg
          aria-hidden
          className="pointer-events-none absolute inset-x-0 top-2.5 h-2 w-full text-primary/40"
          viewBox="0 0 1000 8"
          preserveAspectRatio="none"
        >
          <title>{msg("tv.timeline_markers", "Scene markers")}</title>
          <path
            d={markerPath}
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
          />
        </svg>
        <TvSlider
          label={msg("tv.text.scene_position", "Scene position")}
          value={value}
          min={range.start}
          max={range.end}
          step={0.1}
          onPreviewChange={(visible) => {
            if (visible) setPreviewOpened(true);
            setPreviewVisible(visible);
          }}
          onChange={setDraft}
          onCommit={(next) => {
            controls.seek(next);
            setDraft(null);
          }}
        />
      </div>
    </div>
  );
}
