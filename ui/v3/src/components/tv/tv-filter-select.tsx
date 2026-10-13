import { useQuery } from "@apollo/client/react";
import { FindSavedFiltersDocument } from "@/core/generated-graphql";
import { tvFilterMode } from "@/core/tv/feed-query";
import type {
  TvFilterChoice,
  TvMode,
  TvSavedFilterChoice,
} from "@/core/tv/settings";
import { TvSelect } from "./tv-select";
import { useMsg } from "@/hooks/message";

type TvFilterSelectProps = {
  mode: TvMode;
  label: string;
} & (
  | {
      optional: true;
      value: TvSavedFilterChoice | null;
      onChange: (choice: TvSavedFilterChoice | null) => void;
    }
  | {
      optional?: false;
      value: TvFilterChoice;
      onChange: (choice: TvFilterChoice) => void;
    }
);

export function TvFilterSelect(props: TvFilterSelectProps) {
  const { mode, value, label } = props;
  const { data, error } = useQuery(FindSavedFiltersDocument, {
    variables: { mode: tvFilterMode(mode) },
  });
  const msg = useMsg();
  return (
    <div className="flex flex-col gap-1">
      <TvSelect
        label={label}
        value={value?.kind === "saved" ? value.id : (value?.kind ?? "none")}
        options={[
          ...(props.optional
            ? [
                {
                  value: "none",
                  label: msg("tv.filter.none", "None — markers only"),
                },
              ]
            : [
                {
                  value: "default",
                  label: msg("tv.filter.default", "App default filter"),
                },
                { value: "all", label: msg("tv.filter.all", "All items") },
              ]),
          ...(data?.findSavedFilters ?? []).map((filter) => ({
            value: filter.id,
            label: filter.name,
          })),
        ]}
        onChange={(next) => {
          if (props.optional)
            props.onChange(
              next === "none" ? null : { kind: "saved", id: next },
            );
          else
            props.onChange(
              next === "default" || next === "all"
                ? { kind: next }
                : { kind: "saved", id: next },
            );
        }}
      />
      {error && <p role="alert">{error.message}</p>}
    </div>
  );
}
