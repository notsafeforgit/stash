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
import {
  useImageListConfig,
  useSceneListConfig,
} from "src/components/list/entity-list-configs";
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
import { SceneEditSheet } from "./scene-edit-sheet";
import { useGalleryCover } from "./use-gallery-cover";

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
  const { setCover } = useGalleryCover(gallery.id);
  const [editingId, setEditingId] = useState<string | null>(null);

  const getExtraCardProps = useCallback(
    (image: ImageItem) => ({
      onSetGalleryCover: () => setCover("image", image.id),
    }),
    [setCover],
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

export function GalleryScenesTab({ gallery }: { gallery: GalleryData }) {
  const ctx = useConfigurationContextOptional();
  const label = getGalleryLabel(gallery);
  const configData = ctx?.configuration;
  const defaultFilter = useMemo(
    () => makeGalleryFilter(FilterMode.Scenes, gallery.id, label, configData),
    [gallery.id, label, configData],
  );
  const [editingId, setEditingId] = useState<string | null>(null);
  const { setCover } = useGalleryCover(gallery.id);
  const setSceneCover = useCallback(
    (id: string) => setCover("scene", id),
    [setCover],
  );
  const { config, lightboxElement, lightboxOpen } = useSceneListConfig(
    setEditingId,
    undefined,
    undefined,
    setSceneCover,
  );
  return (
    <>
      <EntityListPage
        key={gallery.id}
        config={config}
        defaultFilter={defaultFilter}
        view={View.GalleryScenes}
        mobileChromeFixed
        keyboardShortcutsDisabled={lightboxOpen}
      />
      <SceneEditSheet id={editingId} onClose={() => setEditingId(null)} />
      {lightboxElement}
    </>
  );
}
