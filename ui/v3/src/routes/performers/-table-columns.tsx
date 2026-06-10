import { useMemo } from "react";
import { useIntl } from "react-intl";
import type { ColumnDef } from "@tanstack/react-table";
import { useMutation } from "@apollo/client/react";
import { Heart } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { getAge } from "src/utils/date";
import { cn } from "src/lib/utils";
import { Button } from "src/components/ui/button";
import {
  selectionColumn,
  thumbnailColumn,
  titleColumn,
  numberColumn,
  ratingColumn,
  tagsColumn,
} from "src/components/list/table-columns";
import { DataTableColumnHeader } from "src/components/list/data-table-column-header";
import { CountryDisplay } from "src/components/forms/country-select";

type PerformerItem =
  GQL.FindPerformersQuery["findPerformers"]["performers"][number];

function FavoriteCell({ performer }: { performer: PerformerItem }) {
  const intl = useIntl();
  const [updatePerformer] = useMutation(GQL.PerformerUpdateDocument);
  const isFavorite = performer.favorite;
  return (
    <Button
      variant="ghost"
      size="icon-sm"
      aria-pressed={isFavorite}
      title={intl.formatMessage(
        isFavorite
          ? {
              id: "actions.unfavorite",
              defaultMessage: "Remove from favorites",
            }
          : { id: "actions.favorite", defaultMessage: "Add to favorites" },
      )}
      onClick={(e) => {
        e.stopPropagation();
        updatePerformer({
          variables: { input: { id: performer.id, favorite: !isFavorite } },
        });
      }}
    >
      <Heart
        className={cn(
          "size-4",
          isFavorite
            ? "fill-rose-500 text-rose-500"
            : "text-muted-foreground hover:text-foreground",
        )}
      />
    </Button>
  );
}

export function usePerformerTableColumns(): ColumnDef<PerformerItem>[] {
  const intl = useIntl();
  return useMemo(
    () => [
      selectionColumn<PerformerItem>(),

      thumbnailColumn<PerformerItem>(
        (p) => p.image_path,
        (p) => `/performers/${p.id}`,
      ),

      {
        id: "favorite",
        accessorFn: (p) => (p.favorite ? 1 : 0),
        enableSorting: true,
        sortDescFirst: true,
        size: 48,
        minSize: 48,
        maxSize: 48,
        meta: { label: intl.formatMessage({ id: "favourite" }) },
        header: ({ column }) => (
          <DataTableColumnHeader
            column={column}
            title={intl.formatMessage({ id: "favourite" })}
          />
        ),
        cell: ({ row }) => <FavoriteCell performer={row.original} />,
      },

      titleColumn<PerformerItem>({
        id: "name",
        header: intl.formatMessage({ id: "name" }),
        getTitle: (p) =>
          p.disambiguation ? `${p.name} (${p.disambiguation})` : p.name,
        getHref: (p) => `/performers/${p.id}`,
      }),

      {
        id: "gender",
        accessorFn: (p) => p.gender ?? "",
        enableSorting: false,
        meta: { label: intl.formatMessage({ id: "gender" }) },
        header: ({ column }) => (
          <DataTableColumnHeader
            column={column}
            title={intl.formatMessage({ id: "gender" })}
          />
        ),
        cell: ({ row }) => {
          const gender = row.original.gender;
          if (!gender) return null;
          return (
            <span className="text-xs text-muted-foreground">
              {intl.formatMessage({
                id: `gender_types.${gender}`,
                defaultMessage: gender,
              })}
            </span>
          );
        },
      },

      {
        id: "birthdate",
        accessorFn: (p) => p.birthdate ?? "",
        enableSorting: true,
        meta: { label: intl.formatMessage({ id: "birthdate" }) },
        header: ({ column }) => (
          <DataTableColumnHeader
            column={column}
            title={intl.formatMessage({ id: "birthdate" })}
          />
        ),
        cell: ({ row }) => {
          const p = row.original;
          const age = getAge(p.birthdate, p.death_date);
          if (!p.birthdate && age == null) return null;
          return (
            <span className="text-xs text-muted-foreground whitespace-nowrap">
              {p.birthdate}
              {age != null ? ` (${age})` : ""}
            </span>
          );
        },
      },

      // Custom cell instead of `textColumn` so the country code
      // resolves to its localised name + flag emoji on display
      // (matches the performer detail view + card subtitle).
      {
        id: "country",
        accessorFn: (row: PerformerItem) => row.country ?? "",
        enableSorting: false,
        meta: { label: intl.formatMessage({ id: "country" }) },
        header: ({ column }) => (
          <DataTableColumnHeader
            column={column}
            title={intl.formatMessage({ id: "country" })}
          />
        ),
        cell: ({ row }) => {
          if (!row.original.country) return null;
          return (
            <span className="text-xs text-muted-foreground">
              <CountryDisplay value={row.original.country} />
            </span>
          );
        },
      } satisfies ColumnDef<PerformerItem>,

      ratingColumn<PerformerItem>({
        getRating: (p) => p.rating100,
        header: intl.formatMessage({ id: "rating" }),
      }),

      numberColumn<PerformerItem>({
        id: "scenes_count",
        header: intl.formatMessage({ id: "scene_count" }),
        getValue: (p) => p.scene_count,
      }),

      numberColumn<PerformerItem>({
        id: "o_counter",
        header: intl.formatMessage({ id: "o_count" }),
        getValue: (p) => p.o_counter,
      }),

      tagsColumn<PerformerItem>({
        getTags: (p) => p.tags,
        header: intl.formatMessage({ id: "tags" }),
      }),
    ],
    [intl],
  );
}
