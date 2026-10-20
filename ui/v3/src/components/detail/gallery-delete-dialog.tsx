import { useId } from "react";
import { useApolloClient, useQuery } from "@apollo/client/react";
import { useForm, useStore } from "@tanstack/react-form";
import { useIntl } from "react-intl";
import * as GQL from "@/core/generated-graphql";
import { removeEntitiesFromCache } from "@/core/client";
import { affectedActiveQueries } from "@/core/mutation-invalidation";
import { QueryError } from "@/components/query-error";
import { useListContextOptional } from "@/components/list/list-provider";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Spinner } from "@/components/ui/spinner";
import { DeleteDialog, type DeleteOptions } from "./delete-dialog";

interface GalleryDeleteDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  ids: readonly string[];
  entityName?: string;
  onDeleted?: () => void;
}

/** One confirmation for cards, details and edit sheets. Mount on opening so
 * destructive choices never carry over to another gallery or a later attempt. */
export function GalleryDeleteDialog(props: GalleryDeleteDialogProps) {
  return props.open ? <GalleryDeleteConfirmation {...props} /> : null;
}

function GalleryDeleteConfirmation({
  ids,
  entityName,
  onOpenChange,
  onDeleted,
}: GalleryDeleteDialogProps) {
  const intl = useIntl();
  const { onRemoveSelection } = useListContextOptional();
  const client = useApolloClient();
  const controlID = useId();
  const uniqueIDs = [...new Set(ids)];
  const form = useForm({ defaultValues: { media: false, shared: false } });
  const values = useStore(form.store, (state) => state.values);
  const { data, loading, error, refetch } = useQuery(
    GQL.GalleryDeletePreviewDocument,
    {
      variables: { ids: uniqueIDs },
      fetchPolicy: "no-cache",
    },
  );
  const preview = data?.galleryDeletePreview;
  const sharedImages = preview?.shared_image_count ?? 0;
  const sharedScenes = preview?.shared_scene_count ?? 0;
  const imageCount =
    (preview?.image_count ?? 0) - (values.shared ? 0 : sharedImages);
  const sceneCount =
    (preview?.scene_count ?? 0) - (values.shared ? 0 : sharedScenes);

  async function confirm(options: DeleteOptions) {
    if (!preview || loading || error) return;
    const result = await client.mutate({
      mutation: GQL.GalleryDestroyDocument,
      variables: {
        ids: uniqueIDs,
        delete_media: values.media,
        delete_shared_media: values.media && values.shared,
        delete_file: values.media && options.deleteFile,
        delete_generated: values.media && options.deleteGenerated,
      },
    });
    if (result.data?.galleryDestroy !== true) {
      throw new Error(intl.formatMessage({ id: "dialogs.delete_unconfirmed" }));
    }
    onRemoveSelection?.(uniqueIDs);
    removeEntitiesFromCache({
      cache: client.cache,
      typename: "Gallery",
      listFieldName: "findGalleries",
      itemsField: "galleries",
      ids: uniqueIDs,
    });
    // Refresh other live views, including scene markers, without holding the
    // confirmation open while those independent requests complete.
    void client
      .refetchQueries({
        include: affectedActiveQueries(client, GQL.GalleryDestroyDocument),
      })
      .catch(console.error);
    onDeleted?.();
  }

  return (
    <DeleteDialog
      open
      onOpenChange={onOpenChange}
      entityName={entityName}
      entityCountLabel={intl.formatMessage(
        {
          id: "dialogs.delete_galleries_count",
          defaultMessage: "{count} galleries",
        },
        { count: uniqueIDs.length },
      )}
      description={intl.formatMessage({
        id: values.media
          ? "gallery_deletion.media_help"
          : "gallery_deletion.gallery_help",
        defaultMessage: values.media
          ? "Deleted images and scenes are removed from the library and every gallery that uses them."
          : "Remove only the galleries and their associations. Keep every image, scene and file.",
      })}
      showFileOptions={values.media}
      deleteFileLabel={intl.formatMessage({
        id: "gallery_deletion.files",
        defaultMessage: "Also delete the media files from disk",
      })}
      confirmDisabled={loading || !!error || !preview || !uniqueIDs.length}
      onConfirm={confirm}
      renderOptions={(busy) => (
        <FieldGroup>
          {loading && <Spinner />}
          {error && (
            <QueryError
              error={error}
              retry={() => refetch()}
              retrying={loading}
            />
          )}
          {preview && (
            <p className="text-sm text-muted-foreground" aria-live="polite">
              {intl.formatMessage(
                {
                  id: "gallery_deletion.members",
                  defaultMessage:
                    "Contains {images, plural, one {# image} other {# images}} and {scenes, plural, one {# scene} other {# scenes}} (each counted once).",
                },
                { images: preview.image_count, scenes: preview.scene_count },
              )}
            </p>
          )}
          {!!preview?.source_count && (
            <p className="text-sm text-muted-foreground">
              {intl.formatMessage({
                id: "gallery_deletion.source_help",
                defaultMessage:
                  "Source post metadata stays available. Deleted source albums are not automatically recreated.",
              })}
            </p>
          )}
          <form.Field name="media">
            {(field) => (
              <Field orientation="horizontal" data-disabled={busy}>
                <Checkbox
                  id={`${controlID}-media`}
                  checked={field.state.value}
                  onCheckedChange={field.handleChange}
                  disabled={busy}
                />
                <FieldLabel htmlFor={`${controlID}-media`}>
                  {intl.formatMessage({
                    id: "gallery_deletion.media",
                    defaultMessage:
                      "Also delete the gallery’s images and scenes",
                  })}
                </FieldLabel>
              </Field>
            )}
          </form.Field>
          {values.media && (
            <>
              {sharedImages + sharedScenes > 0 && (
                <form.Field name="shared">
                  {(field) => (
                    <Field data-disabled={busy}>
                      <Field orientation="horizontal">
                        <Checkbox
                          id={`${controlID}-shared`}
                          checked={field.state.value}
                          onCheckedChange={field.handleChange}
                          disabled={busy}
                        />
                        <FieldLabel htmlFor={`${controlID}-shared`}>
                          {intl.formatMessage({
                            id: "gallery_deletion.shared",
                            defaultMessage:
                              "Also delete media used by other galleries",
                          })}
                        </FieldLabel>
                      </Field>
                      <FieldDescription>
                        {intl.formatMessage(
                          {
                            id: "gallery_deletion.shared_help",
                            defaultMessage:
                              "{images, plural, one {# image} other {# images}} and {scenes, plural, one {# scene} other {# scenes}} also belong to galleries outside this selection. They are kept unless you select this option.",
                          },
                          { images: sharedImages, scenes: sharedScenes },
                        )}
                      </FieldDescription>
                    </Field>
                  )}
                </form.Field>
              )}
              <p className="text-sm" aria-live="polite">
                {intl.formatMessage(
                  {
                    id: "gallery_deletion.removing",
                    defaultMessage:
                      "Will delete {images, plural, one {# image} other {# images}} and {scenes, plural, one {# scene} other {# scenes}} from the library.",
                  },
                  { images: imageCount, scenes: sceneCount },
                )}
              </p>
              <p className="text-sm text-muted-foreground">
                {intl.formatMessage({
                  id: "gallery_deletion.zip_help",
                  defaultMessage:
                    "ZIP files still used by another gallery or retained media are kept. Individual files inside a retained ZIP cannot be removed.",
                })}
              </p>
            </>
          )}
        </FieldGroup>
      )}
    />
  );
}
