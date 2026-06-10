import { useEffect, useState } from "react";
import { useIntl } from "react-intl";
import { useLazyQuery } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import type { EntityOption } from "src/components/forms/async-entity-select";
import { type ScrapedItemResolution, ScrapedItemRow } from "./scraped-item-row";

interface ScrapedGroupRowProps {
  scraped: GQL.ScrapedSceneGroupDataFragment;
  value: ScrapedItemResolution;
  onChange: (next: ScrapedItemResolution) => void;
}

export function ScrapedGroupRow({
  scraped,
  value,
  onChange,
}: ScrapedGroupRowProps) {
  const intl = useIntl();
  const [options, setOptions] = useState<EntityOption[]>([]);
  const [search, { data, loading }] = useLazyQuery(GQL.FindGroupsDocument);

  useEffect(() => {
    if (data) {
      setOptions(
        data.findGroups.groups.map((g) => ({ id: g.id, name: g.name })),
      );
    }
  }, [data]);

  // Studio + date make it easier to disambiguate same-named groups.
  const subtitle = [scraped.studio?.name, scraped.date]
    .filter(Boolean)
    .join(" · ");

  return (
    <ScrapedItemRow
      scraped={scraped}
      subtitle={subtitle || undefined}
      value={value}
      onChange={onChange}
      searchOptions={options}
      onSearch={(q) => search({ variables: { filter: { q, per_page: 20 } } })}
      searching={loading}
      labels={{
        useExisting: intl.formatMessage({
          id: "scrape.use_existing_group",
          defaultMessage: "Pick existing group",
        }),
        createNew: intl.formatMessage({
          id: "scrape.create_new_group",
          defaultMessage: "Create new group",
        }),
        skip: intl.formatMessage({
          id: "scrape.skip_group",
          defaultMessage: "Skip",
        }),
        willCreate: (name) =>
          intl.formatMessage(
            {
              id: "scrape.will_create_group",
              defaultMessage: "Will create group “{name}”",
            },
            { name },
          ),
      }}
    />
  );
}
