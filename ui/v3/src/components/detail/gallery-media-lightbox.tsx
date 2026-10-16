import { useGalleryMediaPages } from "./gallery-media";
import GalleryMediaPlayback from "./gallery-media-playback";

// Mounted only after a list card opens its viewer. The shared page reader
// cancels requests on close and fences subsequent pages against album changes.
export default function GalleryMediaLightbox({
  id,
  onClose,
}: {
  id: string;
  onClose: () => void;
}) {
  const result = useGalleryMediaPages(id);
  return (
    <GalleryMediaPlayback
      loop
      data={result.data}
      index={0}
      signature={result.data?.signature ?? ""}
      busy={result.busy}
      error={result.error}
      onMore={result.data ? result.more : async () => result.reload()}
      onClose={onClose}
    />
  );
}
