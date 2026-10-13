import { skipToken, useApolloClient, useQuery } from "@apollo/client/react";
import { useState } from "react";
import { useIntl } from "react-intl";
import * as GQL from "@/core/generated-graphql";
import { removeEntitiesFromCache } from "@/core/client";
import { affectedActiveQueries } from "@/core/mutation-invalidation";
import {
  BulkDeleteError,
  deleteInBatches,
  type DeleteProgress,
} from "@/core/bulk-delete";
import type { ListDeleteSelection } from "@/components/list/entity-list-types";
import { BulkApplyToggle } from "@/components/list/bulk-apply-toggle";
import { QueryError } from "@/components/query-error";
import { DeleteDialog, type DeleteOptions } from "./delete-dialog";

/** Mounted by the page so deleting a card cannot unmount the progress dialog. */
export function MarkerDeleteDialog({
  selection,
  onClose,
}: {
  selection: ListDeleteSelection;
  onClose: () => void;
}) {
  const intl = useIntl();
  const client = useApolloClient();
  const [applyToAll, setApplyToAll] = useState(false);
  const matching = selection.matching;
  const { data, loading, error, refetch } = useQuery(
    GQL.FindSceneMarkerIDsDocument,
    applyToAll && matching
      ? {
          variables: {
            filter: { ...matching.findFilter, page: 1, per_page: -1 },
            scene_marker_filter_ast: matching.filterAST,
          },
          fetchPolicy: "no-cache",
        }
      : skipToken,
  );
  const result = data?.findSceneMarkers;
  const ids = applyToAll
    ? (result?.scene_markers.map((marker) => marker.id) ?? [])
    : selection.ids;
  // Never silently delete a truncated result or substitute an empty filter.
  const incomplete =
    applyToAll &&
    (!matching || loading || !!error || !result || result.count !== ids.length);
  const count = applyToAll
    ? (result?.count ?? selection.totalCount ?? 0)
    : ids.length;

  async function confirmDelete(
    _options: DeleteOptions,
    onProgress: (progress: DeleteProgress) => void,
  ) {
    if (incomplete || !ids.length) return;
    let deletedIds: string[] = [];
    try {
      deletedIds = await deleteInBatches(
        ids,
        async (batch) => {
          const response = await client.mutate({
            mutation: GQL.SceneMarkersDestroyDocument,
            variables: { ids: batch },
          });
          if (response.data?.sceneMarkersDestroy !== true)
            throw new Error(
              intl.formatMessage({ id: "dialogs.delete_unconfirmed" }),
            );
        },
        onProgress,
      );
    } catch (failure) {
      if (failure instanceof BulkDeleteError) deletedIds = failure.completedIds;
      throw failure;
    } finally {
      removeEntitiesFromCache({
        cache: client.cache,
        typename: "SceneMarker",
        listFieldName: "findSceneMarkers",
        itemsField: "scene_markers",
        ids: deletedIds,
      });
      void client
        .refetchQueries({
          include: affectedActiveQueries(
            client,
            GQL.SceneMarkersDestroyDocument,
          ),
        })
        .catch((failure: unknown) =>
          console.error("Could not refresh markers after deletion", failure),
        );
    }
  }

  return (
    <DeleteDialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      entityCountLabel={intl.formatMessage(
        { id: "dialogs.delete_markers_count" },
        { count },
      )}
      confirmDisabled={incomplete || !ids.length}
      onConfirm={confirmDelete}
      renderOptions={(busy) => (
        <div className="flex flex-col gap-3">
          {matching && selection.totalCount !== undefined && (
            <BulkApplyToggle
              totalCount={selection.totalCount}
              checked={applyToAll}
              onCheckedChange={setApplyToAll}
              disabled={busy}
            />
          )}
          <p className="text-sm text-muted-foreground">
            {intl.formatMessage({
              id: applyToAll
                ? "dialogs.delete_markers_matching"
                : "dialogs.delete_markers_selected",
            })}
          </p>
          {applyToAll && error && (
            <QueryError
              error={error}
              retry={() => refetch()}
              retrying={loading}
            />
          )}
          {applyToAll && result && result.count !== ids.length && (
            <p role="alert">
              {intl.formatMessage({ id: "dialogs.delete_markers_incomplete" })}
            </p>
          )}
        </div>
      )}
    />
  );
}
