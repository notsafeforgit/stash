import { useEffect, useState } from "react";
import { useIntl } from "react-intl";
import { useLazyQuery } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import type { EntityOption } from "src/components/forms/async-entity-select";
import { type ScrapedItemResolution, ScrapedItemRow } from "./scraped-item-row";

interface ScrapedStudioRowProps {
  scraped: GQL.ScrapedSceneStudioDataFragment;
  value: ScrapedItemResolution;
  onChange: (next: ScrapedItemResolution) => void;
}

export function ScrapedStudioRow({
  scraped,
  value,
  onChange,
}: ScrapedStudioRowProps) {
  const intl = useIntl();
  const [options, setOptions] = useState<EntityOption[]>([]);
  const [search, { data, loading }] = useLazyQuery(GQL.FindStudiosDocument);

  useEffect(() => {
    if (data) {
      setOptions(
        data.findStudios.studios.map((s) => ({ id: s.id, name: s.name })),
      );
    }
  }, [data]);

  return (
    <ScrapedItemRow
      scraped={scraped}
      value={value}
      onChange={onChange}
      searchOptions={options}
      onSearch={(q) => search({ variables: { filter: { q, per_page: 20 } } })}
      searching={loading}
      labels={{
        useExisting: intl.formatMessage({
          id: "scrape.use_existing_studio",
          defaultMessage: "Pick existing studio",
        }),
        createNew: intl.formatMessage({
          id: "scrape.create_new_studio",
          defaultMessage: "Create new studio",
        }),
        skip: intl.formatMessage({
          id: "scrape.skip_studio",
          defaultMessage: "Skip",
        }),
        willCreate: (name) =>
          intl.formatMessage(
            {
              id: "scrape.will_create_studio",
              defaultMessage: "Will create studio “{name}”",
            },
            { name },
          ),
      }}
    />
  );
}
