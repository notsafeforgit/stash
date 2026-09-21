import { useState } from "react";
import { useQuery } from "@apollo/client/react";
import { useIntl } from "react-intl";
import { z } from "zod";
import {
  EntityMultiSelect,
  type EntityOption,
} from "@/components/forms/async-entity-select";
import * as GQL from "@/core/generated-graphql";
import { useDebouncedValue } from "@/hooks/debounce";
import { useMsg } from "@/hooks/message";
import { QueryError } from "@/components/query-error";

export const shareSelectionSchema = z.object({
  kind: z.enum(GQL.ShareEntityKind),
  id: z.string().regex(/^[1-9]\d*$/),
  name: z.string(),
});
export type ShareSelection = z.infer<typeof shareSelectionSchema>;

export function shareOption(item: ShareSelection): EntityOption {
  return { id: `${item.kind}:${item.id}`, name: item.name };
}

export function parseShareOption(item: EntityOption): ShareSelection {
  const [kind, id] = item.id.split(":");
  return shareSelectionSchema.parse({ kind, id, name: item.name });
}

export function ShareTargetPicker({
  value,
  onChange,
  disabled,
}: {
  value: ShareSelection[];
  onChange: (items: ShareSelection[]) => void;
  disabled?: boolean;
}) {
  const intl = useIntl();
  const msg = useMsg();
  const [search, setSearch] = useState("");
  const q = useDebouncedValue(search, 250);
  const { data, loading, error, refetch } = useQuery(
    GQL.ShareTargetSearchDocument,
    { variables: { q } },
  );
  const option = (
    kind: GQL.ShareEntityKind,
    id: string,
    title: string | null | undefined,
  ): EntityOption => {
    const entity =
      kind === GQL.ShareEntityKind.Scene
        ? msg("scene", "Scene")
        : kind === GQL.ShareEntityKind.Image
          ? msg("image", "Image")
          : msg("gallery", "Gallery");
    return shareOption({
      kind,
      id,
      name: intl.formatMessage(
        { id: "sharing.target_label", defaultMessage: "{entity}: {title}" },
        { entity, title: title || id },
      ),
    });
  };
  const options = [
    ...(data?.findScenes.scenes.map((item) =>
      option(GQL.ShareEntityKind.Scene, item.id, item.title),
    ) ?? []),
    ...(data?.findImages.images.map((item) =>
      option(GQL.ShareEntityKind.Image, item.id, item.title),
    ) ?? []),
    ...(data?.findGalleries.galleries.map((item) =>
      option(GQL.ShareEntityKind.Gallery, item.id, item.title),
    ) ?? []),
  ];
  return (
    <>
      <EntityMultiSelect
        value={value.map(shareOption)}
        onChange={(items) => onChange(items.map(parseShareOption))}
        options={options}
        onSearch={setSearch}
        loading={loading}
        disabled={disabled}
        placeholder={msg(
          "sharing.search",
          "Search scenes, images and galleries…",
        )}
      />
      {error && <QueryError error={error} retry={refetch} retrying={loading} />}
    </>
  );
}
