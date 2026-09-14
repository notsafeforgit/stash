import { useState } from "react";
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
  const close = () => {
    setOpen(null);
    setClosed((value) => value + 1);
  };
  return (
    <MockedProvider mocks={[]}>
      <ConfigurationProvider configuration={playerConfiguration}>
        <div className="flex h-full flex-col gap-4 bg-background p-6 text-foreground">
          <h1>Lightbox motion</h1>
          <Button onClick={() => setOpen("image")}>Open images</Button>
          <Button onClick={() => setOpen("scene")}>Open scenes</Button>
          <output data-testid="close-count">{closed}</output>
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
