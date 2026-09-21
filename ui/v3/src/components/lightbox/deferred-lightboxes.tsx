import { deferredOverlay } from "@/components/shared/deferred-overlay";

export const Lightbox = deferredOverlay(
  async () => ({ default: (await import("./lightbox")).Lightbox }),
  (props) => ({ open: props.open, onClose: props.onClose }),
);

export const SceneLightbox = deferredOverlay(
  async () => ({ default: (await import("./scene-lightbox")).SceneLightbox }),
  (props) => ({ open: props.open, onClose: props.onClose }),
);
