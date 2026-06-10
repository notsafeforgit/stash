import type React from "react";
import { useCallback } from "react";
import { useApolloClient } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { EntityCard } from "./entity-card";
import { useCardAspect } from "src/components/list/card-aspect-context";
import { useStudioContextMenu } from "./use-studio-context-menu";

// StudioDataFragment has scene_count and child_studios; use that for list cards.
type StudioCardStudio = Pick<
  GQL.StudioDataFragment,
  | "id"
  | "name"
  | "image_path"
  | "rating100"
  | "scene_count"
  | "child_studios"
  | "favorite"
  | "tags"
>;

interface StudioCardProps {
  studio: StudioCardStudio;
  isMobile?: boolean;
  selected?: boolean;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
  onEdit?: () => void;
}

export const StudioCard: React.FC<StudioCardProps> = ({
  studio,
  isMobile = false,
  selected,
  onSelectedChanged,
  onEdit,
}) => {
  const cardAspect = useCardAspect();
  const isPortrait = cardAspect === "portrait";

  const counts = [
    studio.scene_count > 0 ? `${studio.scene_count} scenes` : null,
    studio.child_studios.length > 0
      ? `${studio.child_studios.length} sub-studios`
      : null,
  ]
    .filter(Boolean)
    .join(" · ");

  const { menuContent, dialogs, onContextMenuOpen } = useStudioContextMenu({
    studio,
    onSelectedChanged,
    onEdit,
  });
  const contextMenu = menuContent;

  const client = useApolloClient();
  const prefetch = useCallback(() => {
    void client.query({
      query: GQL.FindStudioDocument,
      variables: { id: studio.id },
      fetchPolicy: "cache-first",
    });
  }, [client, studio.id]);

  return (
    <>
      <EntityCard
        id={studio.id}
        href={`/studios/${studio.id}`}
        isMobile={isMobile}
        selected={selected}
        onSelectedChanged={onSelectedChanged}
        contextMenu={contextMenu}
        onContextMenuOpen={onContextMenuOpen}
        prefetch={prefetch}
        className="studio-card"
      >
        <EntityCard.SelectCheckbox />
        <EntityCard.Preview image={studio.image_path} isPortrait={isPortrait} />
        <EntityCard.Body>
          <EntityCard.Title>{studio.name}</EntityCard.Title>
          {counts && <EntityCard.Subtitle>{counts}</EntityCard.Subtitle>}
          <EntityCard.Tags tags={studio.tags} />
          <EntityCard.Rating rating100={studio.rating100} />
        </EntityCard.Body>
      </EntityCard>
      {dialogs}
    </>
  );
};
