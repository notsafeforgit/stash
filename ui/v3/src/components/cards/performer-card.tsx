import type React from "react";
import { useCallback } from "react";
import { useApolloClient } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { getAge } from "src/utils/date";
import { EntityCard } from "./entity-card";
import { useCardAspect } from "src/components/list/card-aspect-context";
import { usePerformerContextMenu } from "./use-performer-context-menu";
import { useIntl } from "react-intl";
import { formatCountry } from "src/components/forms/country-select";

type PerformerCardPerformer = Pick<
  GQL.SlimPerformerDataFragment,
  | "id"
  | "name"
  | "disambiguation"
  | "gender"
  | "image_path"
  | "birthdate"
  | "death_date"
  | "rating100"
  | "favorite"
  | "tags"
  | "country"
>;

interface PerformerCardProps {
  performer: PerformerCardPerformer;
  isMobile?: boolean;
  selected?: boolean;
  onSelectedChanged?: (selected: boolean, shiftKey: boolean) => void;
  onEdit?: () => void;
}

const GENDER_LABEL: Record<string, string> = {
  [GQL.GenderEnum.Male]: "♂",
  [GQL.GenderEnum.Female]: "♀",
  [GQL.GenderEnum.TransgenderMale]: "⚧♂",
  [GQL.GenderEnum.TransgenderFemale]: "⚧♀",
  [GQL.GenderEnum.Intersex]: "⚥",
  [GQL.GenderEnum.NonBinary]: "⚧",
};

export const PerformerCard: React.FC<PerformerCardProps> = ({
  performer,
  isMobile = false,
  selected,
  onSelectedChanged,
  onEdit,
}) => {
  const intl = useIntl();
  const cardAspect = useCardAspect();
  // Performers default to portrait in auto mode; respect forced landscape.
  const isPortrait = cardAspect !== "landscape";

  const age = getAge(performer.birthdate, performer.death_date);
  const genderSymbol = performer.gender
    ? GENDER_LABEL[performer.gender]
    : undefined;

  const subtitle = [
    genderSymbol,
    age != null ? String(age) : undefined,
    formatCountry(performer.country, intl.locale),
  ]
    .filter(Boolean)
    .join(" · ");

  const displayName = performer.disambiguation
    ? `${performer.name} (${performer.disambiguation})`
    : performer.name;

  const { menuContent, dialogs, onContextMenuOpen } = usePerformerContextMenu({
    performer,
    onSelectedChanged,
    onEdit,
  });
  const contextMenu = menuContent;

  const client = useApolloClient();
  const prefetch = useCallback(() => {
    void client.query({
      query: GQL.FindPerformerDocument,
      variables: { id: performer.id },
      fetchPolicy: "cache-first",
    });
  }, [client, performer.id]);

  return (
    <>
      <EntityCard
        id={performer.id}
        href={`/performers/${performer.id}`}
        isMobile={isMobile}
        selected={selected}
        onSelectedChanged={onSelectedChanged}
        contextMenu={contextMenu}
        onContextMenuOpen={onContextMenuOpen}
        prefetch={prefetch}
        className="performer-card"
      >
        <EntityCard.SelectCheckbox />
        <EntityCard.Preview
          image={performer.image_path}
          isPortrait={isPortrait}
        />
        <EntityCard.Body>
          <EntityCard.Title>{displayName}</EntityCard.Title>
          {subtitle && <EntityCard.Subtitle>{subtitle}</EntityCard.Subtitle>}
          <EntityCard.Tags tags={performer.tags} />
          <EntityCard.Rating rating100={performer.rating100} />
        </EntityCard.Body>
      </EntityCard>
      {dialogs}
    </>
  );
};
