import { useEffect, useState } from "react";
import { useQuery } from "@apollo/client/react";
import { useMsg } from "@/hooks/message";
import * as GQL from "@/core/generated-graphql";
import type { PolicyKind } from "@/core/native-archive/metadata-policy-api";
import {
  Combobox,
  ComboboxInput,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxList,
  ComboboxItem,
} from "@/components/ui/combobox";

export type PolicySampleChoice = { id: string; label: string };

export function PolicySamplePicker({
  kind,
  id,
  disabled,
  value,
  onChange,
}: {
  kind: PolicyKind;
  id: string;
  disabled: boolean;
  value: PolicySampleChoice | null;
  onChange: (choice: PolicySampleChoice | null) => void;
}) {
  const msg = useMsg();
  const [query, setQuery] = useState("");
  const [debounced, setDebounced] = useState("");
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(query), 250);
    return () => clearTimeout(timer);
  }, [query]);
  const variables = { filter: { q: debounced, per_page: 25, page: 1 } };
  const scenes = useQuery(GQL.FindScenesForSelectDocument, {
    variables,
    skip: kind !== "scene" || !debounced,
    fetchPolicy: "no-cache",
  });
  const images = useQuery(GQL.FindImagesDocument, {
    variables,
    skip: kind !== "image" || !debounced,
    fetchPolicy: "no-cache",
  });
  const result = kind === "scene" ? scenes : images;
  const busy = result.loading || debounced !== query;
  const rows: PolicySampleChoice[] =
    busy || result.error || !debounced
      ? []
      : kind === "scene"
        ? (scenes.data?.findScenes.scenes ?? []).map((scene) => ({
            id: scene.id,
            label: `${scene.title || scene.files[0]?.path || msg("metadata_policy.untitled", "Untitled")} (#${scene.id})`,
          }))
        : (images.data?.findImages.images ?? []).map((image) => ({
            id: image.id,
            label: `${image.title || image.visual_files[0]?.path || msg("metadata_policy.untitled", "Untitled")} (#${image.id})`,
          }));
  return (
    <Combobox<PolicySampleChoice>
      items={rows}
      filter={null}
      value={value}
      disabled={disabled}
      itemToStringLabel={(item) => item.label}
      itemToStringValue={(item) => item.id}
      isItemEqualToValue={(a, b) => a.id === b.id}
      onValueChange={onChange}
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
        showClear
        disabled={disabled}
        aria-busy={busy}
        maxLength={256}
      />
      <ComboboxContent>
        <ComboboxEmpty>
          {busy
            ? msg("archive_review.searching", "Searching…")
            : result.error
              ? msg("archive_review.search_failed", "Search failed. Try again.")
              : msg("archive_review.no_matches", "No matches")}
        </ComboboxEmpty>
        <ComboboxList>
          {(item: PolicySampleChoice) => (
            <ComboboxItem key={item.id} value={item}>
              <span className="wrap-anywhere">{item.label}</span>
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
}
