import React, { useEffect, useRef, useState } from "react";
import { useForm, useStore } from "@tanstack/react-form";
import { useIntl } from "react-intl";
import { useApolloClient, useMutation } from "@apollo/client/react";
import { refreshSceneCoversAfterJob } from "@/core/scene-cover-job";
import {
  sceneGenerationScope,
  type SceneGenerationTarget,
} from "@/core/scene-generation-target";
import { BulkApplyToggle } from "@/components/list/bulk-apply-toggle";
import type { BulkApplyTarget } from "@/components/list/list-provider";
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
import { useGenerateTaskOptions } from "src/hooks/use-generate-task-options";
import { SceneCoverResetDialog } from "./deferred-overlays";

interface SceneGenerateDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** One or more scenes to generate metadata for */
  sceneIds: string[];
  applyToAllTarget?: BulkApplyTarget;
  totalCount?: number;
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

function boolDefault(
  value: GQL.InputMaybe<boolean> | undefined,
  fallback: boolean,
) {
  return value ?? fallback;
}

function getMarkerOptions(input: GQL.GenerateMetadataInput) {
  const markers = boolDefault(input.markers, DEFAULTS.markers);
  const markerImagePreviews = boolDefault(
    input.markerImagePreviews,
    DEFAULTS.markers,
  );
  const markerScreenshots = boolDefault(
    input.markerScreenshots,
    DEFAULTS.markers,
  );

  return {
    markers,
    markerImagePreviews,
    markerScreenshots,
    any: markers || markerImagePreviews || markerScreenshots,
  };
}

function getSceneOptions(input: GQL.GenerateMetadataInput): Options {
  return {
    covers: boolDefault(input.covers, DEFAULTS.covers),
    previews: boolDefault(input.previews, DEFAULTS.previews),
    imagePreviews: boolDefault(input.imagePreviews, DEFAULTS.imagePreviews),
    sprites: boolDefault(input.sprites, DEFAULTS.sprites),
    markers: getMarkerOptions(input).any,
    transcodes: boolDefault(input.transcodes, DEFAULTS.transcodes),
    forceTranscodes: boolDefault(
      input.forceTranscodes,
      DEFAULTS.forceTranscodes,
    ),
    phashes: boolDefault(input.phashes, DEFAULTS.phashes),
    interactiveHeatmapsSpeeds: boolDefault(
      input.interactiveHeatmapsSpeeds,
      DEFAULTS.interactiveHeatmapsSpeeds,
    ),
    overwrite: boolDefault(input.overwrite, DEFAULTS.overwrite),
  };
}

export function SceneGenerateDialog({
  open,
  onOpenChange,
  sceneIds,
  applyToAllTarget,
  totalCount,
  hasMarkers = true,
  hasInteractive = true,
}: SceneGenerateDialogProps) {
  const client = useApolloClient();
  const intl = useIntl();
  const toast = useToast();
  // Snapshot the list scope when opening; background count/list updates must
  // not change a bulk target while its confirmation is being reviewed.
  const [scope, setScope] = useState({
    sceneIds,
    applyToAllTarget,
    totalCount,
  });
  const form = useForm({ defaultValues: { applyToAll: false } });
  const applyToAll = useStore(form.store, (state) => state.values.applyToAll);
  const wasOpen = useRef(false);
  useEffect(() => {
    if (open && !wasOpen.current) {
      setScope({ sceneIds: [...sceneIds], applyToAllTarget, totalCount });
      form.reset();
    }
    wasOpen.current = open;
  }, [open, form, sceneIds, applyToAllTarget, totalCount]);
  const target: SceneGenerationTarget =
    applyToAll && scope.applyToAllTarget && scope.totalCount !== undefined
      ? {
          kind: "matching",
          count: scope.totalCount,
          selection: {
            find_filter: scope.applyToAllTarget.findFilter,
            scene_filter_ast: scope.applyToAllTarget.filterAST,
          },
        }
      : { kind: "scenes", ids: scope.sceneIds };
  const count = target.kind === "scenes" ? target.ids.length : target.count;
  const matchingCount = scope.totalCount;
  const empty = count === 0;
  // Single-entity generates force overwrite on — see image-generate-dialog
  // for the reasoning. Hide the checkbox when there's only one scene.
  // Force-transcodes hides under the same single-scene rationale: a
  // deliberate one-scene click should generate unconditionally rather
  // than expose a sub-toggle that's only meaningful at bulk scale.
  const isSingle = target.kind === "scenes" && count === 1;
  const [generateOptions, setGenerateOptions] = useGenerateTaskOptions();
  const options = getSceneOptions(generateOptions);
  const markerOptions = getMarkerOptions(generateOptions);
  const [submitting, setSubmitting] = useState(false);
  const [resetOpen, setResetOpen] = useState(false);
  const [generate] = useMutation(GQL.MetadataGenerateDocument);

  function set<K extends keyof Options>(key: K, value: Options[K]) {
    if (key === "markers") {
      setGenerateOptions({
        ...generateOptions,
        markers: value,
        markerImagePreviews: value,
        markerScreenshots: value,
      });
      return;
    }

    setGenerateOptions({ ...generateOptions, [key]: value });
  }

  async function handleGenerate() {
    if (submitting || empty) return;
    setSubmitting(true);
    try {
      const result = await generate({
        variables: {
          input: {
            ...sceneGenerationScope(target),
            covers: options.covers,
            previews: options.previews,
            imagePreviews: options.previews && options.imagePreviews,
            sprites: options.sprites,
            markers: hasMarkers && markerOptions.markers,
            markerImagePreviews:
              hasMarkers && markerOptions.markerImagePreviews,
            markerScreenshots: hasMarkers && markerOptions.markerScreenshots,
            transcodes: options.transcodes,
            forceTranscodes: options.transcodes && options.forceTranscodes,
            phashes: options.phashes,
            interactiveHeatmapsSpeeds:
              hasInteractive && options.interactiveHeatmapsSpeeds,
            overwrite: isSingle ? true : options.overwrite,
          },
        },
      });
      const jobId = result.data?.metadataGenerate;
      if (options.covers && jobId) {
        void refreshSceneCoversAfterJob(
          client,
          target.kind === "scenes" ? target.ids : "cached",
          jobId,
        )
          .then((completion) => {
            if (completion.kind === "unavailable")
              toast.error(completion.error);
            else if (completion.job?.status === GQL.JobStatus.Failed)
              toast.error(completion.job.error);
          })
          .catch((error: unknown) => toast.error(error));
      }
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
    <>
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
                { count },
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
              description={intl.formatMessage({
                id: "scene_cover.generate_description",
                defaultMessage:
                  "Reuse recorded frames. Covers without a recorded timestamp use the default frame at 20% of the primary video. Changed or unavailable recorded sources are reported in the task log.",
              })}
              checked={options.covers}
              onChange={(v) => set("covers", v)}
            />
            <Button
              type="button"
              variant="outline"
              className="self-start"
              onClick={() => setResetOpen(true)}
              disabled={submitting || empty}
            >
              {intl.formatMessage({
                id: "scene_cover.reset",
                defaultMessage: "Reset covers to default",
              })}
            </Button>
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
                defaultMessage:
                  "Transcode even if source is already streamable",
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

          {scope.applyToAllTarget &&
            matchingCount !== undefined &&
            matchingCount > scope.sceneIds.length && (
              <form.Field name="applyToAll">
                {(field) => (
                  <BulkApplyToggle
                    totalCount={matchingCount}
                    checked={field.state.value}
                    disabled={submitting}
                    onCheckedChange={field.handleChange}
                  />
                )}
              </form.Field>
            )}

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
            <Button
              size="sm"
              disabled={submitting || empty}
              onClick={handleGenerate}
            >
              {submitting ? <Spinner className="size-4" /> : <Cog />}
              {intl.formatMessage({
                id: "actions.generate",
                defaultMessage: "Generate",
              })}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <SceneCoverResetDialog
        open={resetOpen}
        onOpenChange={setResetOpen}
        target={target}
        onQueued={() => onOpenChange(false)}
      />
    </>
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
