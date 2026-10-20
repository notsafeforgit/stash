import { useApolloClient } from "@apollo/client/react";
import { useIntl } from "react-intl";
import {
  BulkDeleteError,
  deleteInBatches,
  type DeleteProgress,
} from "@/core/bulk-delete";
import { removeEntitiesFromCache } from "@/core/client";
import * as GQL from "@/core/generated-graphql";
import { affectedActiveQueries } from "@/core/mutation-invalidation";
import type { DeleteOptions } from "./delete-dialog";
import { useListContextOptional } from "@/components/list/list-provider";

export function useBulkMediaDelete(kind: "Scene" | "Image") {
  const client = useApolloClient();
  const intl = useIntl();
  const { onRemoveSelection } = useListContextOptional();

  return async (
    ids: readonly string[],
    options: DeleteOptions,
    onProgress: (progress: DeleteProgress) => void,
  ) => {
    let deletedIds: string[] = [];
    try {
      deletedIds = await deleteInBatches(
        ids,
        async (batch) => {
          const variables = {
            ids: batch,
            delete_file: options.deleteFile,
            delete_generated: options.deleteGenerated,
          };
          const confirmed =
            kind === "Scene"
              ? (
                  await client.mutate({
                    mutation: GQL.ScenesDestroyDocument,
                    variables,
                  })
                ).data?.scenesDestroy
              : (
                  await client.mutate({
                    mutation: GQL.ImagesDestroyDocument,
                    variables,
                  })
                ).data?.imagesDestroy;
          if (confirmed !== true) {
            throw new Error(
              intl.formatMessage({
                id: "dialogs.delete_unconfirmed",
                defaultMessage: "The server did not confirm deletion.",
              }),
            );
          }
        },
        onProgress,
      );
    } catch (error) {
      if (error instanceof BulkDeleteError) deletedIds = error.completedIds;
      throw error;
    } finally {
      onRemoveSelection?.(deletedIds);
      // Keep the card hosting the dialog mounted throughout the operation.
      // Reconcile once at the end (also on a partial/ambiguous failure), rather
      // than unmounting the progress dialog or refetching after every batch.
      removeEntitiesFromCache({
        cache: client.cache,
        typename: kind,
        listFieldName: kind === "Scene" ? "findScenes" : "findImages",
        itemsField: kind === "Scene" ? "scenes" : "images",
        ids: deletedIds,
      });
      void client
        .refetchQueries({
          include: affectedActiveQueries(
            client,
            kind === "Scene"
              ? GQL.ScenesDestroyDocument
              : GQL.ImagesDestroyDocument,
          ),
        })
        .catch((error: unknown) =>
          console.error("Could not refresh library after deletion", error),
        );
    }
  };
}
