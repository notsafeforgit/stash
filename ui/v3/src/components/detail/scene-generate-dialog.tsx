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

interface SceneGenerateDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** One or more scenes to generate metadata for */
  sceneIds: string[];
  /**
   * When false, the marker-artifacts row is hidden — none of the selected
   * scenes have markers, so the task would be a no-op. Defaults to true
   * when omitted (e.g. bulk callers without per-scene info).
   */
  hasMarkers?: boolean;
  /**
   * When false, the interactive heatmap row is hidden — none of the
   * selected scenes are interactive, so the task would be a no-op.
   * Defaults to true when omitted.
   */
  hasInteractive?: boolean;
}

type Options = {
  covers: boolean;
  previews: boolean;
  imagePreviews: boolean;
  sprites: boolean;
  /**
   * Combined toggle for the three marker artifacts (preview videos,
   * animated image previews, screenshots). When on, all three sub-flags
   * are sent as true; when off, all three are false. The split between
   * them is rarely meaningful per-scene — collapsed for simpler UX.
   */
  markers: boolean;
  transcodes: boolean;
  forceTranscodes: boolean;
  phashes: boolean;
  interactiveHeatmapsSpeeds: boolean;
  overwrite: boolean;
};

const DEFAULTS: Options = {
  covers: false,
  previews: true,
  imagePreviews: false,
  sprites: true,
  markers: true,
  transcodes: false,
  forceTranscodes: false,
  phashes: true,
  interactiveHeatmapsSpeeds: false,
  overwrite: false,
};

export function SceneGenerateDialog({
  open,
  onOpenChange,
  sceneIds,
  hasMarkers = true,
  hasInteractive = true,
}: SceneGenerateDialogProps) {
  const intl = useIntl();
  const toast = useToast();
  // Single-entity generates force overwrite on — see image-generate-dialog
  // for the reasoning. Hide the checkbox when there's only one scene.
  // Force-transcodes hides under the same single-scene rationale: a
  // deliberate one-scene click should generate unconditionally rather
  // than expose a sub-toggle that's only meaningful at bulk scale.
  const isSingle = sceneIds.length === 1;
  const [options, setOptions] = useState<Options>(DEFAULTS);
  const [submitting, setSubmitting] = useState(false);
  const [generate] = useMutation(GQL.MetadataGenerateDocument);

  function set<K extends keyof Options>(key: K, value: Options[K]) {
    setOptions((prev) => ({ ...prev, [key]: value }));
  }

  async function handleGenerate() {
    setSubmitting(true);
    try {
      const markersOn = hasMarkers && options.markers;
      await generate({
        variables: {
          input: {
            sceneIDs: sceneIds,
            covers: options.covers,
            previews: options.previews,
            imagePreviews: options.previews && options.imagePreviews,
            sprites: options.sprites,
            markers: markersOn,
            markerImagePreviews: markersOn,
            markerScreenshots: markersOn,
            transcodes: options.transcodes,
            forceTranscodes: options.transcodes && options.forceTranscodes,
            phashes: options.phashes,
            interactiveHeatmapsSpeeds:
              hasInteractive && options.interactiveHeatmapsSpeeds,
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
                id: "dialogs.generate.scene_count",
                defaultMessage:
                  "{count, plural, one {1 scene} other {# scenes}}",
              },
              { count: sceneIds.length },
            )}
          </DialogDescription>
        </DialogHeader>

        <FieldGroup
          data-slot="checkbox-group"
          className="max-h-[55vh] overflow-y-auto -mx-1 px-1"
        >
          <Row
            label={intl.formatMessage({
              id: "dialogs.scene_gen.covers",
              defaultMessage: "Scene covers",
            })}
            checked={options.covers}
            onChange={(v) => set("covers", v)}
          />
          <Row
            label={intl.formatMessage({
              id: "dialogs.scene_gen.video_previews",
              defaultMessage: "Previews",
            })}
            checked={options.previews}
            onChange={(v) => set("previews", v)}
          />
          <Row
            label={intl.formatMessage({
              id: "dialogs.scene_gen.image_previews",
              defaultMessage: "Animated image previews",
            })}
            checked={options.imagePreviews}
            disabled={!options.previews}
            onChange={(v) => set("imagePreviews", v)}
            indented
          />
          <Row
            label={intl.formatMessage({
              id: "dialogs.scene_gen.sprites",
              defaultMessage: "Scene scrubber sprites",
            })}
            checked={options.sprites}
            onChange={(v) => set("sprites", v)}
          />
          {hasMarkers && (
            <Row
              label={intl.formatMessage({
                id: "dialogs.scene_gen.marker_artifacts",
                defaultMessage: "Marker artifacts",
              })}
              checked={options.markers}
              onChange={(v) => set("markers", v)}
            />
          )}
          <Row
            label={intl.formatMessage({
              id: "dialogs.scene_gen.transcodes",
              defaultMessage: "Transcodes",
            })}
            description={intl.formatMessage({
              id: "dialogs.scene_gen.transcodes_desc",
              defaultMessage:
                "Pre-generate browser-compatible MP4s for sources that aren't already streamable",
            })}
            checked={options.transcodes}
            onChange={(v) => set("transcodes", v)}
          />
          <Row
            label={intl.formatMessage({
              id: "dialogs.scene_gen.force_transcodes_v2",
              defaultMessage: "Transcode even if source is already streamable",
            })}
            description={intl.formatMessage({
              id: "dialogs.scene_gen.force_transcodes_desc",
              defaultMessage:
                "By default the transcode is skipped when the source video can already play in the browser. Tick this when the codec metadata is unreliable or you want a transcode regardless.",
            })}
            checked={options.forceTranscodes}
            disabled={!options.transcodes}
            onChange={(v) => set("forceTranscodes", v)}
            indented
          />
          <Row
            label={intl.formatMessage({
              id: "dialogs.scene_gen.phash",
              defaultMessage: "Video perceptual hashes",
            })}
            checked={options.phashes}
            onChange={(v) => set("phashes", v)}
          />
          {hasInteractive && (
            <Row
              label={intl.formatMessage({
                id: "dialogs.scene_gen.interactive_heatmap_speed",
                defaultMessage:
                  "Generate heatmaps and speeds for interactive scenes",
              })}
              checked={options.interactiveHeatmapsSpeeds}
              onChange={(v) => set("interactiveHeatmapsSpeeds", v)}
            />
          )}
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
  disabled = false,
  indented = false,
}: {
  label: string;
  /** Optional muted secondary line under the label. */
  description?: string;
  checked: boolean;
  onChange: (v: boolean) => void;
  disabled?: boolean;
  indented?: boolean;
}) {
  // Stable id per row so FieldLabel htmlFor wires to the Checkbox.
  const id = React.useId();
  return (
    <Field
      orientation="horizontal"
      className={
        "items-start" +
        (indented ? " pl-6" : "") +
        (disabled ? " opacity-50" : "")
      }
      data-disabled={disabled}
    >
      <Checkbox
        id={id}
        className="mt-0.5"
        checked={checked}
        onCheckedChange={(c) => onChange(c === true)}
        disabled={disabled}
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
