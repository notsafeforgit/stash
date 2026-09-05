import type React from "react";
import { useCallback } from "react";
import { useApolloClient } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { objectTitle } from "src/core/files";
import { EntityCard } from "./entity-card";
import { useCardLayout } from "src/components/list/card-layout-context";
import { useCardAspect } from "src/components/list/card-aspect-context";

type MarkerCardMarker = Pick<
  GQL.SceneMarkerDataFragment,
  | "id"
  | "title"
  | "seconds"
  | "end_seconds"
  | "stream"
  | "preview"
  | "screenshot"
  | "scene"
  | "primary_tag"
  | "tags"
>;

interface MarkerCardProps {
  marker: MarkerCardMarker;
  isMobile?: boolean;
  selected?: boolean;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
  /** When provided, clicking the preview opens a lightbox instead of
   *  navigating to the scene. The card body click still navigates to
   *  the scene's markers tab at `t=<seconds>` via the stretched anchor. */
  onPreviewClick?: () => void;
}

function formatTimestamp(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  if (h > 0) {
    return `${h}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
  }
  return `${m}:${String(s).padStart(2, "0")}`;
}

export const MarkerCard: React.FC<MarkerCardProps> = ({
  marker,
  isMobile = false,
  selected,
  onSelectedChanged,
  onPreviewClick,
}) => {
  const sceneTitle = objectTitle(marker.scene);
  const title =
    marker.title ||
    (sceneTitle
      ? `${sceneTitle} — ${marker.primary_tag.name}`
      : marker.primary_tag.name);
  const timestamp = formatTimestamp(marker.seconds);
  const file = marker.scene.files[0];
  const resolution =
    file?.width && file?.height
      ? { width: file.width, height: file.height }
      : null;
  const cardLayout = useCardLayout();
  // Mirror scene/image cards: when the surrounding context forces an
  // aspect (e.g. the homepage carousel pillarboxes everything except
  // galleries to portrait), pass the corresponding `isPortrait` flag
  // through to `EntityCard.Preview` so the scene-screenshot frame
  // matches the row's aspect.
  const cardAspect = useCardAspect();
  const fileIsPortrait = file ? (file.height ?? 0) > (file.width ?? 0) : false;
  const isPortrait =
    cardAspect === "portrait"
      ? true
      : cardAspect === "landscape"
        ? false
        : fileIsPortrait;

  const client = useApolloClient();
  const prefetch = useCallback(() => {
    void client.query({
      query: GQL.FindSceneDocument,
      variables: { id: marker.scene.id },
      fetchPolicy: "cache-first",
    });
  }, [client, marker.scene.id]);

  return (
    <EntityCard
      label={title}
      id={marker.id}
      href={`/scenes/${marker.scene.id}?tab=markers&t=${marker.seconds}`}
      isMobile={isMobile}
      selected={selected}
      onSelectedChanged={onSelectedChanged}
      onPreviewClick={onPreviewClick}
      prefetch={prefetch}
      className="marker-card"
    >
      <EntityCard.SelectCheckbox />
      <EntityCard.Preview
        image={marker.screenshot}
        video={marker.stream}
        animated={marker.preview}
        resolution={cardLayout === "wall" ? null : resolution}
        isPortrait={isPortrait}
        naturalIsPortrait={fileIsPortrait}
      >
        {cardLayout === "wall" ? (
          <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/80 via-black/40 to-transparent pt-6 pb-1.5 px-2 pointer-events-none">
            <p className="text-white text-[0.7rem] font-semibold leading-tight truncate">
              {title}
            </p>
            <p className="text-white/80 text-[0.65rem] leading-tight mt-0.5">
              {timestamp}
            </p>
          </div>
        ) : (
          <span className="entity-card-badge entity-card-badge-timestamp">
            {timestamp}
          </span>
        )}
      </EntityCard.Preview>
      <EntityCard.Body>
        <EntityCard.Title>{title}</EntityCard.Title>
        <EntityCard.Subtitle>{marker.primary_tag.name}</EntityCard.Subtitle>
        <EntityCard.Tags tags={marker.tags} />
      </EntityCard.Body>
    </EntityCard>
  );
};
