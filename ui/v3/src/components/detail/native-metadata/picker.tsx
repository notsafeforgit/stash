import { useEffect, useState } from "react";
import { useQuery } from "@apollo/client/react";
import { useMsg } from "@/hooks/message";
import * as GQL from "@/core/generated-graphql";
import type { NativeIdentity } from "@/core/native-archive/metadata-review-api";
import {
  Combobox,
  ComboboxInput,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxList,
  ComboboxItem,
} from "@/components/ui/combobox";

export type SearchChoice = {
  id: string;
  name: string;
  disambiguation?: string | null;
};
export function searchChoiceLabel(item: SearchChoice): string {
  return `${item.name}${item.disambiguation ? ` (${item.disambiguation})` : ""} (#${item.id})`;
}
export function ExistingEntityPicker({
  kind,
  id,
  disabled,
  onChange,
}: {
  kind: NativeIdentity["kind"];
  id: string;
  disabled: boolean;
  onChange: (item: SearchChoice) => void;
}) {
  const msg = useMsg();
  const [query, setQuery] = useState("");
  const [debounced, setDebounced] = useState("");
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(query), 250);
    return () => clearTimeout(timer);
  }, [query]);
  const variables = { filter: { q: debounced, per_page: 25, page: 1 } };
  const performers = useQuery(GQL.FindPerformersForSelectDocument, {
    variables,
    skip: kind !== "performer" || !debounced,
    fetchPolicy: "no-cache",
  });
  const tags = useQuery(GQL.FindTagsForSelectDocument, {
    variables,
    skip: kind !== "tag" || !debounced,
    fetchPolicy: "no-cache",
  });
  const studios = useQuery(GQL.FindStudiosForSelectDocument, {
    variables,
    skip: kind !== "studio" || !debounced,
    fetchPolicy: "no-cache",
  });
  const groups = useQuery(GQL.FindGroupsForSelectDocument, {
    variables,
    skip: kind !== "group" || !debounced,
    fetchPolicy: "no-cache",
  });
  const result =
    kind === "performer"
      ? performers
      : kind === "tag"
        ? tags
        : kind === "studio"
          ? studios
          : groups;
  const loading = result.loading || query !== debounced;
  const candidates: SearchChoice[] =
    loading || result.error || !debounced
      ? []
      : kind === "performer"
        ? (performers.data?.findPerformers.performers ?? [])
        : kind === "tag"
          ? (tags.data?.findTags.tags ?? [])
          : kind === "studio"
            ? (studios.data?.findStudios.studios ?? [])
            : (groups.data?.findGroups.groups ?? []);
  return (
    <Combobox<SearchChoice>
      items={candidates}
      filter={null}
      value={null}
      disabled={disabled}
      itemToStringLabel={searchChoiceLabel}
      itemToStringValue={(item) => item.id}
      onValueChange={(item) => {
        if (item) onChange(item);
      }}
      onInputValueChange={(text, details) => {
        if (
          details.reason === "input-change" ||
          details.reason === "input-clear"
        )
          setQuery(text);
      }}
    >
      <ComboboxInput
        id={id}
        disabled={disabled}
        placeholder={msg(
          "archive_review.search_existing",
          "Search existing library entries",
        )}
        aria-busy={loading}
      />
      <ComboboxContent>
        <ComboboxEmpty>
          {loading
            ? msg("archive_review.searching", "Searching…")
            : result.error
              ? msg("archive_review.search_failed", "Search failed. Try again.")
              : msg("archive_review.no_matches", "No matches")}
        </ComboboxEmpty>
        <ComboboxList>
          {(item: SearchChoice) => (
            <ComboboxItem key={item.id} value={item}>
              {searchChoiceLabel(item)}
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
}
