import { useState } from "react";
import { useIntl } from "react-intl";
import { useApolloClient, useMutation } from "@apollo/client/react";
import { removeEntitiesFromCache, useEntityMutation } from "src/core/client";
import {
  Camera,
  CameraOff,
  Cog,
  Download,
  GitMerge,
  RefreshCcw,
  RotateCcw,
  RotateCw,
  Send,
  Trash2,
  Undo2,
  UserRound,
} from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import {
  EntityActionsMenu,
  type EntityActionItem,
} from "./entity-actions-menu";
import {
  DeleteDialog,
  DeleteFilesList,
} from "src/components/detail/delete-dialog";
import {
  SceneGenerateDialog,
  SceneMergeDialog,
} from "@/components/detail/deferred-overlays";
import { useToast } from "src/hooks/toast";
import { useConfigurationContext } from "src/hooks/config";
import { objectPath, objectTitle } from "src/core/files";
import { useSceneDownloadAction } from "@/components/offline/download-action";
import { refreshSceneCoversAfterJob } from "@/core/scene-cover-job";
import { supportsSceneVideoRotation } from "./scene-video-rotation";
import { useScenePerformerImage } from "./use-scene-performer-image";
import { useShareAction } from "@/components/sharing/share-action";

export interface SceneActionsMenuProps {
  scene: NonNullable<GQL.FindSceneQuery["findScene"]>;
  /** Returns the current playback time in seconds, or undefined when unknown */
  getPlayerPosition?: () => number | undefined;
  /** Called once the scene has been deleted so the page can navigate away */
  onDeleted?: () => void;
}

export function SceneActionsMenu({
  scene,
  getPlayerPosition,
  onDeleted,
}: SceneActionsMenuProps) {
  const client = useApolloClient();
  const intl = useIntl();
  const toast = useToast();
  const { configuration } = useConfigurationContext();
  const stashBoxes = configuration.general.stashBoxes ?? [];
  const firstStashBox = stashBoxes[0];

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [generateOpen, setGenerateOpen] = useState(false);
  const [mergeOpen, setMergeOpen] = useState(false);
  const [coverBusy, setCoverBusy] = useState(false);
  const download = useSceneDownloadAction({ scene });
  const performerImage = useScenePerformerImage(scene.id);
  const share = useShareAction([
    {
      kind: GQL.ShareEntityKind.Scene,
      id: scene.id,
      name: objectTitle(scene) || scene.id,
    },
  ]);

  const [scan] = useMutation(GQL.MetadataScanDocument);
  const [generateScreenshot] = useMutation(GQL.SceneGenerateScreenshotDocument);
  const [regenerateCover] = useMutation(GQL.SceneRegenerateCoverDocument);
  const [rotateVideo, { loading: rotationPending }] = useMutation(
    GQL.SceneVideoRotateDocument,
  );
  const [destroyScene] = useEntityMutation(GQL.SceneDestroyDocument);
  const [submitDraft] = useMutation(GQL.SubmitStashBoxSceneDraftDocument);

  const sceneFilePath = scene.files.length > 0 ? objectPath(scene) : null;
  const rotationSupported = supportsSceneVideoRotation(sceneFilePath);
  async function handleRescan() {
    if (!sceneFilePath) return;
    try {
      await scan({ variables: { input: { paths: [sceneFilePath] } } });
      toast.success(
        intl.formatMessage(
          {
            id: "toast.rescanning_entity",
            defaultMessage: "Rescanning {count} {singularEntity}",
          },
          {
            count: 1,
            singularEntity: intl
              .formatMessage({ id: "scene", defaultMessage: "scene" })
              .toLocaleLowerCase(),
          },
        ),
      );
    } catch (e) {
      toast.error(e);
    }
  }

  async function handleGenerateScreenshot(
    selection: number | "default" | "saved",
  ) {
    setCoverBusy(true);
    try {
      const jobId =
        selection === "saved"
          ? (await regenerateCover({ variables: { id: scene.id } })).data
              ?.sceneRegenerateCover
          : (
              await generateScreenshot({
                variables: {
                  id: scene.id,
                  at: selection === "default" ? undefined : selection,
                },
              })
            ).data?.sceneGenerateScreenshot;
      if (!jobId)
        throw new Error(
          intl.formatMessage({
            id: "toast.screenshot_generation_failed",
            defaultMessage: "Screenshot generation failed",
          }),
        );
      toast.success(
        intl.formatMessage({
          id: "toast.generating_screenshot",
          defaultMessage: "Generating screenshot",
        }),
      );
      // This promise remains responsible for the cache after this menu unmounts.
      const completion = await refreshSceneCoversAfterJob(
        client,
        [scene.id],
        jobId,
      );
      if (completion.kind === "unavailable") throw completion.error;
      if (completion.job?.status === GQL.JobStatus.Cancelled) return;
      if (completion.job?.status === GQL.JobStatus.Failed) {
        throw new Error(
          completion.job.error ||
            intl.formatMessage({
              id: "toast.screenshot_generation_failed",
              defaultMessage: "Screenshot generation failed",
            }),
        );
      }
      toast.success(
        intl.formatMessage({
          id: "toast.screenshot_generated",
          defaultMessage: "Screenshot generated",
        }),
      );
    } catch (e) {
      toast.error(e);
    } finally {
      setCoverBusy(false);
    }
  }

  async function handleVideoRotation(
    direction: GQL.SceneVideoRotationDirection,
  ) {
    try {
      const result = await rotateVideo({
        variables: { id: scene.id, direction },
      });
      if (!result.data?.sceneVideoRotate) return;
      toast.success(
        direction === GQL.SceneVideoRotationDirection.Clear
          ? intl.formatMessage({
              id: "toast.video_rotation_cleared",
              defaultMessage: "Video rotation cleared",
            })
          : intl.formatMessage({
              id: "toast.video_rotation_updated",
              defaultMessage: "Video rotation updated",
            }),
      );
    } catch (e) {
      toast.error(e);
    }
  }

  async function handleSubmit(endpoint: string, endpointName: string) {
    try {
      await submitDraft({
        variables: {
          input: { id: scene.id, stash_box_endpoint: endpoint },
        },
      });
      toast.success(
        intl.formatMessage(
          {
            id: "toast.submitted_to_stash_box",
            defaultMessage: "Submitted to {name}",
          },
          { name: endpointName },
        ),
      );
    } catch (e) {
      toast.error(e);
    }
  }

  async function handleDelete({
    deleteFile,
    deleteGenerated,
  }: {
    deleteFile: boolean;
    deleteGenerated: boolean;
  }) {
    const sceneId = scene.id;
    await destroyScene({
      variables: {
        id: sceneId,
        delete_file: deleteFile,
        delete_generated: deleteGenerated,
      },
      update(cache) {
        removeEntitiesFromCache({
          cache,
          typename: "Scene",
          listFieldName: "findScenes",
          itemsField: "scenes",
          ids: [sceneId],
        });
      },
    });
    onDeleted?.();
  }

  const items: EntityActionItem[] = [share.action];
  if (sceneFilePath)
    items.push({
      key: "rescan",
      icon: RefreshCcw,
      label: intl.formatMessage({
        id: "actions.rescan",
        defaultMessage: "Rescan",
      }),
      onSelect: handleRescan,
    });
  items.push({
    key: "generate",
    icon: Cog,
    label:
      intl.formatMessage({
        id: "actions.generate",
        defaultMessage: "Generate",
      }) + "…",
    onSelect: () => setGenerateOpen(true),
  });
  items.push({
    key: "current-thumbnail",
    icon: Camera,
    label: intl.formatMessage({
      id: "actions.generate_thumb_from_current",
      defaultMessage: "Generate thumbnail from current",
    }),
    onSelect: () => {
      const at = getPlayerPosition?.();
      if (at !== undefined) return handleGenerateScreenshot(at);
    },
    disabled: () => coverBusy || getPlayerPosition?.() === undefined,
  });
  items.push({
    key: "performer-current-frame",
    icon: UserRound,
    label: intl.formatMessage({
      id: "actions.generate_performer_image_from_current",
      defaultMessage: "Generate performer image from current",
    }),
    onSelect: () => {
      const at = getPlayerPosition?.();
      if (at !== undefined) performerImage.setFromScene(at);
    },
    disabled: () =>
      performerImage.pending ||
      scene.performers.length === 0 ||
      getPlayerPosition?.() === undefined,
  });
  items.push({
    key: "regenerate-cover",
    icon: RefreshCcw,
    label: intl.formatMessage({
      id: "actions.regenerate_scene_cover",
      defaultMessage: "Regenerate selected cover",
    }),
    onSelect: () => handleGenerateScreenshot("saved"),
    disabled:
      coverBusy ||
      scene.cover_origin?.status !== GQL.SceneCoverOriginStatus.Available,
  });
  items.push({
    key: "default-thumbnail",
    icon: CameraOff,
    label: intl.formatMessage({
      id: "actions.generate_thumb_default",
      defaultMessage: "Generate default thumbnail",
    }),
    onSelect: () => handleGenerateScreenshot("default"),
    disabled: coverBusy,
  });
  if (rotationSupported)
    items.push({
      key: "rotation",
      icon: RotateCw,
      label: intl.formatMessage({
        id: "actions.rotation",
        defaultMessage: "Rotation",
      }),
      disabled: rotationPending,
      actions: [
        {
          key: "rotate_ccw",
          icon: RotateCcw,
          label: intl.formatMessage({
            id: "actions.rotate_ccw",
            defaultMessage: "Rotate counter-clockwise",
          }),
          onSelect: () =>
            handleVideoRotation(GQL.SceneVideoRotationDirection.Ccw),
        },
        {
          key: "rotate_cw",
          icon: RotateCw,
          label: intl.formatMessage({
            id: "actions.rotate_cw",
            defaultMessage: "Rotate clockwise",
          }),
          onSelect: () =>
            handleVideoRotation(GQL.SceneVideoRotationDirection.Cw),
        },
        {
          key: "clear_rotation",
          icon: Undo2,
          label: intl.formatMessage({
            id: "actions.clear_rotation",
            defaultMessage: "Clear rotation",
          }),
          onSelect: () =>
            handleVideoRotation(GQL.SceneVideoRotationDirection.Clear),
        },
      ],
    });
  if (firstStashBox) {
    items.push({ key: "submit-separator", separator: true });
    items.push(
      stashBoxes.length === 1
        ? {
            key: "submit",
            icon: Send,
            label: intl.formatMessage({
              id: "actions.submit_stash_box",
              defaultMessage: "Submit to Stash-Box",
            }),
            onSelect: () =>
              handleSubmit(
                firstStashBox.endpoint,
                firstStashBox.name || firstStashBox.endpoint,
              ),
          }
        : {
            key: "submit",
            icon: Send,
            label: intl.formatMessage({
              id: "actions.submit_stash_box",
              defaultMessage: "Submit to Stash-Box",
            }),
            actions: stashBoxes.map((box) => ({
              key: box.endpoint,
              icon: Send,
              label: box.name || box.endpoint,
              onSelect: () =>
                handleSubmit(box.endpoint, box.name || box.endpoint),
            })),
          },
    );
  }
  items.push(
    { key: "download-separator", separator: true },
    {
      key: "download",
      icon: Download,
      label: download.label,
      disabled: download.disabled,
      onSelect: download.onSelect,
    },
    { key: "manage-separator", separator: true },
  );
  items.push({
    key: "merge",
    icon: GitMerge,
    label:
      intl.formatMessage({ id: "actions.merge", defaultMessage: "Merge" }) +
      "…",
    onSelect: () => setMergeOpen(true),
  });
  items.push({
    key: "delete",
    icon: Trash2,
    label:
      intl.formatMessage(
        { id: "actions.delete_entity", defaultMessage: "Delete {entityType}" },
        {
          entityType: intl
            .formatMessage({ id: "scene", defaultMessage: "scene" })
            .toLocaleLowerCase(),
        },
      ) + "…",
    onSelect: () => setDeleteOpen(true),
    destructive: true,
  });

  return (
    <>
      <EntityActionsMenu items={items} busy={rotationPending} />
      {performerImage.dialog}
      {share.dialog}

      <DeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        entityName={objectTitle(scene) || undefined}
        showFileOptions
        details={
          scene.files.length > 0 ? (
            <DeleteFilesList paths={scene.files.map((f) => f.path)} />
          ) : undefined
        }
        detailsLabel={intl.formatMessage(
          {
            id: "dialogs.delete_show_files_count",
            defaultMessage:
              "Show {count, plural, one {# file} other {# files}}",
          },
          { count: scene.files.length },
        )}
        onConfirm={handleDelete}
      />
      <SceneGenerateDialog
        open={generateOpen}
        onOpenChange={setGenerateOpen}
        sceneIds={[scene.id]}
        hasMarkers={scene.scene_markers.length > 0}
        hasInteractive={scene.interactive}
      />
      <SceneMergeDialog
        open={mergeOpen}
        onOpenChange={setMergeOpen}
        sources={[
          {
            id: scene.id,
            title: scene.title,
            date: scene.date ?? null,
            paths: { screenshot: scene.paths.screenshot ?? null },
            studio: scene.studio ? { name: scene.studio.name } : null,
            performers: scene.performers.map((p) => ({ name: p.name })),
          },
        ]}
      />
    </>
  );
}
