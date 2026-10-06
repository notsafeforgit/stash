import { useEffect, useState } from "react";
import { useQuery } from "@apollo/client/react";
import { useMsg } from "@/hooks/message";
import { FindGalleriesForSelectDocument } from "@/core/generated-graphql";
import {
  PolicySamplePicker,
  type PolicySampleChoice,
} from "../metadata-policy/sample-picker";
import {
  Combobox,
  ComboboxInput,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxList,
  ComboboxItem,
} from "@/components/ui/combobox";

type PickerProps = {
  id: string;
  disabled: boolean;
  value: PolicySampleChoice | null;
  onChange: (value: PolicySampleChoice | null) => void;
};
export function AssociationPicker({
  kind,
  ...props
}: PickerProps & { kind: "scene" | "image" | "gallery" }) {
  return kind === "gallery" ? (
    <GalleryPicker {...props} />
  ) : (
    <PolicySamplePicker kind={kind} {...props} />
  );
}
function GalleryPicker({ id, disabled, value, onChange }: PickerProps) {
  const msg = useMsg();
  const [query, setQuery] = useState(""),
    [debounced, setDebounced] = useState("");
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(query), 250);
    return () => clearTimeout(timer);
  }, [query]);
  const result = useQuery(FindGalleriesForSelectDocument, {
    variables: { filter: { q: debounced, per_page: 25, page: 1 } },
    skip: !debounced,
    fetchPolicy: "no-cache",
  });
  const busy = result.loading || query !== debounced;
  const rows: PolicySampleChoice[] =
    busy || result.error || !debounced
      ? []
      : (result.data?.findGalleries.galleries ?? []).map((item) => ({
          id: item.id,
          label: `${item.title || msg("metadata_policy.untitled", "Untitled")} (#${item.id})`,
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
        disabled={disabled}
        showClear
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
