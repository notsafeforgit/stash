/**
 * Studio detail embedded list tab panels.
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
  useSceneListConfig,
  useImageListConfig,
  useGalleryListConfig,
  usePerformerListConfig,
  useGroupListConfig,
} from "src/components/list/entity-list-configs";
import { useMutation } from "@apollo/client/react";
import { ListFilterModel } from "src/models/list-filter/filter";
import { StudiosCriterion } from "src/models/list-filter/criteria/studios";
import {
  createASTConditionFromCriterion,
  createASTGroup,
} from "src/models/list-filter/filter-ast";
import { useConfigurationContextOptional } from "src/hooks/config";
import { SceneEditSheet } from "./scene-edit-sheet";
import { ImageEditSheet } from "./image-edit-sheet";
import { GalleryEditSheet } from "./gallery-edit-sheet";
import { PerformerEditSheet } from "./performer-edit-sheet";
import { GroupEditSheet } from "./group-edit-sheet";

// ── Helpers ────────────────────────────────────────────────────────────────────

type StudioData = NonNullable<GQL.FindStudioQuery["findStudio"]>;
type ImageItem = GQL.FindImagesQuery["findImages"]["images"][number];

function makeStudioFilter(
  mode: FilterMode,
  studioId: string,
  studioName: string,
  config: GQL.ConfigDataFragment | undefined,
): ListFilterModel {
  const filter = new ListFilterModel(mode, config);

  const criterion = new StudiosCriterion();
  criterion.modifier = CriterionModifier.Includes;
  criterion.value = {
    items: [{ id: studioId, label: studioName }],
    excluded: [],
    depth: 0,
  };

  const conditionNode = createASTConditionFromCriterion(mode, criterion);
  filter.lockedFilterAst = createASTGroup(mode, FilterGroupOperator.And, [
    conditionNode,
  ]);

  return filter;
}

// ── Scenes tab ─────────────────────────────────────────────────────────────────

export function StudioScenesTab({ studio }: { studio: StudioData }) {
  const ctx = useConfigurationContextOptional();
  const { id: studioId, name: studioName } = studio;
  const gqlConfig = ctx?.configuration;
  const [editingId, setEditingId] = useState<string | null>(null);
  const defaultFilter = useMemo(
    () => makeStudioFilter(FilterMode.Scenes, studioId, studioName, gqlConfig),
    [studioId, studioName, gqlConfig],
  );
  const { config, lightboxElement, lightboxOpen } =
    useSceneListConfig(setEditingId);
  return (
    <>
      <EntityListPage
        key={studioId}
        config={config}
        defaultFilter={defaultFilter}
        view={View.StudioScenes}
        mobileChromeFixed
        keyboardShortcutsDisabled={lightboxOpen}
      />
      <SceneEditSheet id={editingId} onClose={() => setEditingId(null)} />
      {lightboxElement}
    </>
  );
}

// ── Images tab ─────────────────────────────────────────────────────────────────

export function StudioImagesTab({ studio }: { studio: StudioData }) {
  const ctx = useConfigurationContextOptional();
  const { id: studioId, name: studioName } = studio;
  const gqlConfig = ctx?.configuration;
  const defaultFilter = useMemo(
    () => makeStudioFilter(FilterMode.Images, studioId, studioName, gqlConfig),
    [studioId, studioName, gqlConfig],
  );
  const [updateStudio] = useMutation(GQL.StudioUpdateDocument);
  const [editingId, setEditingId] = useState<string | null>(null);

  const getExtraCardProps = useCallback(
    (image: ImageItem) => ({
      onSetStudioImage: () =>
        updateStudio({
          variables: {
            input: {
              id: studio.id,
              image_input: { image_id: image.id },
            },
          },
        }),
    }),
    [updateStudio, studio.id],
  );

  const { config, lightboxElement } = useImageListConfig(
    setEditingId,
    getExtraCardProps,
  );

  return (
    <>
      <EntityListPage
        key={studioId}
        config={config}
        defaultFilter={defaultFilter}
        view={View.StudioImages}
        mobileChromeFixed
      />
      <ImageEditSheet id={editingId} onClose={() => setEditingId(null)} />
      {lightboxElement}
    </>
  );
}

// ── Galleries tab ──────────────────────────────────────────────────────────────

export function StudioGalleriesTab({ studio }: { studio: StudioData }) {
  const ctx = useConfigurationContextOptional();
  const { id: studioId, name: studioName } = studio;
  const gqlConfig = ctx?.configuration;
  const [editingId, setEditingId] = useState<string | null>(null);
  const defaultFilter = useMemo(
    () =>
      makeStudioFilter(FilterMode.Galleries, studioId, studioName, gqlConfig),
    [studioId, studioName, gqlConfig],
  );
  const config = useGalleryListConfig(setEditingId);
  return (
    <>
      <EntityListPage
        key={studioId}
        config={config}
        defaultFilter={defaultFilter}
        view={View.StudioGalleries}
        mobileChromeFixed
      />
      <GalleryEditSheet id={editingId} onClose={() => setEditingId(null)} />
    </>
  );
}

// ── Performers tab ─────────────────────────────────────────────────────────────

export function StudioPerformersTab({ studio }: { studio: StudioData }) {
  const ctx = useConfigurationContextOptional();
  const { id: studioId, name: studioName } = studio;
  const gqlConfig = ctx?.configuration;
  const [editingId, setEditingId] = useState<string | null>(null);
  const defaultFilter = useMemo(
    () =>
      makeStudioFilter(FilterMode.Performers, studioId, studioName, gqlConfig),
    [studioId, studioName, gqlConfig],
  );
  const config = usePerformerListConfig(setEditingId);
  return (
    <>
      <EntityListPage
        key={studioId}
        config={config}
        defaultFilter={defaultFilter}
        view={View.StudioPerformers}
        mobileChromeFixed
      />
      <PerformerEditSheet id={editingId} onClose={() => setEditingId(null)} />
    </>
  );
}

// ── Groups tab ─────────────────────────────────────────────────────────────────

export function StudioGroupsTab({ studio }: { studio: StudioData }) {
  const ctx = useConfigurationContextOptional();
  const { id: studioId, name: studioName } = studio;
  const gqlConfig = ctx?.configuration;
  const [editingId, setEditingId] = useState<string | null>(null);
  const defaultFilter = useMemo(
    () => makeStudioFilter(FilterMode.Groups, studioId, studioName, gqlConfig),
    [studioId, studioName, gqlConfig],
  );
  const config = useGroupListConfig(setEditingId);
  return (
    <>
      <EntityListPage
        key={studioId}
        config={config}
        defaultFilter={defaultFilter}
        view={View.StudioGroups}
        mobileChromeFixed
      />
      <GroupEditSheet id={editingId} onClose={() => setEditingId(null)} />
    </>
  );
}
