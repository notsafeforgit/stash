import { GalleryMedia } from "@/components/detail/gallery-media";
import { useQuery } from "@apollo/client/react";
import {
  ConfigurationDocument,
  FindGalleryCoverDocument,
} from "@/core/generated-graphql";
import { ConfigurationProvider } from "@/hooks/config";
import { sourceTrackingConfiguration } from "./player-configuration";
import { Toaster } from "@/components/ui/sonner";
import { galleryCover } from "@/lib/gallery-utils";
import { AssociationFixtureProvider } from "./association-provider";
import { EntityListPage } from "@/components/list/entity-list-page";
import { useGalleryListConfig } from "@/components/list/entity-list-configs";
import { View } from "@/components/list/views";

const onEdit = () => {};

function GalleryList() {
  const { config, lightboxElement, lightboxOpen } =
    useGalleryListConfig(onEdit);
  return (
    <>
      <EntityListPage
        config={config}
        view={View.Galleries}
        keyboardShortcutsDisabled={lightboxOpen}
      />
      {lightboxElement}
    </>
  );
}

export function GalleryListFixture() {
  return (
    <AssociationFixtureProvider>
      <div data-app-viewport className="flex h-dvh flex-col overflow-hidden">
        {new URLSearchParams(location.search).has("sequence") ? (
          <GalleryListWithSettings />
        ) : (
          <GalleryList />
        )}
        <Toaster />
      </div>
    </AssociationFixtureProvider>
  );
}

function GalleryListWithSettings() {
  const { data } = useQuery(ConfigurationDocument);
  return (
    <ConfigurationProvider
      configuration={data?.configuration ?? sourceTrackingConfiguration}
    >
      <GalleryList />
    </ConfigurationProvider>
  );
}

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
