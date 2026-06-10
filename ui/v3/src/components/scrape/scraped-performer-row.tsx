import { useEffect, useState } from "react";
import { useIntl } from "react-intl";
import { useLazyQuery } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import type { EntityOption } from "src/components/forms/async-entity-select";
import { type ScrapedItemResolution, ScrapedItemRow } from "./scraped-item-row";

interface ScrapedPerformerRowProps {
  scraped: GQL.ScrapedScenePerformerDataFragment;
  value: ScrapedItemResolution;
  onChange: (next: ScrapedItemResolution) => void;
}

export function ScrapedPerformerRow({
  scraped,
  value,
  onChange,
}: ScrapedPerformerRowProps) {
  const intl = useIntl();
  const [options, setOptions] = useState<EntityOption[]>([]);
  const [search, { data, loading }] = useLazyQuery(GQL.FindPerformersDocument);

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
