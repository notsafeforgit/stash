import React, { useState } from "react";
import { useIntl } from "react-intl";
import { useMutation } from "@apollo/client/react";
import { Cog } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
import { Checkbox } from "src/components/ui/checkbox";
import { Spinner } from "src/components/ui/spinner";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "src/components/ui/dialog";
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldSeparator,
} from "src/components/ui/field";
import { useToast } from "src/hooks/toast";
import { useConfigurationContext } from "src/hooks/config";
import { useGenerateTaskOptions } from "src/hooks/use-generate-task-options";

interface ImageGenerateDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** One or more images to generate metadata for */
  imageIds: string[];
}

type Options = {
  imageThumbnails: boolean;
  clipPreviews: boolean;
  imagePhashes: boolean;
  overwrite: boolean;
};

const DEFAULTS: Options = {
  imageThumbnails: true,
  clipPreviews: false,
  imagePhashes: true,
  overwrite: false,
};

function boolDefault(
  value: GQL.InputMaybe<boolean> | undefined,
  fallback: boolean,
) {
  return value ?? fallback;
}

function getImageOptions(input: GQL.GenerateMetadataInput): Options {
  return {
    imageThumbnails: boolDefault(
      input.imageThumbnails,
      DEFAULTS.imageThumbnails,
    ),
    clipPreviews: boolDefault(input.clipPreviews, DEFAULTS.clipPreviews),
    imagePhashes: boolDefault(input.imagePhashes, DEFAULTS.imagePhashes),
    overwrite: boolDefault(input.overwrite, DEFAULTS.overwrite),
  };
}

// For single-entity generates, overwrite is forced on — running the task on
// one item the user explicitly picked should always refresh the artifact
// rather than no-op when one's already on disk. The checkbox is hidden in
// that case; bulk generates keep it (the "fill in only missing artifacts"
// flow remains useful at scale).

export function ImageGenerateDialog({
  open,
  onOpenChange,
  imageIds,
}: ImageGenerateDialogProps) {
  const intl = useIntl();
  const toast = useToast();
  const { configuration } = useConfigurationContext();
  // Image clips only get created when (a) the global
  // `createImageClipsFromVideos` flag is on AND (b) at least one library
  // has `excludeVideo: true` — that's the gate the scan task uses
  // (`internal/manager/manager_tasks.go:24,32`). If either condition is
  // false, no image entry in the library can be a video, so the
  // clip-previews task is a no-op everywhere — hide the row.
  const showClipPreviews =
    configuration.general.createImageClipsFromVideos === true &&
    configuration.general.stashes.some((s) => s.excludeVideo);
  const isSingle = imageIds.length === 1;
  const [generateOptions, setGenerateOptions] = useGenerateTaskOptions();
  const options = getImageOptions(generateOptions);
  const [submitting, setSubmitting] = useState(false);
  const [generate] = useMutation(GQL.MetadataGenerateDocument);

  function set<K extends keyof Options>(key: K, value: Options[K]) {
    setGenerateOptions({ ...generateOptions, [key]: value });
  }

  async function handleGenerate() {
    setSubmitting(true);
    try {
      await generate({
        variables: {
          input: {
            imageIDs: imageIds,
            imageThumbnails: options.imageThumbnails,
            clipPreviews: showClipPreviews && options.clipPreviews,
            imagePhashes: options.imagePhashes,
            overwrite: isSingle ? true : options.overwrite,
          },
        },
      });
      toast.success(
        intl.formatMessage(
          {
            id: "config.tasks.added_job_to_queue",
            defaultMessage: "Added {operation_name} to job queue",
          },
          {
            operation_name: intl.formatMessage({
              id: "actions.generate",
              defaultMessage: "Generate",
            }),
          },
        ),
      );
      onOpenChange(false);
    } catch (e) {
      toast.error(e);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={submitting ? () => {} : onOpenChange}>
      <DialogContent showCloseButton={false} className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {intl.formatMessage({
              id: "actions.generate",
              defaultMessage: "Generate",
            })}
          </DialogTitle>
          <DialogDescription>
            {intl.formatMessage(
              {
                id: "dialogs.generate.image_count",
                defaultMessage:
                  "{count, plural, one {1 image} other {# images}}",
              },
              { count: imageIds.length },
            )}
          </DialogDescription>
        </DialogHeader>

        <FieldGroup data-slot="checkbox-group">
          <Row
            label={intl.formatMessage({
              id: "dialogs.scene_gen.image_thumbnails",
              defaultMessage: "Image thumbnails",
            })}
            checked={options.imageThumbnails}
            onChange={(v) => set("imageThumbnails", v)}
          />
          {showClipPreviews && (
            <Row
              label={intl.formatMessage({
                id: "dialogs.scene_gen.clip_previews",
                defaultMessage: "Image clip previews",
              })}
              checked={options.clipPreviews}
              onChange={(v) => set("clipPreviews", v)}
            />
          )}
          <Row
            label={intl.formatMessage({
              id: "dialogs.scene_gen.image_phash",
              defaultMessage: "Image perceptual hashes",
            })}
            checked={options.imagePhashes}
            onChange={(v) => set("imagePhashes", v)}
          />
          {!isSingle && (
            <>
              <FieldSeparator />
              <Row
                label={intl.formatMessage({
                  id: "dialogs.scene_gen.overwrite_v2",
                  defaultMessage: "Replace existing artifacts",
                })}
                description={intl.formatMessage({
                  id: "dialogs.scene_gen.overwrite_desc",
                  defaultMessage:
                    "By default an artifact that's already on disk is left alone. Tick this to regenerate it.",
                })}
                checked={options.overwrite}
                onChange={(v) => set("overwrite", v)}
              />
            </>
          )}
        </FieldGroup>

        <DialogFooter>
          <Button
            variant="outline"
            size="sm"
            disabled={submitting}
            onClick={() => onOpenChange(false)}
          >
            {intl.formatMessage({
              id: "actions.cancel",
              defaultMessage: "Cancel",
            })}
          </Button>
          <Button size="sm" disabled={submitting} onClick={handleGenerate}>
            {submitting ? <Spinner className="size-4" /> : <Cog />}
            {intl.formatMessage({
              id: "actions.generate",
              defaultMessage: "Generate",
            })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function Row({
  label,
  description,
  checked,
  onChange,
}: {
  label: string;
  description?: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  const id = React.useId();
  return (
    <Field orientation="horizontal" className="items-start">
      <Checkbox
        id={id}
        className="mt-0.5"
        checked={checked}
        onCheckedChange={(c) => onChange(c === true)}
      />
      <div className="flex flex-col">
        <FieldLabel htmlFor={id} className="font-normal">
          {label}
        </FieldLabel>
        {description && (
          <FieldDescription className="text-xs">{description}</FieldDescription>
        )}
      </div>
    </Field>
  );
}
