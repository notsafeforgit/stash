import type React from "react";
import { useCallback } from "react";
import { useApolloClient } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { EntityCard } from "./entity-card";
import { useCardAspect } from "src/components/list/card-aspect-context";
import { useGroupContextMenu } from "./use-group-context-menu";

type GroupCardGroup = Pick<
  GQL.ListGroupDataFragment,
  | "id"
  | "name"
  | "date"
  | "rating100"
  | "front_image_path"
  | "scene_count"
  | "sub_group_count"
  | "studio"
  | "tags"
>;

interface GroupCardProps {
  group: GroupCardGroup;
  isMobile?: boolean;
  selected?: boolean;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
  onEdit?: () => void;
}

export const GroupCard: React.FC<GroupCardProps> = ({
  group,
  isMobile = false,
  selected,
  onSelectedChanged,
  onEdit,
}) => {
  const cardAspect = useCardAspect();
  const isPortrait = cardAspect === "portrait";

  const counts = [
    group.scene_count > 0 ? `${group.scene_count} scenes` : null,
    group.sub_group_count > 0 ? `${group.sub_group_count} sub-groups` : null,
  ]
    .filter(Boolean)
    .join(" · ");

  const { menuContent, dialogs, onContextMenuOpen } = useGroupContextMenu({
    group,
    onSelectedChanged,
    onEdit,
  });
  const contextMenu = menuContent;

  const client = useApolloClient();
  const prefetch = useCallback(() => {
    void client.query({
      query: GQL.FindGroupDocument,
      variables: { id: group.id },
      fetchPolicy: "cache-first",
    });
  }, [client, group.id]);

  return (
    <>
      <EntityCard
        label={group.name}
        id={group.id}
        href={`/groups/${group.id}`}
        isMobile={isMobile}
        selected={selected}
        onSelectedChanged={onSelectedChanged}
        contextMenu={contextMenu}
        onContextMenuOpen={onContextMenuOpen}
        prefetch={prefetch}
        className="group-card"
      >
        <EntityCard.SelectCheckbox />
        <EntityCard.Preview
          image={group.front_image_path}
          isPortrait={isPortrait}
        />
        <EntityCard.Body>
          <EntityCard.Title>{group.name}</EntityCard.Title>
          {(group.date || group.studio || counts) && (
            <EntityCard.Subtitle>
              {[group.date, group.studio?.name, counts]
                .filter(Boolean)
                .join(" · ")}
            </EntityCard.Subtitle>
          )}
          <EntityCard.Tags tags={group.tags} />
          <EntityCard.Rating rating100={group.rating100} />
        </EntityCard.Body>
      </EntityCard>
      {dialogs}
    </>
  );
};
