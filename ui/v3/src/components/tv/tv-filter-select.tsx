import { useQuery } from "@apollo/client/react";
import { FindSavedFiltersDocument } from "@/core/generated-graphql";
import { tvFilterMode } from "@/core/tv/feed-query";
import type { TvFilterChoice, TvSourceMode } from "@/core/tv/settings";
import { TvSelect } from "./tv-select";
import { useMsg } from "@/hooks/message";

export function TvFilterSelect({
  mode,
  value,
  onChange,
  label,
}: {
  mode: TvSourceMode;
  value: TvFilterChoice;
  onChange: (choice: TvFilterChoice) => void;
  label: string;
}) {
  const { data, error } = useQuery(FindSavedFiltersDocument, {
    variables: { mode: tvFilterMode(mode) },
  });
  const msg = useMsg();
  return (
    <div className="flex flex-col gap-1">
      <TvSelect
        label={label}
        value={value.kind === "saved" ? value.id : value.kind}
        options={[
          {
            value: "default",
            label: msg("tv.filter.default", "App default filter"),
          },
          { value: "all", label: msg("tv.filter.all", "All items") },
          ...(data?.findSavedFilters ?? []).map((filter) => ({
            value: filter.id,
            label: filter.name,
          })),
        ]}
        onChange={(next) =>
          onChange(
            next === "default" || next === "all"
              ? { kind: next }
              : { kind: "saved", id: next },
          )
        }
      />
      {error && <p role="alert">{error.message}</p>}
    </div>
  );
}
