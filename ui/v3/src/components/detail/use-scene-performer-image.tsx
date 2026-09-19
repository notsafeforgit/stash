import { useState } from "react";
import { useMutation, useQuery } from "@apollo/client/react";
import { useIntl } from "react-intl";
import * as GQL from "@/core/generated-graphql";
import { useToast } from "@/hooks/toast";
import { QueryError } from "@/components/query-error";
import { SearchResultRow } from "@/components/scrape/search-results";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Empty, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Spinner } from "@/components/ui/spinner";

/** Only image_path is written back to Apollo; scene streams and open edits
 * must not be refetched when a performer image changes. */
export function useScenePerformerImage(
  sceneId: string,
  performerImageTargetId?: string,
) {
  const intl = useIntl();
  const toast = useToast();
  const [source, setSource] = useState<GQL.SceneImageInput | null>(null);
  const [updateImage, { loading }] = useMutation(
    GQL.PerformerUpdateImageDocument,
  );

  async function save(performerId: string, imageSource: GQL.SceneImageInput) {
    try {
      const result = await updateImage({
        variables: { id: performerId, image: { scene: imageSource } },
      });
      if (!result.data?.performerUpdate) {
        throw new Error(
          intl.formatMessage({
            id: "toast.performer_image_failed",
            defaultMessage: "Could not update performer image.",
          }),
        );
      }
      setSource(null);
      toast.success(
        intl.formatMessage({
          id: "toast.performer_image_set",
          defaultMessage: "Performer image updated.",
        }),
      );
    } catch (error) {
      toast.error(error);
    }
  }

  return {
    pending: loading,
    setFromScene: (at?: number) => {
      const imageSource = { id: sceneId, ...(at === undefined ? {} : { at }) };
      if (performerImageTargetId)
        void save(performerImageTargetId, imageSource);
      else setSource(imageSource);
    },
    dialog: source ? (
      <ScenePerformerImageDialog
        sceneId={source.id}
        pending={loading}
        onClose={() => setSource(null)}
        onSelect={(id) => void save(id, source)}
      />
    ) : null,
  };
}

function ScenePerformerImageDialog({
  sceneId,
  pending,
  onClose,
  onSelect,
}: {
  sceneId: string;
  pending: boolean;
  onClose: () => void;
  onSelect: (id: string) => void;
}) {
  const intl = useIntl();
  // Mobile cards omit performers. Fetch just the choices, never FindScene's
  // signed stream URLs, and only after the action is requested.
  const { data, loading, error, refetch } = useQuery(
    GQL.FindSceneImagePerformersDocument,
    {
      variables: { id: sceneId },
    },
  );
  const performers = data?.findScene?.performers ?? [];
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>
            {intl.formatMessage({
              id: "actions.set_as_performer_image",
              defaultMessage: "Set as performer image",
            })}
          </DialogTitle>
          <DialogDescription>
            {intl.formatMessage({
              id: "dialogs.scene_performer_image_select",
              defaultMessage:
                "Choose the performer whose image you want to replace.",
            })}
          </DialogDescription>
        </DialogHeader>
        {error ? (
          <QueryError error={error} retry={refetch} retrying={loading} />
        ) : loading ? (
          <Spinner />
        ) : performers.length === 0 ? (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>
                {intl.formatMessage({
                  id: "dialogs.scene_has_no_performers",
                  defaultMessage: "This scene has no performers.",
                })}
              </EmptyTitle>
            </EmptyHeader>
          </Empty>
        ) : (
          <div className="max-h-80 overflow-y-auto" aria-busy={pending}>
            {performers.map((performer) => (
              <SearchResultRow
                key={performer.id}
                primary={performer.name}
                subtitles={
                  performer.disambiguation
                    ? [performer.disambiguation]
                    : undefined
                }
                imageSrc={performer.image_path}
                disabled={pending}
                onClick={() => onSelect(performer.id)}
              />
            ))}
          </div>
        )}
        {pending && <Spinner />}
      </DialogContent>
    </Dialog>
  );
}
