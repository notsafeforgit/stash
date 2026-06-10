import { useEffect, useState } from "react";
import { useLazyQuery } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import {
  EntitySingleSelect,
  type EntityOption,
} from "src/components/forms/async-entity-select";

interface IProps {
  value: string | null | undefined;
  onChange: (id: string | null) => void;
  placeholder?: string;
  disabled?: boolean;
}

/**
 * Standalone async tag picker for spots where only a tag ID is stored (no
 * embedded tag name) — e.g. identify dialog's skip-multiple-match-tag,
 * skip-single-name-performer-tag. Resolves the ID to a display name once on
 * mount so the input shows the chosen tag's name rather than its raw ID.
 */
export function TagIdPicker({
  value,
  onChange,
  placeholder,
  disabled,
}: IProps) {
  const [option, setOption] = useState<EntityOption | null>(null);
  const [options, setOptions] = useState<EntityOption[]>([]);
  const [searchTags, { data: searchData, loading: searchLoading }] =
    useLazyQuery(GQL.FindTagsDocument);
  const [fetchTag] = useLazyQuery(GQL.FindTagDocument);

  // Resolve the stored ID once to a display name.
  useEffect(() => {
    if (!value) {
      setOption(null);
      return;
    }
    if (option?.id === value) return;
    void fetchTag({ variables: { id: value } }).then((res) => {
      const t = res.data?.findTag;
      if (t) setOption({ id: t.id, name: t.name });
    });
  }, [value, option?.id, fetchTag]);

  useEffect(() => {
    if (searchData) {
      setOptions(
        searchData.findTags.tags.map((t) => ({ id: t.id, name: t.name })),
      );
    }
  }, [searchData]);

  function onSearch(q: string) {
    void searchTags({
      variables: { filter: { q, per_page: 25 } },
    });
  }

  return (
    <EntitySingleSelect
      value={option}
      onChange={(v) => {
        setOption(v);
        onChange(v?.id ?? null);
      }}
      options={options}
      onSearch={onSearch}
      loading={searchLoading}
      placeholder={placeholder}
      disabled={disabled}
    />
  );
}
