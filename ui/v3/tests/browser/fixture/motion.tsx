import { useState } from "react";
import { flushSync } from "react-dom";
import { MockedProvider } from "@apollo/client/testing/react";
import { Button } from "@/components/ui/button";
import { ConfigurationProvider } from "@/hooks/config";
import { Lightbox, type LightboxSlide } from "@/components/lightbox/lightbox";
import {
  SceneLightbox,
  type SceneSlide,
} from "@/components/lightbox/scene-lightbox";
import { playerConfiguration } from "./player-configuration";
import { MediaDetailLayout } from "@/components/detail/media-detail-layout";
import { EntityCard } from "@/components/cards/entity-card";
import { entityDestination } from "@/core/navigation";

export function MotionViewerFixture() {
  const [focused, setFocused] = useState(false);
  return (
    <MediaDetailLayout
      title="Viewer motion"
      primaryFocusMode={focused}
      onClosePrimaryFocus={() => setFocused(false)}
      mobilePageScroll
      tabs={[{ id: "details", label: "Details", content: <p>Details</p> }]}
      primaryContent={
        <div
          data-testid="retained-viewer"
          className="flex h-64 items-center justify-center text-white"
        >
          <Button onClick={() => setFocused(true)}>Open viewer</Button>
        </div>
      }
    />
  );
}

const images: LightboxSlide[] = ["#356bd4", "#ad3b81"].map((color, index) => ({
  src: `data:image/svg+xml,${encodeURIComponent(`<svg xmlns="http://www.w3.org/2000/svg" width="800" height="600"><rect width="800" height="600" fill="${color}"/><circle cx="400" cy="300" r="120" fill="white"/></svg>`)}`,
  alt: `Example image ${index + 1}`,
  width: 800,
  height: 600,
}));
// Exercise the real scene lightbox and its pending Close control without media
// decoding: Linux WebKit's video driver is separate from this motion regression.
const scenes: SceneSlide[] = ["1", "2"].map((sceneId) => ({
  type: "scene",
  sceneId,
  loading: true,
}));

export function MotionFixture() {
  const [open, setOpen] = useState<"image" | "scene" | null>(null);
  const [closed, setClosed] = useState(0);
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState(false);
  const show = (kind: "image" | "scene") => {
    flushSync(() => setOpen(kind));
    if (new URLSearchParams(location.search).has("busy")) {
      // A player/query can occupy the first frame after the portal mounts.
      // The entrance must still be visible once the main thread is available.
      requestAnimationFrame(() => {
        const portal = document.querySelector(".yarl__portal");
        portal?.setAttribute("data-first-frame-work", "running");
        if (portal) getComputedStyle(portal).opacity;
        const until = performance.now() + 350;
        while (performance.now() < until) {
          /* intentional startup work */
        }
        portal?.setAttribute("data-first-frame-work", "done");
      });
    }
  };
  const close = () => {
    setOpen(null);
    setClosed((value) => value + 1);
  };
  return (
    <MockedProvider mocks={[]}>
      <ConfigurationProvider configuration={playerConfiguration}>
        <div className="flex h-full flex-col gap-4 bg-background p-6 text-foreground">
          <h1>Lightbox motion</h1>
          <Button onClick={() => show("image")}>Open images</Button>
          <Button onClick={() => show("scene")}>Open scenes</Button>
          <Button onClick={() => setSelecting((value) => !value)}>
            Select cards
          </Button>
          <div
            className="grid grid-cols-2 gap-4"
            data-selecting={selecting || undefined}
          >
            {(["image", "scene"] as const).map((kind, index) => (
              <EntityCard
                key={kind}
                id={kind}
                label={`Example ${kind}`}
                destination={entityDestination.performer("1")}
                selected={selected}
                onSelectedChanged={setSelected}
                onPreviewClick={() => show(kind)}
              >
                <EntityCard.SelectCheckbox />
                <EntityCard.Preview image={images[index]?.src} />
                <EntityCard.Body>
                  <EntityCard.Title>Example {kind}</EntityCard.Title>
                  <Button onClick={() => setSelected((value) => !value)}>
                    Card action
                  </Button>
                </EntityCard.Body>
              </EntityCard>
            ))}
          </div>
          <output data-testid="close-count">{closed}</output>
          <output data-testid="selection">{String(selected)}</output>
        </div>
        <Lightbox open={open === "image"} onClose={close} slides={images} />
        <SceneLightbox
          open={open === "scene"}
          onClose={close}
          slides={scenes}
          finite
        />
      </ConfigurationProvider>
    </MockedProvider>
  );
}
