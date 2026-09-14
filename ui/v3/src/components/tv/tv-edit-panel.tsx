import { FormattedMessage } from "react-intl";
import { useMsg } from "@/hooks/message";
import { useForm, useStore } from "@tanstack/react-form";
import { MarkerEditForm } from "@/components/detail/marker-edit-form";
import {
  DeleteDialog,
  DeleteFilesList,
} from "@/components/detail/delete-dialog";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel, FieldGroup } from "@/components/ui/field";
import { TvTagPicker, TvTagsPicker } from "./tv-tag-picker";
import type { TvScene, useTvMutations } from "./use-tv-mutations";
import type { TvFeedItem } from "@/core/tv/feed-state";
import { objectTitle } from "@/core/files";

export type TvEditTarget = {
  kind: "tags" | "marker" | "delete";
  scene: TvScene;
  item: TvFeedItem;
  position: number;
};
type Mutations = ReturnType<typeof useTvMutations>;
function TagsForm({
  target,
  mutations,
  close,
}: {
  target: TvEditTarget;
  mutations: Mutations;
  close: () => void;
}) {
  const msg = useMsg();
  const marker =
    target.item.kind === "marker"
      ? target.scene.scene_markers.find(
          (marker) => marker.id === target.item.id,
        )
      : undefined;
  const form = useForm({
    defaultValues: {
      tags: (marker?.tags ?? target.scene.tags).map((tag) => ({
        id: tag.id,
        name: tag.name,
      })),
      primary: marker?.primary_tag ?? null,
    },
    onSubmit: async ({ value }) => {
      if (target.item.kind === "marker" && !value.primary) return;
      if (
        await mutations.run(target.item.key, () =>
          mutations.updateTags(
            target.scene,
            target.item,
            value.tags.map((tag) => tag.id),
            value.primary?.id,
          ),
        )
      )
        close();
    },
  });
  const values = useStore(form.store, (state) => state.values);
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        void form.handleSubmit();
      }}
    >
      <FieldGroup>
        {marker && (
          <Field>
            <FieldLabel>
              <FormattedMessage
                id="tv.text.primary_tag"
                defaultMessage="Primary tag"
              />
            </FieldLabel>
            <TvTagPicker
              label={msg("tv.text.primary_tag", "Primary tag")}
              value={values.primary}
              onChange={(tag) => form.setFieldValue("primary", tag)}
            />
          </Field>
        )}
        <Field>
          <FieldLabel>
            {marker
              ? msg("tv.text.additional_tags", "Additional tags")
              : msg("tv.text.scene_tags", "Scene tags")}
          </FieldLabel>
          <TvTagsPicker
            label={msg("tv.text.tags", "Tags")}
            value={values.tags}
            onChange={(tags) => form.setFieldValue("tags", tags)}
          />
        </Field>
        <Button
          type="submit"
          disabled={
            mutations.busy || (target.item.kind === "marker" && !values.primary)
          }
        >
          <FormattedMessage id="tv.text.save_tags" defaultMessage="Save tags" />
        </Button>
      </FieldGroup>
    </form>
  );
}

export default function TvEditPanel({
  target,
  mutations,
  close,
}: {
  target: TvEditTarget;
  mutations: Mutations;
  close: () => void;
}) {
  const msg = useMsg();
  const title =
    target.item.kind === "marker"
      ? (target.scene.scene_markers.find(
          (marker) => marker.id === target.item.id,
        )?.title ?? "Marker")
      : objectTitle(target.scene);
  if (target.kind === "delete")
    return (
      <DeleteDialog
        open
        onOpenChange={(open) => {
          if (!open) close();
        }}
        entityName={title}
        showFileOptions={target.item.kind === "scene"}
        details={
          <DeleteFilesList
            paths={target.scene.files.map((file) => file.path)}
          />
        }
        onConfirm={(options) => mutations.destroy(target.item, options)}
      />
    );
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !mutations.busy) close();
      }}
    >
      <DialogContent className="max-h-[calc(100%-2rem)] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {target.kind === "marker"
              ? msg("tv.text.create_marker", "Create marker")
              : msg("tv.text.edit_tags", "Edit tags")}
          </DialogTitle>
          <DialogDescription>{title}</DialogDescription>
        </DialogHeader>
        {target.kind === "marker" ? (
          <MarkerEditForm
            sceneId={target.scene.id}
            marker={null}
            initialTimestamp={target.position}
            maxTimestamp={target.scene.files[0]?.duration}
            getCurrentTime={() => target.position}
            onSaved={() => {
              void mutations.refresh(target.scene.id);
              close();
            }}
            onCancel={close}
          />
        ) : (
          <TagsForm target={target} mutations={mutations} close={close} />
        )}
      </DialogContent>
    </Dialog>
  );
}
