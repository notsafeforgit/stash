import { useEffect, useState } from "react";
import { useIntl } from "react-intl";
import { useLazyQuery } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import type { EntityOption } from "src/components/forms/async-entity-select";
import {
  type ScrapedItemResolution,
  ScrapedItemRow,
  defaultItemResolution,
} from "./scraped-item-row";

// Re-exported under historical names so existing call sites
// (performer-scrape-merge-dialog) keep working unchanged.
export type ScrapedTagResolution = ScrapedItemResolution;
export const defaultResolution = defaultItemResolution;

interface ScrapedTagRowProps {
  scraped: GQL.ScrapedSceneTagDataFragment;
  value: ScrapedTagResolution;
  onChange: (next: ScrapedTagResolution) => void;
}

export function ScrapedTagRow({
  scraped,
  value,
  onChange,
}: ScrapedTagRowProps) {
  const intl = useIntl();
  const [tagOptions, setTagOptions] = useState<EntityOption[]>([]);
  const [searchTags, { data, loading }] = useLazyQuery(GQL.FindTagsDocument);

  useEffect(() => {
    if (data) {
      setTagOptions(
        data.findTags.tags.map((t) => ({ id: t.id, name: t.name })),
      );
    }
  }, [data]);

  return (
    <ScrapedItemRow
      scraped={scraped}
      value={value}
      onChange={onChange}
      searchOptions={tagOptions}
      onSearch={(q) =>
        searchTags({ variables: { filter: { q, per_page: 20 } } })
      }
      searching={loading}
      labels={{
        useExisting: intl.formatMessage({
          id: "scrape.use_existing_tag",
          defaultMessage: "Pick existing tag",
        }),
        createNew: intl.formatMessage({
          id: "scrape.create_new_tag",
          defaultMessage: "Create new tag",
        }),
        skip: intl.formatMessage({
          id: "scrape.skip_tag",
          defaultMessage: "Skip",
        }),
        willCreate: (name) =>
          intl.formatMessage(
            {
              id: "scrape.will_create_tag",
              defaultMessage: "Will create tag “{name}”",
            },
            { name },
          ),
      }}
    />
  );
}
