import { useCallback, useState } from "react";
import { useIntl } from "react-intl";
import { useMutation } from "@apollo/client/react";
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
import { SceneGenerateDialog } from "src/components/detail/scene-generate-dialog";
import { SceneMergeDialog } from "src/components/detail/scene-merge-dialog";
import { useToast } from "src/hooks/toast";
import { useConfigurationContext } from "src/hooks/config";
import { objectPath, objectTitle } from "src/core/files";
import { useSceneDownloadAction } from "@/components/offline/download-action";
import { type MonitoredJob, useMonitorJob } from "src/hooks/use-monitor-job";
import { supportsSceneVideoRotation } from "./scene-video-rotation";

export interface SceneActionsMenuProps {
  scene: NonNullable<GQL.FindSceneQuery["findScene"]>;
  /** Returns the current playback time in seconds, or undefined when unknown */
  getPlayerPosition?: () => number | undefined;
  /** Called once the scene has been deleted so the page can navigate away */
  onDeleted?: () => void;
  /** Refreshes the scene after a generated screenshot replaces its cover. */
  onScreenshotGenerated?: () => void | Promise<void>;
}

export function SceneActionsMenu({
  scene,
  getPlayerPosition,
  onDeleted,
  onScreenshotGenerated,
}: SceneActionsMenuProps) {
  const intl = useIntl();
  const toast = useToast();
  const { configuration } = useConfigurationContext();
  const stashBoxes = configuration.general.stashBoxes ?? [];
  const firstStashBox = stashBoxes[0];

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [generateOpen, setGenerateOpen] = useState(false);
  const [mergeOpen, setMergeOpen] = useState(false);
  const [screenshotJobId, setScreenshotJobId] = useState<string | null>(null);
  const download = useSceneDownloadAction({ scene });

  const [scan] = useMutation(GQL.MetadataScanDocument);
  const [generateScreenshot] = useMutation(GQL.SceneGenerateScreenshotDocument);
  const [rotateVideo, { loading: rotationPending }] = useMutation(
    GQL.SceneVideoRotateDocument,
  );
  const [destroyScene] = useEntityMutation(GQL.SceneDestroyDocument);
  const [submitDraft] = useMutation(GQL.SubmitStashBoxSceneDraftDocument);

  const sceneFilePath = scene.files.length > 0 ? objectPath(scene) : null;
  const rotationSupported = supportsSceneVideoRotation(sceneFilePath);

  const handleScreenshotJobComplete = useCallback(
    async (job?: MonitoredJob) => {
      setScreenshotJobId(null);
      if (job?.status === GQL.JobStatus.Failed) {
        toast.error(
          job.error ||
            intl.formatMessage({
              id: "toast.screenshot_generation_failed",
              defaultMessage: "Screenshot generation failed",
            }),
        );
        return;
      }
      if (job?.status === GQL.JobStatus.Cancelled) return;

      try {
        await onScreenshotGenerated?.();
        toast.success(
          intl.formatMessage({
            id: "toast.screenshot_generated",
            defaultMessage: "Screenshot generated",
          }),
        );
      } catch (error) {
        toast.error(error);
      }
    },
    [intl, onScreenshotGenerated, toast],
  );

  useMonitorJob(screenshotJobId, handleScreenshotJobComplete);

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

  async function handleGenerateScreenshot(at?: number) {
    try {
      const result = await generateScreenshot({
        variables: { id: scene.id, at },
      });
      const jobId = result.data?.sceneGenerateScreenshot;
      if (jobId) {
        setScreenshotJobId(jobId);
      } else {
        await onScreenshotGenerated?.();
      }
      toast.success(
        intl.formatMessage({
          id: "toast.generating_screenshot",
          defaultMessage: "Generating screenshot",
        }),
      );
    } catch (e) {
      toast.error(e);
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

  const items: EntityActionItem[] = [];
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
    disabled: () =>
      screenshotJobId !== null || getPlayerPosition?.() === undefined,
  });
  items.push({
    key: "default-thumbnail",
    icon: CameraOff,
    label: intl.formatMessage({
      id: "actions.generate_thumb_default",
      defaultMessage: "Generate default thumbnail",
    }),
    onSelect: () => handleGenerateScreenshot(),
    disabled: screenshotJobId !== null,
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
