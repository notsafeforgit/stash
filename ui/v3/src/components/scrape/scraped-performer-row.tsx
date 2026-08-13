import { useEffect, useState } from "react";
import { useIntl } from "react-intl";
import { useLazyQuery, useQuery } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import type { EntityOption } from "src/components/forms/async-entity-select";
import { type ScrapedItemResolution, ScrapedItemRow } from "./scraped-item-row";
import { getPerformerCollisions } from "./performer-match";
import {
  LocalPerformerPreviewButton,
  PerformerCollisionWarning,
  ScrapedPerformerPreviewButton,
} from "./performer-match-preview";

interface ScrapedPerformerRowProps {
  scraped: GQL.ScrapedScenePerformerDataFragment;
  value: ScrapedItemResolution;
  onChange: (next: ScrapedItemResolution) => void;
  endpoint?: string;
}

export function ScrapedPerformerRow({
  scraped,
  value,
  onChange,
  endpoint,
}: ScrapedPerformerRowProps) {
  const intl = useIntl();
  const [options, setOptions] = useState<EntityOption[]>([]);
  const [search, { data, loading }] = useLazyQuery(GQL.FindPerformersDocument);
  const selectedId = value.kind === "existing" ? value.option.id : "";
  const { data: selectedData } = useQuery(GQL.FindPerformerDocument, {
    variables: { id: selectedId },
    skip: !selectedId,
  });
  const selectedPerformer = selectedData?.findPerformer;
  const collisions = selectedPerformer
    ? getPerformerCollisions(scraped, selectedPerformer, endpoint)
    : [];

  useEffect(() => {
    if (data) {
      setOptions(
        data.findPerformers.performers.map((p) => ({
          id: p.id,
          name: p.disambiguation ? `${p.name} (${p.disambiguation})` : p.name,
        })),
      );
    }
  }, [data]);

  return (
    <ScrapedItemRow
      scraped={scraped}
      subtitle={scraped.disambiguation}
      nameAddon={
        <ScrapedPerformerPreviewButton
          performer={scraped}
          endpoint={endpoint}
        />
      }
      existingAddons={
        selectedPerformer ? (
          <>
            <LocalPerformerPreviewButton performer={selectedPerformer} />
            <PerformerCollisionWarning collisions={collisions} />
          </>
        ) : undefined
      }
      value={value}
      onChange={onChange}
      searchOptions={options}
      onSearch={(q) => search({ variables: { filter: { q, per_page: 20 } } })}
      searching={loading}
      labels={{
        useExisting: intl.formatMessage({
          id: "scrape.use_existing_performer",
          defaultMessage: "Pick existing performer",
        }),
        createNew: intl.formatMessage({
          id: "scrape.create_new_performer",
          defaultMessage: "Create new performer",
        }),
        skip: intl.formatMessage({
          id: "scrape.skip_performer",
          defaultMessage: "Skip",
        }),
        willCreate: (name) =>
          intl.formatMessage(
            {
              id: "scrape.will_create_performer",
              defaultMessage: "Will create performer “{name}”",
            },
            { name },
          ),
      }}
    />
  );
}
