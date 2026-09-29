import { useState } from "react";
import { useQuery } from "@apollo/client/react";
import * as GQL from "@/core/generated-graphql";
import { useDebouncedValue } from "@/hooks/debounce";
import { useMsg } from "@/hooks/message";
import { Button } from "@/components/ui/button";
import { FieldError } from "@/components/ui/field";
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from "@/components/ui/combobox";

export type PreviewEntityOption = { id: string; label: string; path: string };

export function PluginPreviewEntityPicker({
  id,
  entity,
  value,
  onChange,
  disabled,
}: {
  id: string;
  entity: GQL.PluginPreviewEntityV3;
  value: PreviewEntityOption | null;
  onChange: (value: PreviewEntityOption | null) => void;
  disabled: boolean;
}) {
  const msg = useMsg();
  const [search, setSearch] = useState("");
  const query = useDebouncedValue(search, 250);
  const scenes = useQuery(GQL.PluginPreviewScenesDocument, {
    variables: { query },
    skip: entity !== GQL.PluginPreviewEntityV3.Scene,
    fetchPolicy: "no-cache",
  });
  const images = useQuery(GQL.PluginPreviewImagesDocument, {
    variables: { query },
    skip: entity !== GQL.PluginPreviewEntityV3.Image,
    fetchPolicy: "no-cache",
  });
  const result = entity === GQL.PluginPreviewEntityV3.Scene ? scenes : images;
  const loading = result.loading || search !== query;
  const rows =
    entity === GQL.PluginPreviewEntityV3.Scene
      ? scenes.data?.findScenes.scenes.map((item) => ({
          ...item,
          path: item.files[0]?.path ?? "",
        }))
      : images.data?.findImages.images.map((item) => ({
          ...item,
          path: item.visual_files[0]?.path ?? "",
        }));
  const items: PreviewEntityOption[] =
    loading || result.error
      ? []
      : (rows ?? []).map((item) => ({
          id: item.id,
          label: `${item.title || item.path.split(/[\\/]/).pop() || msg("config.plugins.untitled_entity", "Untitled")} (#${item.id})`,
          path: item.path,
        }));
  return (
    <>
      <Combobox<PreviewEntityOption>
        items={items}
        filter={null}
        value={value}
        onValueChange={onChange}
        itemToStringLabel={(item) => item.label}
        itemToStringValue={(item) => item.id}
        isItemEqualToValue={(item, selected) => item.id === selected.id}
        onInputValueChange={(text, details) => {
          if (
            details.reason === "input-change" ||
            details.reason === "input-clear"
          )
            setSearch(text);
        }}
        onOpenChange={(open) => {
          if (open) setSearch("");
        }}
        disabled={disabled}
      >
        <ComboboxInput
          id={id}
          showClear
          placeholder={msg(
            "config.plugins.search_entity",
            "Search by title or file path",
          )}
          aria-busy={loading}
        />
        <ComboboxContent>
          <ComboboxEmpty>
            {loading
              ? msg("config.plugins.searching_entities", "Searching…")
              : result.error
                ? msg(
                    "config.plugins.entity_search_failed",
                    "Could not load items",
                  )
                : msg("config.plugins.no_entities", "No matching items")}
          </ComboboxEmpty>
          <ComboboxList>
            {(item: PreviewEntityOption) => (
              <ComboboxItem key={item.id} value={item}>
                <div className="flex min-w-0 flex-col">
                  <span className="truncate">{item.label}</span>
                  <span className="truncate text-xs text-muted-foreground">
                    {item.path}
                  </span>
                </div>
              </ComboboxItem>
            )}
          </ComboboxList>
        </ComboboxContent>
      </Combobox>
      {result.error && (
        <FieldError role="alert">
          {result.error.message}
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => void result.refetch().catch(() => {})}
          >
            {msg("actions.retry", "Retry")}
          </Button>
        </FieldError>
      )}
    </>
  );
}
