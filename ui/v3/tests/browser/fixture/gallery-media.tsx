import { GalleryMedia } from "@/components/detail/gallery-media";
import { useQuery } from "@apollo/client/react";
import { FindGalleryCoverDocument } from "@/core/generated-graphql";
import { Toaster } from "@/components/ui/sonner";
import { galleryCover } from "@/lib/gallery-utils";
import { AssociationFixtureProvider } from "./association-provider";

function CoverPreview() {
  const { data } = useQuery(FindGalleryCoverDocument, {
    variables: { id: "12" },
  });
  return data?.findGallery ? (
    <img
      className="size-16"
      src={galleryCover(data.findGallery)}
      alt="Gallery cover"
    />
  ) : null;
}

export function GalleryMediaFixture() {
  return (
    <AssociationFixtureProvider>
      <CoverPreview />
      <GalleryMedia id="12" />
      <Toaster />
    </AssociationFixtureProvider>
  );
}
