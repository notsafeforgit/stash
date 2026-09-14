import { useLazyQuery, useQuery, skipToken } from "@apollo/client/react";
import { FindTagsForSelectDocument } from "@/core/generated-graphql";
import {
  EntityMultiSelect,
  EntitySingleSelect,
  type EntityOption,
} from "@/components/forms/async-entity-select";

export function TvTagPicker({
  value,
  onChange,
  label,
}: {
  value: EntityOption | null;
  onChange: (tag: EntityOption | null) => void;
  label: string;
}) {
  const [search, { data, loading }] = useLazyQuery(FindTagsForSelectDocument);
  const selected = useQuery(
    FindTagsForSelectDocument,
    value
      ? {
          variables: { ids: [value.id], filter: { per_page: 1 } },
          fetchPolicy: "cache-first",
        }
      : skipToken,
  );
  return (
    <EntitySingleSelect
      value={
        selected.data?.findTags.tags.find((tag) => tag.id === value?.id) ??
        value
      }
      onChange={onChange}
      options={data?.findTags.tags ?? []}
      loading={loading}
      placeholder={label}
      onSearch={(q) => {
        void search({ variables: { filter: { q, per_page: 30 } } });
      }}
    />
  );
}
export function TvTagsPicker({
  value,
  onChange,
  label,
}: {
  value: EntityOption[];
  onChange: (tags: EntityOption[]) => void;
  label: string;
}) {
  const [search, { data, loading }] = useLazyQuery(FindTagsForSelectDocument);
  const selected = useQuery(
    FindTagsForSelectDocument,
    value.length
      ? {
          variables: {
            ids: value.map((tag) => tag.id),
            filter: { per_page: value.length },
          },
          fetchPolicy: "cache-first",
        }
      : skipToken,
  );
  return (
    <EntityMultiSelect
      value={value.map(
        (tag) =>
          selected.data?.findTags.tags.find((item) => item.id === tag.id) ??
          tag,
      )}
      onChange={onChange}
      options={data?.findTags.tags ?? []}
      loading={loading}
      placeholder={label}
      onSearch={(q) => {
        void search({ variables: { filter: { q, per_page: 30 } } });
      }}
    />
  );
}
