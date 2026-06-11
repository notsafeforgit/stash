import { useState } from "react";
import { FormattedMessage, useIntl } from "react-intl";
import { useMutation } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
import { useToast } from "src/hooks/toast";
import { useConfigurationContext } from "src/hooks/config";
import { useTaskOptions } from "src/hooks/use-task-options";
import { withoutTypename } from "src/utils/data";
import { AutoTagWarning } from "src/components/shared/auto-tag-warning";
import { DestructiveConfirmDialog } from "src/components/shared/destructive-confirm-dialog";
import {
  VideoPreviewInput,
  type VideoPreviewSettingsInput,
} from "src/components/shared/video-preview-input";
import { IdentifyDialog } from "./identify-dialog";
import { SelectivePathsButton } from "./selective-paths-button";
import {
  TaskGroup,
  TaskOptionToggle,
  TaskSectionHeading,
} from "./task-section";

function ScanOptionsForm({
  options,
  setOptions,
}: {
  options: GQL.ScanMetadataInput;
  setOptions: (s: GQL.ScanMetadataInput) => void;
}) {
  function set(input: Partial<GQL.ScanMetadataInput>) {
    setOptions({ ...options, ...input });
  }
  return (
    <>
      <TaskOptionToggle
        id="scan-generate-covers"
        label="Generate scene covers"
        checked={options.scanGenerateCovers ?? true}
        onChange={(v) => set({ scanGenerateCovers: v })}
      />
      <TaskOptionToggle
        id="scan-generate-previews"
        label="Generate video previews"
        description="Generate video previews which play when hovering over a scene"
        checked={options.scanGeneratePreviews ?? false}
        onChange={(v) => set({ scanGeneratePreviews: v })}
      />
      <TaskOptionToggle
        id="scan-generate-image-previews"
        label="Generate animated image previews"
        description="Also generate animated (webp) previews"
        checked={options.scanGenerateImagePreviews ?? false}
        disabled={!options.scanGeneratePreviews}
        onChange={(v) => set({ scanGenerateImagePreviews: v })}
        className="ml-6"
      />
      <TaskOptionToggle
        id="scan-generate-sprites"
        label="Generate scrubber sprites"
        checked={options.scanGenerateSprites ?? false}
        onChange={(v) => set({ scanGenerateSprites: v })}
      />
      <TaskOptionToggle
        id="scan-generate-phashes"
        label="Generate video perceptual hashes"
        checked={options.scanGeneratePhashes ?? false}
        onChange={(v) => set({ scanGeneratePhashes: v })}
      />
      <TaskOptionToggle
        id="scan-generate-thumbnails"
        label="Generate thumbnails for images"
        checked={options.scanGenerateThumbnails ?? false}
        onChange={(v) => set({ scanGenerateThumbnails: v })}
      />
      <TaskOptionToggle
        id="scan-generate-image-phashes"
        label="Generate image perceptual hashes"
        checked={options.scanGenerateImagePhashes ?? false}
        onChange={(v) => set({ scanGenerateImagePhashes: v })}
      />
      <TaskOptionToggle
        id="scan-generate-clip-previews"
        label="Generate previews for image clips"
        checked={options.scanGenerateClipPreviews ?? false}
        onChange={(v) => set({ scanGenerateClipPreviews: v })}
      />
      <TaskOptionToggle
        id="scan-rescan"
        label="Rescan files"
        description="Force every file through the full scan path: re-read fingerprints, re-check zip contents, and run every selected artifact generator above (even for files that already have the artifact). Required if you want Scan to backfill missing covers / sprites / phashes / previews for files that have already been scanned. Slower. To backfill without recomputing fingerprints, use Generate instead."
        checked={options.rescan ?? false}
        onChange={(v) => set({ rescan: v })}
      />
    </>
  );
}

function AutoTagOptionsForm({
  options,
  setOptions,
}: {
  options: GQL.AutoTagMetadataInput;
  setOptions: (s: GQL.AutoTagMetadataInput) => void;
}) {
  const set = (key: "performers" | "studios" | "tags", v: boolean) =>
    setOptions({ ...options, [key]: v ? ["*"] : [] });
  return (
    <>
      <TaskOptionToggle
        id="autotag-performers"
        label="Performers"
        checked={!!options.performers?.length}
        onChange={(v) => set("performers", v)}
      />
      <TaskOptionToggle
        id="autotag-studios"
        label="Studios"
        checked={!!options.studios?.length}
        onChange={(v) => set("studios", v)}
      />
      <TaskOptionToggle
        id="autotag-tags"
        label="Tags"
        checked={!!options.tags?.length}
        onChange={(v) => set("tags", v)}
      />
    </>
  );
}

function GenerateOptionsForm({
  options,
  setOptions,
}: {
  options: GQL.GenerateMetadataInput;
  setOptions: (s: GQL.GenerateMetadataInput) => void;
}) {
  function set(input: Partial<GQL.GenerateMetadataInput>) {
    setOptions({ ...options, ...input });
  }
  return (
    <>
      <TaskOptionToggle
        id="gen-covers"
        label="Scene covers"
        checked={options.covers ?? false}
        onChange={(v) => set({ covers: v })}
      />
      <TaskOptionToggle
        id="gen-previews"
        label="Video previews"
        checked={options.previews ?? false}
        onChange={(v) => set({ previews: v })}
      />
      <TaskOptionToggle
        id="gen-image-previews"
        label="Animated image previews"
        checked={options.imagePreviews ?? false}
        disabled={!options.previews}
        onChange={(v) => set({ imagePreviews: v })}
        className="ml-6"
      />
      <TaskOptionToggle
        id="gen-sprites"
        label="Scrubber sprites"
        checked={options.sprites ?? false}
        onChange={(v) => set({ sprites: v })}
      />
      <TaskOptionToggle
        id="gen-markers"
        label="Markers"
        checked={options.markers ?? false}
        onChange={(v) => set({ markers: v })}
      />
      <TaskOptionToggle
        id="gen-marker-image-previews"
        label="Marker image previews"
        checked={options.markerImagePreviews ?? false}
        onChange={(v) => set({ markerImagePreviews: v })}
        className="ml-6"
      />
      <TaskOptionToggle
        id="gen-marker-screenshots"
        label="Marker screenshots"
        checked={options.markerScreenshots ?? false}
        onChange={(v) => set({ markerScreenshots: v })}
      />
      <TaskOptionToggle
        id="gen-transcodes"
        label="Transcodes"
        checked={options.transcodes ?? false}
        onChange={(v) => set({ transcodes: v })}
      />
      <TaskOptionToggle
        id="gen-phashes"
        label="Perceptual hashes"
        checked={options.phashes ?? false}
        onChange={(v) => set({ phashes: v })}
      />
      <TaskOptionToggle
        id="gen-interactive-heatmaps"
        label="Interactive heatmaps / speeds"
        checked={options.interactiveHeatmapsSpeeds ?? false}
        onChange={(v) => set({ interactiveHeatmapsSpeeds: v })}
      />
      <TaskOptionToggle
        id="gen-clip-previews"
        label="Image clip previews"
        checked={options.clipPreviews ?? false}
        onChange={(v) => set({ clipPreviews: v })}
      />
      <TaskOptionToggle
        id="gen-image-thumbnails"
        label="Image thumbnails"
        checked={options.imageThumbnails ?? false}
        onChange={(v) => set({ imageThumbnails: v })}
      />
      <TaskOptionToggle
        id="gen-image-phashes"
        label="Image perceptual hashes"
        checked={options.imagePhashes ?? false}
        onChange={(v) => set({ imagePhashes: v })}
      />
      <TaskOptionToggle
        id="gen-overwrite"
        label="Overwrite existing"
        checked={options.overwrite ?? false}
        onChange={(v) => set({ overwrite: v })}
      />
    </>
  );
}

export function LibraryTasks() {
  const intl = useIntl();
  const toast = useToast();
  const { configuration } = useConfigurationContext();

  const [scanOptions, setScanOptions] = useTaskOptions("scan", () =>
    configuration.defaults.scan
      ? withoutTypename(configuration.defaults.scan)
      : {
          scanGenerateCovers: true,
          scanGeneratePreviews: false,
          scanGenerateImagePreviews: false,
          scanGenerateSprites: false,
          scanGeneratePhashes: false,
          scanGenerateThumbnails: false,
          scanGenerateClipPreviews: false,
        },
  );

  const [autoTagOptions, setAutoTagOptions] = useTaskOptions("autoTag", () =>
    configuration.defaults.autoTag
      ? withoutTypename(configuration.defaults.autoTag)
      : { performers: ["*"], studios: ["*"], tags: ["*"] },
  );

  const [generateOptions, setGenerateOptions] = useTaskOptions(
    "generate",
    () =>
      configuration.defaults.generate
        ? withoutTypename(configuration.defaults.generate)
        : {
            covers: true,
            sprites: true,
            phashes: true,
            previews: true,
            markers: true,
          },
  );

  const [autoTagConfirmOpen, setAutoTagConfirmOpen] = useState(false);
  const [identifyOpen, setIdentifyOpen] = useState(false);
  const [generatePreviewOverrides, setGeneratePreviewOverrides] =
    useState<VideoPreviewSettingsInput>({});
  const [overridePreviewOptions, setOverridePreviewOptions] = useState(false);

  const [scan] = useMutation(GQL.MetadataScanDocument);
  const [generate] = useMutation(GQL.MetadataGenerateDocument);
  const [autoTag] = useMutation(GQL.MetadataAutoTagDocument);

  function added(operation: string) {
    toast.success(
      intl.formatMessage(
        {
          id: "config.tasks.added_job_to_queue",
          defaultMessage: "Added {operation_name} job to queue.",
        },
        { operation_name: operation },
      ),
    );
  }

  async function runScan(paths?: string[]) {
    try {
      await scan({ variables: { input: { ...scanOptions, paths } } });
      added(intl.formatMessage({ id: "actions.scan", defaultMessage: "Scan" }));
    } catch (e) {
      toast.error(e);
    }
  }

  async function runGenerate(
    paths?: string[],
    overrides?: Partial<GQL.GenerateMetadataInput>,
  ) {
    try {
      // Merge system-level preview generation config into previewOptions so
      // the server picks them up rather than defaulting (v2.5 LibraryTasks
      // onGenerateClicked behaviour). Per-run overrides win, then UI form
      // state, then system config.
      const general = configuration.general;
      const formPreview = generateOptions.previewOptions ?? {};
      const previewOptions: GQL.GeneratePreviewOptionsInput = {
        previewSegments: formPreview.previewSegments ?? general.previewSegments,
        previewSegmentDuration:
          formPreview.previewSegmentDuration ?? general.previewSegmentDuration,
        previewExcludeStart:
          formPreview.previewExcludeStart ?? general.previewExcludeStart,
        previewExcludeEnd:
          formPreview.previewExcludeEnd ?? general.previewExcludeEnd,
        previewPreset: formPreview.previewPreset ?? general.previewPreset,
        ...overrides?.previewOptions,
      };
      await generate({
        variables: {
          input: {
            ...generateOptions,
            ...overrides,
            previewOptions,
            paths,
          },
        },
      });
      added(
        intl.formatMessage({
          id: "actions.generate",
          defaultMessage: "Generate",
        }),
      );
    } catch (e) {
      toast.error(e);
    }
  }

  async function runAutoTag(paths?: string[]) {
    try {
      await autoTag({ variables: { input: { ...autoTagOptions, paths } } });
      added(
        intl.formatMessage({
          id: "actions.auto_tag",
          defaultMessage: "Auto tag",
        }),
      );
    } catch (e) {
      toast.error(e);
    } finally {
      setAutoTagConfirmOpen(false);
    }
  }

  return (
    <>
      <TaskGroup
        title={intl.formatMessage({ id: "library", defaultMessage: "Library" })}
      >
        <TaskSectionHeading
          title={<FormattedMessage id="actions.scan" defaultMessage="Scan" />}
          description={intl.formatMessage({
            id: "config.tasks.scan_for_content_desc",
            defaultMessage:
              "Scan for new content and add it to the database. Files already in the library are skipped (only new or modified files run the selected generators). To backfill artifacts on already-scanned files, use Generate — or tick Rescan to force the full path on every file.",
          })}
          actions={
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                variant="secondary"
                onClick={() => void runScan()}
              >
                <FormattedMessage id="actions.scan" defaultMessage="Scan" />
              </Button>
              <SelectivePathsButton
                buttonLabel={
                  <FormattedMessage
                    id="actions.selective_scan"
                    defaultMessage="Selective scan"
                  />
                }
                dialogTitle={
                  <FormattedMessage
                    id="actions.selective_scan"
                    defaultMessage="Selective scan"
                  />
                }
                onConfirm={(paths) => void runScan(paths)}
              />
            </div>
          }
          collapsible
        >
          <ScanOptionsForm options={scanOptions} setOptions={setScanOptions} />
        </TaskSectionHeading>

        <TaskSectionHeading
          title={
            <FormattedMessage
              id="config.tasks.identify.heading"
              defaultMessage="Identify"
            />
          }
          description={intl.formatMessage({
            id: "config.tasks.identify.description",
            defaultMessage:
              "Automatically set scene metadata using stash-box and scraper sources.",
          })}
          actions={
            <Button
              type="button"
              variant="secondary"
              onClick={() => setIdentifyOpen(true)}
            >
              <FormattedMessage
                id="actions.identify"
                defaultMessage="Identify"
              />
              …
            </Button>
          }
        />

        <TaskSectionHeading
          title={
            <FormattedMessage id="actions.auto_tag" defaultMessage="Auto tag" />
          }
          description={intl.formatMessage({
            id: "config.tasks.auto_tag_based_on_filenames",
            defaultMessage: "Auto-tag based on file names.",
          })}
          actions={
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                variant="secondary"
                onClick={() => setAutoTagConfirmOpen(true)}
              >
                <FormattedMessage
                  id="actions.auto_tag"
                  defaultMessage="Auto tag"
                />
                …
              </Button>
              <SelectivePathsButton
                buttonLabel={
                  <FormattedMessage
                    id="actions.selective_auto_tag"
                    defaultMessage="Selective auto tag"
                  />
                }
                dialogTitle={
                  <FormattedMessage
                    id="actions.selective_auto_tag"
                    defaultMessage="Selective auto tag"
                  />
                }
                dialogDescription={<AutoTagWarning />}
                confirmVariant="destructive"
                onConfirm={(paths) => void runAutoTag(paths)}
              />
            </div>
          }
          collapsible
        >
          <AutoTagOptionsForm
            options={autoTagOptions}
            setOptions={setAutoTagOptions}
          />
        </TaskSectionHeading>

        <TaskSectionHeading
          title={
            <FormattedMessage id="actions.generate" defaultMessage="Generate" />
          }
          description={intl.formatMessage({
            id: "config.tasks.generate_desc",
            defaultMessage:
              "Generate supporting image, sprite, video, vtt and other files. This is the canonical way to backfill missing artifacts on already-scanned files — each generator skips files that already have the artifact (unless Overwrite is enabled), so it's safe to run repeatedly.",
          })}
          actions={
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                variant="secondary"
                onClick={() => void runGenerate()}
              >
                <FormattedMessage
                  id="actions.generate"
                  defaultMessage="Generate"
                />
              </Button>
              <SelectivePathsButton
                buttonLabel={
                  <FormattedMessage
                    id="actions.selective_generate"
                    defaultMessage="Selective generate"
                  />
                }
                dialogTitle={
                  <FormattedMessage
                    id="actions.selective_generate"
                    defaultMessage="Selective generate"
                  />
                }
                extra={
                  <div className="space-y-3 rounded-md border bg-card px-3 py-3">
                    <TaskOptionToggle
                      id="generate-override-preview-options"
                      label={intl.formatMessage({
                        id: "dialogs.scene_gen.override_preview_generation_options",
                        defaultMessage: "Override preview generation options",
                      })}
                      description={intl.formatMessage({
                        id: "dialogs.scene_gen.override_preview_generation_options_desc",
                        defaultMessage:
                          "Override preview generation options for this operation. Defaults are set in System -> Preview Generation.",
                      })}
                      checked={overridePreviewOptions}
                      onChange={(v) => {
                        setOverridePreviewOptions(v);
                        if (!v) setGeneratePreviewOverrides({});
                      }}
                    />
                    {overridePreviewOptions && (
                      <div className="border-t pt-3">
                        <VideoPreviewInput
                          value={generatePreviewOverrides}
                          onChange={setGeneratePreviewOverrides}
                        />
                      </div>
                    )}
                  </div>
                }
                onConfirm={(paths) =>
                  void runGenerate(
                    paths,
                    overridePreviewOptions
                      ? { previewOptions: generatePreviewOverrides }
                      : undefined,
                  )
                }
              />
            </div>
          }
          collapsible
        >
          <GenerateOptionsForm
            options={generateOptions}
            setOptions={setGenerateOptions}
          />
        </TaskSectionHeading>
      </TaskGroup>

      <IdentifyDialog open={identifyOpen} onOpenChange={setIdentifyOpen} />

      <DestructiveConfirmDialog
        open={autoTagConfirmOpen}
        onOpenChange={setAutoTagConfirmOpen}
        title={
          <FormattedMessage id="actions.auto_tag" defaultMessage="Auto tag" />
        }
        onConfirm={() => void runAutoTag()}
      >
        <AutoTagWarning />
      </DestructiveConfirmDialog>
    </>
  );
}
