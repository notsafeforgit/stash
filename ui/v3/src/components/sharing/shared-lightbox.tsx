import { createContext, useContext, useMemo, useState } from "react";
import {
  ImageLightbox,
  type LightboxSlide,
} from "@/components/lightbox/lightbox";
import {
  SceneLightbox,
  type SceneSlide,
  type SceneLightboxSlideProps,
} from "@/components/lightbox/scene-lightbox";
import { LightboxOverlay } from "@/components/lightbox/lightbox-overlay";
import { SceneSlidePoster } from "@/components/lightbox/scene-slide-content";
import { PlayerCloseButton } from "@/components/player/player-close-button";
import { cn } from "@/lib/utils";
import type { SharedDetail, SharedMedia } from "./share-contract";
import {
  ShareDownload,
  SharedMediaStatus,
  SharedPlayback,
} from "./shared-media-viewer";
import { useSharedMedia } from "./use-shared-media";

const ShareBaseContext = createContext<URL | null>(null);

function SharedSceneSlide({
  slide,
  isActive,
  ...controls
}: SceneLightboxSlideProps) {
  const base = useContext(ShareBaseContext);
  if (!base) throw new Error("Missing share context");
  if (!isActive) return <SceneSlidePoster slide={slide} />;
  return (
    <SharedSceneContent
      mediaKey={slide.sceneId}
      base={base}
      active={isActive}
      controls={controls}
    />
  );
}

function SharedSceneContent({
  mediaKey,
  base,
  active,
  controls,
}: {
  mediaKey: string;
  base: URL;
  active: boolean;
  controls: Omit<SceneLightboxSlideProps, "slide" | "isActive">;
}) {
  const state = useSharedMedia(mediaKey, base);
  const detail = state.kind === "ready" ? state.detail : undefined;
  const [retained, setRetained] = useState<SharedDetail>();
  if (detail && detail !== retained) setRetained(detail);
  const playerDetail = detail ?? retained;
  const [chromeVisible, setChromeVisible] = useState(true);
  return (
    <div className="relative flex size-full items-center justify-center bg-black">
      {playerDetail && (
        <SharedPlayback
          detail={playerDetail}
          base={base}
          suspended={!active || !detail}
          lightbox={controls}
          onControlsVisibilityChange={setChromeVisible}
        />
      )}
      {detail?.media.title && (
        <LightboxOverlay
          position="top"
          passThrough
          className={cn(!chromeVisible && "opacity-0")}
        >
          <p className="min-w-0 truncate font-medium">{detail.media.title}</p>
        </LightboxOverlay>
      )}
      {!detail && (
        <div className="absolute inset-0 flex items-center justify-center bg-black">
          <SharedMediaStatus loading={state.kind === "loading"} />
          {controls.onClose && (
            <div className="viewport-controls absolute inset-x-0 bottom-0 flex justify-end">
              <PlayerCloseButton onClose={controls.onClose} />
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export function SharedLightbox({
  media,
  selectedKey,
  base,
  onClose,
}: {
  media: SharedMedia[];
  selectedKey: string;
  base: URL;
  onClose: () => void;
}) {
  const selected = media.find((item) => item.key === selectedKey);
  const items = useMemo(
    () => media.filter((item) => item.video === selected?.video),
    [media, selected?.video],
  );
  const index = items.findIndex((item) => item.key === selectedKey);
  const scenes = useMemo<SceneSlide[]>(
    () =>
      items.map((item) => ({
        type: "scene",
        sceneId: item.key,
        title: item.title,
        posterSrc: item.thumbnail,
      })),
    [items],
  );
  const images = useMemo<LightboxSlide[]>(
    () =>
      items.map((item) => ({
        src: item.image,
        alt: item.title,
        imageTitle: item.title,
        width: item.width,
        height: item.height,
      })),
    [items],
  );
  if (!selected) return null;
  return selected.video ? (
    <ShareBaseContext value={base}>
      <SceneLightbox
        open
        slides={scenes}
        index={index}
        onClose={onClose}
        slideContent={SharedSceneSlide}
      />
    </ShareBaseContext>
  ) : (
    <ImageLightbox
      open
      slides={images}
      index={index}
      onClose={onClose}
      renderFooter={(slide) => {
        const item = items.find((item) => item.image === slide.src);
        if (!item || (!item.title && !item.download)) return null;
        return (
          <LightboxOverlay position="bottom">
            {item.title && (
              <p className="min-w-0 truncate font-medium">{item.title}</p>
            )}
            <div className="pointer-events-auto self-start">
              <ShareDownload media={item} />
            </div>
          </LightboxOverlay>
        );
      }}
    />
  );
}
