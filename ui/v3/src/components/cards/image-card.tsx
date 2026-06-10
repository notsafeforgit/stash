import type React from "react";
import { useCallback, useState } from "react";
import { useApolloClient } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { imageTitle } from "src/core/files";
import { EntityCard } from "./entity-card";
import { useMobileGridCols } from "src/components/list/mobile-grid-context";
import { useCardLayout } from "src/components/list/card-layout-context";
import { useZoomIndex } from "src/components/list/zoom-index-context";
import { useCardAspect } from "src/components/list/card-aspect-context";
import { Lightbox } from "src/components/lightbox";
import { useImageContextMenu } from "./use-image-context-menu";

type ImageCardImage = Pick<
  GQL.SlimImageDataFragment,
  | "id"
  | "title"
  | "date"
  | "details"
  | "rating100"
  | "organized"
  | "o_counter"
  | "paths"
  | "studio"
  | "tags"
  | "performers"
  | "visual_files"
>;

interface ImageCardProps {
  image: ImageCardImage;
  isMobile?: boolean;
  selected?: boolean;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
  onPreviewClick?: () => void;
  onSetGalleryCover?: () => void;
  onSetPerformerImage?: () => void;
  onSetStudioImage?: () => void;
  onSetTagImage?: () => void;
  onEdit?: () => void;
  hidePerformers?: boolean;
}

export const ImageCard: React.FC<ImageCardProps> = ({
  image,
  isMobile = false,
  selected,
  onSelectedChanged,
  onPreviewClick: externalPreviewClick,
  onSetGalleryCover,
  onSetPerformerImage,
  onSetStudioImage,
  onSetTagImage,
  onEdit,
  hidePerformers = false,
}) => {
  const [lightboxOpen, setLightboxOpen] = useState(false);

  const file = image.visual_files[0];

  const cardAspect = useCardAspect();
  const fileIsPortrait = file ? (file.height ?? 0) > (file.width ?? 0) : false;
  const isPortrait =
    cardAspect === "portrait"
      ? true
      : cardAspect === "landscape"
        ? false
        : fileIsPortrait;
  const mobileGridCols = useMobileGridCols();
  const cardLayout = useCardLayout();
  const zoomIndex = useZoomIndex();
  const showDetails =
    cardLayout === "details" ||
    (isMobile && mobileGridCols === 1) ||
    (!isMobile && cardLayout === "grid" && zoomIndex <= 1);
  const showExtras = showDetails || !isMobile;

  const lightboxSrc =
    image.paths.image ?? image.paths.preview ?? image.paths.thumbnail ?? "";

  // If a parent-level handler is provided (list lightbox), use it; otherwise
  // fall back to the per-card single-image lightbox.
  const handlePreviewClick =
    externalPreviewClick ?? (() => setLightboxOpen(true));

  const { menuContent, dialogs, onContextMenuOpen } = useImageContextMenu({
    image,
    onSelectedChanged,
    onEdit,
    onSetGalleryCover,
    onSetPerformerImage,
    onSetStudioImage,
    onSetTagImage,
  });
  const contextMenu = menuContent;

  const client = useApolloClient();
  const prefetch = useCallback(() => {
    void client.query({
      query: GQL.FindImageDocument,
      variables: { id: image.id },
      fetchPolicy: "cache-first",
    });
  }, [client, image.id]);

  return (
    <>
      <EntityCard
        id={image.id}
        href={`/images/${image.id}`}
        isMobile={isMobile}
        selected={selected}
        onSelectedChanged={onSelectedChanged}
        onPreviewClick={handlePreviewClick}
        contextMenu={contextMenu}
        onContextMenuOpen={onContextMenuOpen}
        prefetch={prefetch}
        className="image-card"
      >
        <EntityCard.SelectCheckbox />
        <EntityCard.Preview
          image={image.paths.thumbnail}
          isPortrait={isPortrait}
          naturalIsPortrait={
            file?.width && file?.height ? file.height > file.width : undefined
          }
          organized={image.organized}
          oCounter={image.o_counter}
        />
        <EntityCard.Body>
          <EntityCard.Title>{imageTitle(image)}</EntityCard.Title>
          {showDetails && image.details && (
            <EntityCard.Subtitle>{image.details}</EntityCard.Subtitle>
          )}
          {(image.date || image.studio) && (
            <EntityCard.Subtitle noTooltip>
              {[image.date, image.studio?.name].filter(Boolean).join(" · ")}
            </EntityCard.Subtitle>
          )}
          {showExtras && !hidePerformers && (
            <EntityCard.Performers performers={image.performers} />
          )}
          {showExtras && <EntityCard.Tags tags={image.tags} />}
          <EntityCard.Rating rating100={image.rating100} />
        </EntityCard.Body>
      </EntityCard>

      {!externalPreviewClick && lightboxSrc && (
        <Lightbox
          open={lightboxOpen}
          onClose={() => setLightboxOpen(false)}
          slides={[
            {
              src: lightboxSrc,
              alt: imageTitle(image),
              imageId: image.id,
              imageTitle: imageTitle(image),
            },
          ]}
        />
      )}
      {dialogs}
    </>
  );
};
