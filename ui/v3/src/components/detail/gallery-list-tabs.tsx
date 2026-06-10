/**
 * Gallery detail embedded list tab panels.
 */

import { useMemo, useState, useCallback } from "react";
import * as GQL from "src/core/generated-graphql";
import {
  FilterMode,
  FilterGroupOperator,
  CriterionModifier,
} from "src/core/generated-graphql";
import { EntityListPage } from "src/components/list";
import { View } from "src/components/list/views";
import { useImageListConfig } from "src/components/list/entity-list-configs";
import { useMutation } from "@apollo/client/react";
import { ListFilterModel } from "src/models/list-filter/filter";
import {
  GalleriesCriterion,
  GalleriesCriterionOption,
} from "src/models/list-filter/criteria/galleries";
import {
  createASTConditionFromCriterion,
  createASTGroup,
} from "src/models/list-filter/filter-ast";
import { useConfigurationContextOptional } from "src/hooks/config";
import { galleryLabel as getGalleryLabel } from "src/lib/gallery-utils";
import { ImageEditSheet } from "./image-edit-sheet";

// ── Helpers ────────────────────────────────────────────────────────────────────

type GalleryData = NonNullable<GQL.FindGalleryQuery["findGallery"]>;
type ImageItem = GQL.FindImagesQuery["findImages"]["images"][number];

function makeGalleryFilter(
  mode: FilterMode,
  galleryId: string,
  galleryLabel: string,
  config: GQL.ConfigDataFragment | undefined,
): ListFilterModel {
  const filter = new ListFilterModel(mode, config);

  const criterion = new GalleriesCriterion(GalleriesCriterionOption);
  criterion.modifier = CriterionModifier.IncludesAll;
  criterion.value = {
    items: [{ id: galleryId, label: galleryLabel }],
    excluded: [],
    depth: 0,
  };

  const conditionNode = createASTConditionFromCriterion(mode, criterion);
  filter.lockedFilterAst = createASTGroup(mode, FilterGroupOperator.And, [
    conditionNode,
  ]);

  return filter;
}

// ── Images tab ─────────────────────────────────────────────────────────────────

export function GalleryImagesTab({ gallery }: { gallery: GalleryData }) {
  const ctx = useConfigurationContextOptional();
  const { id: galleryId } = gallery;
  const galleryLabel = getGalleryLabel(gallery);
  const gqlConfig = ctx?.configuration;
  const defaultFilter = useMemo(
    () =>
      makeGalleryFilter(FilterMode.Images, galleryId, galleryLabel, gqlConfig),
    [galleryId, galleryLabel, gqlConfig],
  );
  // The mutation returns just `boolean`, so we can't update the cache
  // directly. Refetch only `FindGallery` for this specific gallery —
  // narrower than `refetchQueries: "active"`, which would also reload
  // the embedded image list and make the whole page flash.
  const [setGalleryCover] = useMutation(GQL.SetGalleryCoverDocument, {
    refetchQueries: [
      {
        query: GQL.FindGalleryDocument,
        variables: { id: gallery.id },
      },
    ],
  });
  const [editingId, setEditingId] = useState<string | null>(null);

  const getExtraCardProps = useCallback(
    (image: ImageItem) => ({
      onSetGalleryCover: () =>
        setGalleryCover({
          variables: { gallery_id: gallery.id, cover_image_id: image.id },
        }),
    }),
    [setGalleryCover, gallery.id],
  );

  const { config, lightboxElement, lightboxOpen } = useImageListConfig(
    setEditingId,
    getExtraCardProps,
  );

  return (
    <>
      <EntityListPage
        key={galleryId}
        config={config}
        defaultFilter={defaultFilter}
        view={View.GalleryImages}
        mobileChromeFixed
        keyboardShortcutsDisabled={lightboxOpen}
      />
      <ImageEditSheet id={editingId} onClose={() => setEditingId(null)} />
      {lightboxElement}
    </>
  );
}
