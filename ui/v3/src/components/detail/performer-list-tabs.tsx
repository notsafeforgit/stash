/**
 * Performer detail embedded list tab panels.
 *
 * Each panel renders an EntityListPage scoped to the current performer.
 * Filter state (sort, page, criteria) is synced to the URL alongside the
 * active `tab` param so position is preserved on refresh.
 */

import { useMemo, useState, useCallback } from "react";
import { useIntl } from "react-intl";
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
  useGroupListConfig,
} from "src/components/list/entity-list-configs";
import { useMutation } from "@apollo/client/react";
import { ListFilterModel } from "src/models/list-filter/filter";
import { PerformersCriterion } from "src/models/list-filter/criteria/performers";
import {
  createASTConditionFromCriterion,
  createASTGroup,
} from "src/models/list-filter/filter-ast";
import { useConfigurationContextOptional } from "src/hooks/config";
import { useToast } from "src/hooks/toast";
import { SceneEditSheet } from "./scene-edit-sheet";
import { ImageEditSheet } from "./image-edit-sheet";
import { GalleryEditSheet } from "./gallery-edit-sheet";
import { GroupEditSheet } from "./group-edit-sheet";

// ── Helpers ────────────────────────────────────────────────────────────────────

type PerformerData = NonNullable<GQL.FindPerformerQuery["findPerformer"]>;
type ImageItem = GQL.FindImagesQuery["findImages"]["images"][number];

/**
 * Build a ListFilterModel with a locked PerformersCriterion in the AST for
 * the given performer. The criterion uses IncludesAll so only content
 * featuring this performer is shown. It lives in `lockedFilterAst` so it
 * cannot be removed or bypassed by the user (loading saved filters, clearing
 * filters, etc.) and does not inflate the filter badge count.
 */
function makePerformerFilter(
  mode: FilterMode,
  performerId: string,
  performerName: string,
  config: GQL.ConfigDataFragment | undefined,
): ListFilterModel {
  const filter = new ListFilterModel(mode, config);

  const criterion = new PerformersCriterion();
  criterion.modifier = CriterionModifier.IncludesAll;
  criterion.value = {
    items: [{ id: performerId, label: performerName }],
    excluded: [],
  };

  const conditionNode = createASTConditionFromCriterion(mode, criterion);
  filter.lockedFilterAst = createASTGroup(mode, FilterGroupOperator.And, [
    conditionNode,
  ]);

  return filter;
}

// ── Scenes tab ─────────────────────────────────────────────────────────────────

export function PerformerScenesTab({
  performer,
}: {
  performer: PerformerData;
}) {
  const ctx = useConfigurationContextOptional();
  const { id: performerId, name: performerName } = performer;
  const gqlConfig = ctx?.configuration;
  const [editingId, setEditingId] = useState<string | null>(null);
  const defaultFilter = useMemo(
    () =>
      makePerformerFilter(
        FilterMode.Scenes,
        performerId,
        performerName,
        gqlConfig,
      ),
    [performerId, performerName, gqlConfig],
  );
  const { config, lightboxElement, lightboxOpen } = useSceneListConfig(
    setEditingId,
    true,
  );
  return (
    <>
      <EntityListPage
        key={performerId}
        config={config}
        defaultFilter={defaultFilter}
        view={View.PerformerScenes}
        mobileChromeFixed
        keyboardShortcutsDisabled={lightboxOpen}
      />
      <SceneEditSheet id={editingId} onClose={() => setEditingId(null)} />
      {lightboxElement}
    </>
  );
}

// ── Images tab ─────────────────────────────────────────────────────────────────

export function PerformerImagesTab({
  performer,
  onImageUpdateChange,
}: {
  performer: PerformerData;
  onImageUpdateChange?: (updating: boolean) => void;
}) {
  const ctx = useConfigurationContextOptional();
  const { id: performerId, name: performerName } = performer;
  const gqlConfig = ctx?.configuration;
  const intl = useIntl();
  const toast = useToast();
  const defaultFilter = useMemo(
    () =>
      makePerformerFilter(
        FilterMode.Images,
        performerId,
        performerName,
        gqlConfig,
      ),
    [performerId, performerName, gqlConfig],
  );
  const [updatePerformer] = useMutation(GQL.PerformerUpdateDocument);
  const [editingId, setEditingId] = useState<string | null>(null);

  const getExtraCardProps = useCallback(
    (image: ImageItem) => ({
      onSetPerformerImage: async () => {
        onImageUpdateChange?.(true);
        try {
          await updatePerformer({
            variables: {
              input: { id: performer.id, image_input: { image_id: image.id } },
            },
          });
          toast.success(
            intl.formatMessage({
              id: "toast.performer_image_set",
              defaultMessage: "Performer image updated.",
            }),
          );
        } catch (e) {
          toast.error(e);
        } finally {
          onImageUpdateChange?.(false);
        }
      },
    }),
    [updatePerformer, performer.id, toast, intl, onImageUpdateChange],
  );

  const { config, lightboxElement, lightboxOpen } = useImageListConfig(
    setEditingId,
    getExtraCardProps,
    true,
  );

  return (
    <>
      <EntityListPage
        key={performerId}
        config={config}
        defaultFilter={defaultFilter}
        view={View.PerformerImages}
        mobileChromeFixed
        keyboardShortcutsDisabled={lightboxOpen}
      />
      <ImageEditSheet id={editingId} onClose={() => setEditingId(null)} />
      {lightboxElement}
    </>
  );
}

// ── Galleries tab ──────────────────────────────────────────────────────────────

export function PerformerGalleriesTab({
  performer,
}: {
  performer: PerformerData;
}) {
  const ctx = useConfigurationContextOptional();
  const { id: performerId, name: performerName } = performer;
  const gqlConfig = ctx?.configuration;
  const [editingId, setEditingId] = useState<string | null>(null);
  const defaultFilter = useMemo(
    () =>
      makePerformerFilter(
        FilterMode.Galleries,
        performerId,
        performerName,
        gqlConfig,
      ),
    [performerId, performerName, gqlConfig],
  );
  const config = useGalleryListConfig(setEditingId);
  return (
    <>
      <EntityListPage
        key={performerId}
        config={config}
        defaultFilter={defaultFilter}
        view={View.PerformerGalleries}
        mobileChromeFixed
      />
      <GalleryEditSheet id={editingId} onClose={() => setEditingId(null)} />
    </>
  );
}

// ── Groups tab ─────────────────────────────────────────────────────────────────

export function PerformerGroupsTab({
  performer,
}: {
  performer: PerformerData;
}) {
  const ctx = useConfigurationContextOptional();
  const { id: performerId, name: performerName } = performer;
  const gqlConfig = ctx?.configuration;
  const [editingId, setEditingId] = useState<string | null>(null);
  const defaultFilter = useMemo(
    () =>
      makePerformerFilter(
        FilterMode.Groups,
        performerId,
        performerName,
        gqlConfig,
      ),
    [performerId, performerName, gqlConfig],
  );
  const config = useGroupListConfig(setEditingId);
  return (
    <>
      <EntityListPage
        key={performerId}
        config={config}
        defaultFilter={defaultFilter}
        view={View.PerformerGroups}
        mobileChromeFixed
      />
      <GroupEditSheet id={editingId} onClose={() => setEditingId(null)} />
    </>
  );
}
