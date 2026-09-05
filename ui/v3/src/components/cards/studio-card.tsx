import type React from "react";
import { useCallback } from "react";
import { useApolloClient } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { EntityCard } from "./entity-card";
import { useCardAspect } from "src/components/list/card-aspect-context";
import { useStudioContextMenu } from "./use-studio-context-menu";
import { useConfigurationContextOptional } from "src/hooks/config";

// StudioDataFragment has scene_count and child_studios; use that for list cards.
type StudioCardStudio = Pick<
  GQL.StudioDataFragment,
  | "id"
  | "name"
  | "image_path"
  | "rating100"
  | "scene_count"
  | "scene_count_all"
  | "performer_count"
  | "performer_count_all"
  | "child_studios"
  | "o_counter"
  | "o_counter_all"
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
  const showChildStudioContent =
    useConfigurationContextOptional()?.configuration.ui
      .showChildStudioContent ?? false;
  const sceneCount = showChildStudioContent
    ? studio.scene_count_all
    : studio.scene_count;
  const performerCount = showChildStudioContent
    ? studio.performer_count_all
    : studio.performer_count;
  const oCounter = showChildStudioContent
    ? studio.o_counter_all
    : studio.o_counter;

  const counts = [
    sceneCount > 0 ? `${sceneCount} scenes` : null,
    performerCount > 0 ? `${performerCount} performers` : null,
    oCounter != null && oCounter > 0 ? `${oCounter} O` : null,
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
        label={studio.name}
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
        <EntityCard.Preview
          image={studio.image_path}
          isPortrait={isPortrait}
          fit="contain"
        />
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
