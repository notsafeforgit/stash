import { GalleryMedia } from "@/components/detail/gallery-media";
import { AssociationFixtureProvider } from "./association-provider";

export function GalleryMediaFixture() {
  return (
    <AssociationFixtureProvider>
      <GalleryMedia id="12" />
    </AssociationFixtureProvider>
  );
}
