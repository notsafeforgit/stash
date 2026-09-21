import { useApolloClient, useMutation } from "@apollo/client/react";
import { useIntl } from "react-intl";
import { DestructiveConfirmDialog } from "@/components/shared/destructive-confirm-dialog";
import * as GQL from "@/core/generated-graphql";
import { refreshSceneCoversAfterJob } from "@/core/scene-cover-job";
import {
  sceneGenerationScope,
  type SceneGenerationTarget,
} from "@/core/scene-generation-target";
import { useMsg } from "@/hooks/message";
import { useToast } from "@/hooks/toast";

export function SceneCoverResetDialog({
  open,
  onOpenChange,
  target,
  onQueued,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  target: SceneGenerationTarget;
  onQueued?: () => void;
}) {
  const client = useApolloClient();
  const intl = useIntl();
  const msg = useMsg();
  const toast = useToast();
  const [generate, { loading }] = useMutation(GQL.MetadataGenerateDocument);
  const empty = target.kind === "scenes" && target.ids.length === 0;
  const title = msg("scene_cover.reset", "Reset covers to default");

  async function reset() {
    if (loading || empty) return;
    try {
      const { data } = await generate({
        variables: {
          input: {
            covers: true,
            resetCoversToDefault: true,
            ...sceneGenerationScope(target),
          },
        },
      });
      if (!data?.metadataGenerate)
        throw new Error(
          msg(
            "toast.screenshot_generation_failed",
            "Screenshot generation failed",
          ),
        );
      void refreshSceneCoversAfterJob(
        client,
        target.kind === "scenes" ? target.ids : "cached",
        data.metadataGenerate,
      )
        .then((completion) => {
          if (completion.kind === "unavailable") toast.error(completion.error);
          else if (completion.job?.status === GQL.JobStatus.Failed)
            toast.error(completion.job.error);
        })
        .catch((error: unknown) => toast.error(error));
      toast.success(
        intl.formatMessage(
          {
            id: "config.tasks.added_job_to_queue",
            defaultMessage: "Added {operation_name} to job queue",
          },
          { operation_name: title },
        ),
      );
      onOpenChange(false);
      onQueued?.();
    } catch (error) {
      toast.error(error);
    }
  }

  return (
    <DestructiveConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={title}
      confirmText={title}
      busy={loading}
      disabled={empty}
      onConfirm={() => void reset()}
    >
      <div className="flex flex-col gap-3 text-sm">
        <p>
          {target.kind === "library"
            ? msg(
                "scene_cover.reset_library",
                "Reset covers for every scene in your library?",
              )
            : target.kind === "matching"
              ? intl.formatMessage(
                  {
                    id: "scene_cover.reset_matching",
                    defaultMessage:
                      "Reset covers for all {count, plural, one {# matching scene} other {# matching scenes}} across every page?",
                  },
                  { count: target.count },
                )
              : intl.formatMessage(
                  {
                    id: "scene_cover.reset_selected",
                    defaultMessage:
                      "Reset covers for {count, plural, one {# selected scene} other {# selected scenes}}?",
                  },
                  { count: target.ids.length },
                )}
        </p>
        <p>
          {msg(
            "scene_cover.reset_description",
            "Replace custom, uploaded and scraped covers with the frame 20% into each primary video, then rebuild their thumbnails. A failed generation keeps the existing cover. This cannot be undone.",
          )}
        </p>
      </div>
    </DestructiveConfirmDialog>
  );
}
