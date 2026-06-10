import { useState } from "react";
import { useIntl } from "react-intl";
import { useMutation } from "@apollo/client/react";
import { useNavigate } from "@tanstack/react-router";
import {
  EllipsisVerticalIcon,
  StarIcon,
  SplitSquareHorizontalIcon,
  ArrowRightLeftIcon,
  Trash2Icon,
} from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { useToast } from "src/hooks/toast";
import { evictQueries } from "src/core/client";
import { Button } from "src/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "src/components/ui/dropdown-menu";
import { DeleteDialog } from "src/components/detail/delete-dialog";
import {
  SceneSelectDialog,
  type SceneSummary,
} from "src/components/detail/scene-select-dialog";
import { SceneCreateSheet } from "src/components/detail/scene-create-sheet";
import {
  type SceneData,
  sceneToFormValues,
} from "src/components/detail/scene-edit-form";

interface SceneFileActionsMenuProps {
  scene: SceneData;
  fileId: string;
  /** File path used as the "name" in the delete confirmation. */
  filePath: string;
}

export function SceneFileActionsMenu({
  scene,
  fileId,
  filePath,
}: SceneFileActionsMenuProps) {
  const sceneId = scene.id;
  const intl = useIntl();
  const toast = useToast();
  const navigate = useNavigate();

  const [reassignOpen, setReassignOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [splitOpen, setSplitOpen] = useState(false);

  const [updateScene] = useMutation(GQL.SceneUpdateDocument);
  const [assignFile] = useMutation(GQL.SceneAssignFileDocument);
  const [deleteFiles] = useMutation(GQL.DeleteFilesDocument);

  // After every file-level mutation we evict findScene + findScenes so the
  // current detail page (and any open list view) re-reads the new file list
  // and counts. The mutations themselves return Boolean (or the freshly
  // created scene), so the Apollo entity cache won't auto-merge on its own.
  const fileName = filePath.split("/").pop() || filePath;

  async function handleMakePrimary() {
    try {
      await updateScene({
        variables: { input: { id: sceneId, primary_file_id: fileId } },
        update(cache) {
          evictQueries(cache, [GQL.FindSceneDocument, GQL.FindScenesDocument]);
        },
      });
      toast.success(
        intl.formatMessage({
          id: "toast.primary_file_set",
          defaultMessage: "Primary file updated",
        }),
      );
    } catch (e) {
      toast.error(e);
    }
  }

  async function handleReassignTo(target: SceneSummary) {
    try {
      await assignFile({
        variables: { input: { scene_id: target.id, file_id: fileId } },
        update(cache) {
          evictQueries(cache, [GQL.FindSceneDocument, GQL.FindScenesDocument]);
        },
      });
      toast.success(
        intl.formatMessage(
          {
            id: "toast.file_reassigned",
            defaultMessage: "File reassigned to “{name}”",
          },
          { name: target.title },
        ),
      );
    } catch (e) {
      toast.error(e);
    }
  }

  // Pre-fill the split sheet from the parent scene. The new scene shares
  // the parent's metadata (title, code, performers, etc.) but NOT its
  // stash-box identities — those reference the parent specifically — and
  // starts unorganized so the user is prompted to review.
  const splitInitialValues = (() => {
    const v = sceneToFormValues(scene);
    return { ...v, stash_ids: [], organized: false };
  })();

  async function handleDelete() {
    try {
      await deleteFiles({
        variables: { ids: [fileId] },
        update(cache) {
          evictQueries(cache, [GQL.FindSceneDocument, GQL.FindScenesDocument]);
        },
      });
      toast.success(
        intl.formatMessage({
          id: "toast.file_deleted",
          defaultMessage: "File deleted",
        }),
      );
    } catch (e) {
      toast.error(e);
      throw e;
    }
  }

  return (
    <>
      <DropdownMenu modal={false}>
        <DropdownMenuTrigger
          render={<Button variant="ghost" size="icon-sm" />}
          aria-label={intl.formatMessage({
            id: "actions.file_actions",
            defaultMessage: "File actions",
          })}
        >
          <EllipsisVerticalIcon className="size-4" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onClick={handleMakePrimary}>
            <StarIcon />
            {intl.formatMessage({
              id: "actions.make_primary",
              defaultMessage: "Make primary",
            })}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => setReassignOpen(true)}>
            <ArrowRightLeftIcon />
            {intl.formatMessage({
              id: "actions.reassign",
              defaultMessage: "Reassign…",
            })}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => setSplitOpen(true)}>
            <SplitSquareHorizontalIcon />
            {intl.formatMessage({
              id: "actions.split",
              defaultMessage: "Split to new scene…",
            })}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            variant="destructive"
            onClick={() => setDeleteOpen(true)}
          >
            <Trash2Icon />
            {intl.formatMessage({
              id: "actions.delete_file",
              defaultMessage: "Delete file",
            })}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <SceneSelectDialog
        open={reassignOpen}
        onOpenChange={setReassignOpen}
        title={intl.formatMessage({
          id: "dialogs.reassign_file_title",
          defaultMessage: "Reassign file to scene",
        })}
        confirmLabel={intl.formatMessage({
          id: "actions.reassign",
          defaultMessage: "Reassign",
        })}
        excludeIds={[sceneId]}
        onSelect={handleReassignTo}
      />

      <DeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        entityName={fileName}
        onConfirm={async () => {
          await handleDelete();
        }}
      />

      <SceneCreateSheet
        open={splitOpen}
        onOpenChange={setSplitOpen}
        initialValues={splitInitialValues}
        createInputExtras={{ file_ids: [fileId] }}
        onCreated={(newId) => {
          toast.success(
            intl.formatMessage({
              id: "toast.scene_split",
              defaultMessage: "Scene created from file",
            }),
          );
          // The file moves with the new scene, so the parent scene's file
          // list must refresh too.
          navigate({
            to: "/scenes/$sceneId",
            params: { sceneId: newId },
          });
        }}
      />
    </>
  );
}
