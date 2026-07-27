import { useCallback, useState } from "react";
import { useIntl } from "react-intl";
import { useMutation } from "@apollo/client/react";
import { removeEntitiesFromCache, useEntityMutation } from "src/core/client";
import {
  Camera,
  CameraOff,
  Cog,
  EllipsisVertical,
  GitMerge,
  RefreshCcw,
  Send,
  Trash2,
} from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "src/components/ui/dropdown-menu";
import {
  DeleteDialog,
  DeleteFilesList,
} from "src/components/detail/delete-dialog";
import { SceneGenerateDialog } from "src/components/detail/scene-generate-dialog";
import { SceneMergeDialog } from "src/components/detail/scene-merge-dialog";
import { useToast } from "src/hooks/toast";
import { useConfigurationContext } from "src/hooks/config";
import { objectPath, objectTitle } from "src/core/files";
import { SceneDetailDownloadMenuItem } from "src/components/offline/scene-detail-download-menu-item";
import { type MonitoredJob, useMonitorJob } from "src/hooks/use-monitor-job";

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

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [generateOpen, setGenerateOpen] = useState(false);
  const [mergeOpen, setMergeOpen] = useState(false);
  const [screenshotJobId, setScreenshotJobId] = useState<string | null>(null);
  // Controlled so opening the menu forces a re-render — `getPlayerPosition`
  // reads from a ref set imperatively when the player mounts, so without this
  // the disabled check below latches at its initial render value.
  const [menuOpen, setMenuOpen] = useState(false);

  const [scan] = useMutation(GQL.MetadataScanDocument);
  const [generateScreenshot] = useMutation(GQL.SceneGenerateScreenshotDocument);
  const [destroyScene] = useEntityMutation(GQL.SceneDestroyDocument);
  const [submitDraft] = useMutation(GQL.SubmitStashBoxSceneDraftDocument);

  const sceneFilePath = scene.files.length > 0 ? objectPath(scene) : null;

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

  return (
    <>
      <DropdownMenu modal={false} open={menuOpen} onOpenChange={setMenuOpen}>
        <DropdownMenuTrigger
          render={<Button variant="outline" size="sm" />}
          aria-label={intl.formatMessage({
            id: "operations",
            defaultMessage: "Operations",
          })}
          title={intl.formatMessage({
            id: "operations",
            defaultMessage: "Operations",
          })}
        >
          <EllipsisVertical />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          {sceneFilePath && (
            <DropdownMenuItem onClick={handleRescan}>
              <RefreshCcw />
              {intl.formatMessage({
                id: "actions.rescan",
                defaultMessage: "Rescan",
              })}
            </DropdownMenuItem>
          )}
          <DropdownMenuItem onClick={() => setGenerateOpen(true)}>
            <Cog />
            {intl.formatMessage({
              id: "actions.generate",
              defaultMessage: "Generate",
            })}
            …
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={
              screenshotJobId !== null || getPlayerPosition?.() === undefined
            }
            onClick={() => {
              const at = getPlayerPosition?.();
              if (at !== undefined) handleGenerateScreenshot(at);
            }}
          >
            <Camera />
            {intl.formatMessage({
              id: "actions.generate_thumb_from_current",
              defaultMessage: "Generate thumbnail from current",
            })}
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={screenshotJobId !== null}
            onClick={() => handleGenerateScreenshot()}
          >
            <CameraOff />
            {intl.formatMessage({
              id: "actions.generate_thumb_default",
              defaultMessage: "Generate default thumbnail",
            })}
          </DropdownMenuItem>

          {stashBoxes.length > 0 && (
            <>
              <DropdownMenuSeparator />
              {stashBoxes.length === 1 ? (
                <DropdownMenuItem
                  onClick={() =>
                    handleSubmit(
                      stashBoxes[0].endpoint,
                      stashBoxes[0].name || stashBoxes[0].endpoint,
                    )
                  }
                >
                  <Send />
                  {intl.formatMessage({
                    id: "actions.submit_stash_box",
                    defaultMessage: "Submit to Stash-Box",
                  })}
                </DropdownMenuItem>
              ) : (
                <DropdownMenuSub>
                  <DropdownMenuSubTrigger>
                    <Send />
                    {intl.formatMessage({
                      id: "actions.submit_stash_box",
                      defaultMessage: "Submit to Stash-Box",
                    })}
                  </DropdownMenuSubTrigger>
                  <DropdownMenuSubContent>
                    {stashBoxes.map((box) => (
                      <DropdownMenuItem
                        key={box.endpoint}
                        onClick={() =>
                          handleSubmit(box.endpoint, box.name || box.endpoint)
                        }
                      >
                        {box.name || box.endpoint}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuSubContent>
                </DropdownMenuSub>
              )}
            </>
          )}

          <DropdownMenuSeparator />
          <SceneDetailDownloadMenuItem scene={scene} />
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={() => setMergeOpen(true)}>
            <GitMerge />
            {intl.formatMessage({
              id: "actions.merge",
              defaultMessage: "Merge",
            })}
            …
          </DropdownMenuItem>
          <DropdownMenuItem
            variant="destructive"
            onClick={() => setDeleteOpen(true)}
          >
            <Trash2 />
            {intl.formatMessage(
              {
                id: "actions.delete_entity",
                defaultMessage: "Delete {entityType}",
              },
              {
                entityType: intl
                  .formatMessage({ id: "scene", defaultMessage: "scene" })
                  .toLocaleLowerCase(),
              },
            )}
            …
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

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
