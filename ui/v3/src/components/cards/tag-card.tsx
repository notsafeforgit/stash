import type React from "react";
import { useCallback } from "react";
import { useApolloClient } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { EntityCard } from "./entity-card";
import { useCardAspect } from "src/components/list/card-aspect-context";
import { useTagContextMenu } from "./use-tag-context-menu";

// TagDataFragment has counts; use that for list cards.
type TagCardTag = Pick<
  GQL.TagDataFragment,
  | "id"
  | "name"
  | "image_path"
  | "scene_count"
  | "scene_marker_count"
  | "image_count"
  | "gallery_count"
  | "performer_count"
  | "studio_count"
  | "group_count"
  | "favorite"
>;

interface TagCardProps {
  tag: TagCardTag;
  isMobile?: boolean;
  selected?: boolean;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
  onEdit?: () => void;
}

export const TagCard: React.FC<TagCardProps> = ({
  tag,
  isMobile = false,
  selected,
  onSelectedChanged,
  onEdit,
}) => {
  const cardAspect = useCardAspect();
  const isPortrait = cardAspect === "portrait";

  const counts = [
    tag.scene_count > 0 ? `${tag.scene_count} scenes` : null,
    tag.scene_marker_count > 0 ? `${tag.scene_marker_count} markers` : null,
    tag.image_count > 0 ? `${tag.image_count} images` : null,
    tag.gallery_count > 0 ? `${tag.gallery_count} galleries` : null,
    tag.performer_count > 0 ? `${tag.performer_count} performers` : null,
    tag.studio_count > 0 ? `${tag.studio_count} studios` : null,
    tag.group_count > 0 ? `${tag.group_count} groups` : null,
  ]
    .filter(Boolean)
    .join(" · ");

  const { menuContent, dialogs, onContextMenuOpen } = useTagContextMenu({
    tag,
    onSelectedChanged,
    onEdit,
  });
  const contextMenu = menuContent;

  const client = useApolloClient();
  const prefetch = useCallback(() => {
    void client.query({
      query: GQL.FindTagDocument,
      variables: { id: tag.id },
      fetchPolicy: "cache-first",
    });
  }, [client, tag.id]);

  return (
    <>
      <EntityCard
        label={tag.name}
        id={tag.id}
        href={`/tags/${tag.id}`}
        isMobile={isMobile}
        selected={selected}
        onSelectedChanged={onSelectedChanged}
        contextMenu={contextMenu}
        onContextMenuOpen={onContextMenuOpen}
        prefetch={prefetch}
        className="tag-card"
      >
        <EntityCard.SelectCheckbox />
        <EntityCard.Preview image={tag.image_path} isPortrait={isPortrait} />
        <EntityCard.Body>
          <EntityCard.Title>{tag.name}</EntityCard.Title>
          {counts && <EntityCard.Subtitle>{counts}</EntityCard.Subtitle>}
        </EntityCard.Body>
      </EntityCard>
      {dialogs}
    </>
  );
};
