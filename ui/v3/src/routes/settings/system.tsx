import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useMutation } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { useConfigurationContext, useConfigureGeneral } from "src/hooks/config";
import { useToast } from "src/hooks/toast";
import { Button } from "src/components/ui/button";
import {
  SettingDisplay,
  SettingNumber,
  SettingsSection,
  SettingSelect,
  SettingStringList,
  SettingSwitch,
  SettingText,
} from "src/components/settings/setting-row";

const TRANSCODE_RESOLUTIONS: {
  value: GQL.StreamingResolutionEnum;
  label: string;
}[] = [
  { value: GQL.StreamingResolutionEnum.Low, label: "240p" },
  { value: GQL.StreamingResolutionEnum.Standard, label: "480p" },
  { value: GQL.StreamingResolutionEnum.StandardHd, label: "720p" },
  { value: GQL.StreamingResolutionEnum.FullHd, label: "1080p" },
  { value: GQL.StreamingResolutionEnum.FourK, label: "4K" },
  { value: GQL.StreamingResolutionEnum.Original, label: "Original" },
];

function SettingsSystemPage() {
  const intl = useIntl();
  const Toast = useToast();
  const navigate = useNavigate();
  const { configuration } = useConfigurationContext();
  const general = configuration.general;
  const [configureGeneral] = useConfigureGeneral();
  const [downloadFFMpeg] = useMutation(GQL.DownloadFfMpegDocument);

  function save(input: Partial<GQL.ConfigGeneralInput>) {
    void configureGeneral({ variables: { input } });
  }

  const msg = (id: string, defaultMessage: string) =>
    intl.formatMessage({ id, defaultMessage });

  async function onDownloadFFMpeg() {
    try {
      await downloadFFMpeg();
      // navigate to tasks page to watch the download job
      void navigate({ to: "/settings/tasks" });
    } catch (e) {
      Toast.error(e);
    }
  }

  return (
    <div className="max-w-3xl space-y-8 p-6">
      <SettingsSection
        title={msg("config.application_paths.heading", "Application paths")}
      >
        <SettingText
          label={msg("config.general.generated_path_head", "Generated path")}
          description={msg(
            "config.general.generated_files_location",
            "Directory location for the generated files (previews, screenshots, sprites, etc.)",
          )}
          value={general.generatedPath}
          onChange={(v) => save({ generatedPath: v })}
        />
        <SettingText
          label={msg("config.general.cache_path_head", "Cache path")}
          description={msg(
            "config.general.cache_location",
            "Directory location of the cache",
          )}
          value={general.cachePath}
          onChange={(v) => save({ cachePath: v })}
        />
        <SettingText
          label={msg("config.general.scrapers_path.heading", "Scrapers path")}
          description={msg(
            "config.general.scrapers_path.description",
            "Directory location for scraper configuration files",
          )}
          value={general.scrapersPath}
          onChange={(v) => save({ scrapersPath: v })}
        />
        <SettingText
          label={msg("config.general.plugins_path.heading", "Plugins path")}
          description={msg(
            "config.general.plugins_path.description",
            "Directory location for plugin configuration files",
          )}
          value={general.pluginsPath}
          onChange={(v) => save({ pluginsPath: v })}
        />
        <SettingText
          label={msg("config.general.metadata_path.heading", "Metadata path")}
          description={msg(
            "config.general.metadata_path.description",
            "Directory location for metadata export",
          )}
          value={general.metadataPath}
          onChange={(v) => save({ metadataPath: v })}
        />
        <SettingText
          label={msg(
            "config.ui.performers.options.image_location.heading",
            "Custom performer image location",
          )}
          description={msg(
            "config.ui.performers.options.image_location.description",
            "Directory location of images for performers (with filenames matching performer names)",
          )}
          value={general.customPerformerImageLocation ?? ""}
          onChange={(v) => save({ customPerformerImageLocation: v })}
        />
        <SettingText
          label={msg(
            "config.general.ffmpeg.ffmpeg_path.heading",
            "FFmpeg path",
          )}
          description={msg(
            "config.general.ffmpeg.ffmpeg_path.description",
            "Path to the ffmpeg executable",
          )}
          value={general.ffmpegPath ?? ""}
          onChange={(v) => save({ ffmpegPath: v })}
        />
        <SettingText
          label={msg(
            "config.general.ffmpeg.ffprobe_path.heading",
            "FFprobe path",
          )}
          description={msg(
            "config.general.ffmpeg.ffprobe_path.description",
            "Path to the ffprobe executable",
          )}
          value={general.ffprobePath ?? ""}
          onChange={(v) => save({ ffprobePath: v })}
        />
        <SettingDisplay
          label={msg(
            "config.general.ffmpeg.download_ffmpeg.heading",
            "Download FFmpeg",
          )}
          description={msg(
            "config.general.ffmpeg.download_ffmpeg.description",
            "Downloads FFmpeg and FFprobe into the config directory",
          )}
          actions={
            <Button
              type="button"
              variant="secondary"
              onClick={() => void onDownloadFFMpeg()}
            >
              {msg(
                "config.general.ffmpeg.download_ffmpeg.heading",
                "Download FFmpeg",
              )}
            </Button>
          }
        />
        <SettingText
          label={msg("config.general.python_path.heading", "Python path")}
          description={msg(
            "config.general.python_path.description",
            "Path to the python executable, used by plugins and scrapers",
          )}
          value={general.pythonPath ?? ""}
          onChange={(v) => save({ pythonPath: v })}
        />
        <SettingText
          label={msg(
            "config.general.backup_directory_path.heading",
            "Backup directory path",
          )}
          description={msg(
            "config.general.backup_directory_path.description",
            "Directory location for database backups",
          )}
          value={general.backupDirectoryPath ?? ""}
          onChange={(v) => save({ backupDirectoryPath: v })}
        />
        <SettingText
          label={msg(
            "config.general.delete_trash_path.heading",
            "Delete trash path",
          )}
          description={msg(
            "config.general.delete_trash_path.description",
            "Directory files are moved to when deleted, rather than being permanently deleted",
          )}
          value={general.deleteTrashPath ?? ""}
          onChange={(v) => save({ deleteTrashPath: v })}
        />
      </SettingsSection>

      <SettingsSection title={msg("config.general.database", "Database")}>
        <SettingText
          label={msg("config.general.db_path_head", "Database path")}
          description={msg(
            "config.general.sqlite_location",
            "File location for the SQLite database (requires restart)",
          )}
          value={general.databasePath}
          onChange={(v) => save({ databasePath: v })}
        />
        <SettingSelect
          label={msg("config.general.blobs_storage.heading", "Blobs storage")}
          description={msg(
            "config.general.blobs_storage.description",
            "Where binary data (images) is stored.",
          )}
          value={general.blobsStorage}
          options={[
            {
              value: GQL.BlobsStorageType.Database,
              label: msg("blobs_storage_type.database", "Database"),
            },
            {
              value: GQL.BlobsStorageType.Filesystem,
              label: msg("blobs_storage_type.filesystem", "Filesystem"),
            },
          ]}
          onChange={(v) => save({ blobsStorage: v as GQL.BlobsStorageType })}
        />
        <SettingText
          label={msg("config.general.blobs_path.heading", "Blobs path")}
          description={msg(
            "config.general.blobs_path.description",
            "Directory location to store binary data (when using filesystem storage)",
          )}
          value={general.blobsPath}
          onChange={(v) => save({ blobsPath: v })}
        />
      </SettingsSection>

      <SettingsSection title={msg("config.general.hashing", "Hashing")}>
        <SettingSwitch
          label={msg(
            "config.general.calculate_md5_and_ohash_label",
            "Calculate MD5 for videos",
          )}
          description={msg(
            "config.general.calculate_md5_and_ohash_desc",
            "Calculate MD5 checksum in addition to oshash. Enabling will cause initial scans to be slower.",
          )}
          checked={general.calculateMD5}
          onChange={(v) => save({ calculateMD5: v })}
        />
        <SettingSelect
          label={msg(
            "config.general.generated_file_naming_hash_head",
            "Generated file naming hash",
          )}
          description={msg(
            "config.general.generated_file_naming_hash_desc",
            "Hash algorithm used for generated file names.",
          )}
          value={general.videoFileNamingAlgorithm}
          options={[
            { value: GQL.HashAlgorithm.Md5, label: "MD5" },
            { value: GQL.HashAlgorithm.Oshash, label: "oshash" },
          ]}
          onChange={(v) =>
            save({ videoFileNamingAlgorithm: v as GQL.HashAlgorithm })
          }
        />
      </SettingsSection>

      <SettingsSection title={msg("config.system.transcoding", "Transcoding")}>
        <SettingSelect
          label={msg(
            "config.general.maximum_transcode_size_head",
            "Maximum transcode size",
          )}
          description={msg(
            "config.general.maximum_transcode_size_desc",
            "Maximum size for generated transcodes",
          )}
          value={
            general.maxTranscodeSize ?? GQL.StreamingResolutionEnum.Original
          }
          options={TRANSCODE_RESOLUTIONS}
          onChange={(v) =>
            save({ maxTranscodeSize: v as GQL.StreamingResolutionEnum })
          }
        />
        <SettingSelect
          label={msg(
            "config.general.maximum_streaming_transcode_size_head",
            "Maximum streaming transcode size",
          )}
          description={msg(
            "config.general.maximum_streaming_transcode_size_desc",
            "Maximum size for transcoded streams",
          )}
          value={
            general.maxStreamingTranscodeSize ??
            GQL.StreamingResolutionEnum.Original
          }
          options={TRANSCODE_RESOLUTIONS}
          onChange={(v) =>
            save({
              maxStreamingTranscodeSize: v as GQL.StreamingResolutionEnum,
            })
          }
        />
        <SettingSwitch
          label={msg(
            "config.general.ffmpeg.hardware_acceleration.heading",
            "FFmpeg hardware encoding",
          )}
          description={msg(
            "config.general.ffmpeg.hardware_acceleration.desc",
            "Use hardware acceleration when transcoding, if supported",
          )}
          checked={general.transcodeHardwareAcceleration}
          onChange={(v) => save({ transcodeHardwareAcceleration: v })}
        />
        <SettingStringList
          label={msg(
            "config.general.ffmpeg.transcode.input_args.heading",
            "FFmpeg transcode input args",
          )}
          description={msg(
            "config.general.ffmpeg.transcode.input_args.desc",
            "Advanced: additional ffmpeg input arguments for transcode generation",
          )}
          value={general.transcodeInputArgs ?? []}
          onChange={(v) => save({ transcodeInputArgs: v })}
        />
        <SettingStringList
          label={msg(
            "config.general.ffmpeg.transcode.output_args.heading",
            "FFmpeg transcode output args",
          )}
          description={msg(
            "config.general.ffmpeg.transcode.output_args.desc",
            "Advanced: additional ffmpeg output arguments for transcode generation",
          )}
          value={general.transcodeOutputArgs ?? []}
          onChange={(v) => save({ transcodeOutputArgs: v })}
        />
        <SettingStringList
          label={msg(
            "config.general.ffmpeg.live_transcode.input_args.heading",
            "FFmpeg live transcode input args",
          )}
          description={msg(
            "config.general.ffmpeg.live_transcode.input_args.desc",
            "Advanced: additional ffmpeg input arguments for live transcoding",
          )}
          value={general.liveTranscodeInputArgs ?? []}
          onChange={(v) => save({ liveTranscodeInputArgs: v })}
        />
        <SettingStringList
          label={msg(
            "config.general.ffmpeg.live_transcode.output_args.heading",
            "FFmpeg live transcode output args",
          )}
          description={msg(
            "config.general.ffmpeg.live_transcode.output_args.desc",
            "Advanced: additional ffmpeg output arguments for live transcoding",
          )}
          value={general.liveTranscodeOutputArgs ?? []}
          onChange={(v) => save({ liveTranscodeOutputArgs: v })}
        />
      </SettingsSection>

      <SettingsSection
        title={msg(
          "config.general.parallel_scan_head",
          "Parallel scan/generation",
        )}
      >
        <SettingNumber
          label={msg(
            "config.general.number_of_parallel_task_for_scan_generation_head",
            "Number of parallel tasks",
          )}
          description={msg(
            "config.general.number_of_parallel_task_for_scan_generation_desc",
            "Set to 0 for auto-detection. Warning: setting above the auto-detected value can decrease performance and cause issues.",
          )}
          value={general.parallelTasks}
          onChange={(v) => save({ parallelTasks: v })}
        />
      </SettingsSection>

      <SettingsSection
        title={msg("config.general.preview_generation", "Preview generation")}
      >
        <SettingSelect
          label={msg(
            "dialogs.scene_gen.preview_preset_head",
            "Preview encoding preset",
          )}
          description={msg(
            "dialogs.scene_gen.preview_preset_desc",
            "The preset regulates size, quality and encoding time of preview generation.",
          )}
          value={general.previewPreset}
          options={Object.values(GQL.PreviewPreset).map((p) => ({
            value: p,
            label: p,
          }))}
          onChange={(v) => save({ previewPreset: v as GQL.PreviewPreset })}
        />
        <SettingSwitch
          label={msg("config.general.include_audio_head", "Include audio")}
          description={msg(
            "config.general.include_audio_desc",
            "Includes audio stream when generating previews.",
          )}
          checked={general.previewAudio}
          onChange={(v) => save({ previewAudio: v })}
        />
        <SettingNumber
          label={msg(
            "dialogs.scene_gen.preview_seg_count_head",
            "Number of segments in preview",
          )}
          description={msg(
            "dialogs.scene_gen.preview_seg_count_desc",
            "Number of segments in preview files.",
          )}
          value={general.previewSegments}
          onChange={(v) => save({ previewSegments: v })}
        />
        <SettingNumber
          label={msg(
            "dialogs.scene_gen.preview_seg_duration_head",
            "Preview segment duration",
          )}
          description={msg(
            "dialogs.scene_gen.preview_seg_duration_desc",
            "Duration of each preview segment, in seconds.",
          )}
          value={general.previewSegmentDuration}
          onChange={(v) => save({ previewSegmentDuration: v })}
        />
        <SettingText
          label={msg(
            "dialogs.scene_gen.preview_exclude_start_time_head",
            "Exclude start time",
          )}
          description={msg(
            "dialogs.scene_gen.preview_exclude_start_time_desc",
            "Exclude the first x seconds from scene previews. This can be a value in seconds, or a percentage (eg 2%) of the total scene duration.",
          )}
          value={general.previewExcludeStart}
          onChange={(v) => save({ previewExcludeStart: v })}
          inputClassName="w-28"
        />
        <SettingText
          label={msg(
            "dialogs.scene_gen.preview_exclude_end_time_head",
            "Exclude end time",
          )}
          description={msg(
            "dialogs.scene_gen.preview_exclude_end_time_desc",
            "Exclude the last x seconds from scene previews. This can be a value in seconds, or a percentage (eg 2%) of the total scene duration.",
          )}
          value={general.previewExcludeEnd}
          onChange={(v) => save({ previewExcludeEnd: v })}
          inputClassName="w-28"
        />
      </SettingsSection>

      <SettingsSection
        title={msg(
          "config.general.sprite_generation_head",
          "Sprite generation",
        )}
      >
        <SettingNumber
          label={msg(
            "config.general.sprite_screenshot_size_head",
            "Sprite screenshot size",
          )}
          description={msg(
            "config.general.sprite_screenshot_size_desc",
            "Width of the generated sprite screenshots, in pixels.",
          )}
          value={general.spriteScreenshotSize ?? 160}
          onChange={(v) => save({ spriteScreenshotSize: v })}
        />
        <SettingSwitch
          label={msg(
            "config.general.use_custom_sprite_interval_head",
            "Use custom sprite interval",
          )}
          description={msg(
            "config.general.use_custom_sprite_interval_desc",
            "Use a fixed interval between sprite screenshots instead of a fixed count.",
          )}
          checked={general.useCustomSpriteInterval ?? false}
          onChange={(v) => save({ useCustomSpriteInterval: v })}
        />
        <SettingNumber
          label={msg("config.general.sprite_interval_head", "Sprite interval")}
          description={msg(
            "config.general.sprite_interval_desc",
            "Seconds between sprite screenshots when using a custom interval.",
          )}
          value={general.spriteInterval ?? 0}
          onChange={(v) => save({ spriteInterval: v })}
        />
        <SettingNumber
          label={msg("config.general.sprite_minimum_head", "Minimum sprites")}
          description={msg(
            "config.general.sprite_minimum_desc",
            "Minimum number of sprite screenshots per video.",
          )}
          value={general.minimumSprites ?? 10}
          onChange={(v) => save({ minimumSprites: v })}
        />
        <SettingNumber
          label={msg("config.general.sprite_maximum_head", "Maximum sprites")}
          description={msg(
            "config.general.sprite_maximum_desc",
            "Maximum number of sprite screenshots per video.",
          )}
          value={general.maximumSprites ?? 10}
          onChange={(v) => save({ maximumSprites: v })}
        />
      </SettingsSection>

      <SettingsSection
        title={msg("config.general.heatmap_generation", "Heatmap generation")}
      >
        <SettingSwitch
          label={msg(
            "config.general.funscript_heatmap_draw_range",
            "Draw funscript heatmap range",
          )}
          description={msg(
            "config.general.funscript_heatmap_draw_range_desc",
            "Draws the motion range into generated funscript heatmaps.",
          )}
          checked={general.drawFunscriptHeatmapRange ?? true}
          onChange={(v) => save({ drawFunscriptHeatmapRange: v })}
        />
      </SettingsSection>

      <SettingsSection title={msg("config.general.logging", "Logging")}>
        <SettingText
          label={msg("config.general.auth.log_file", "Log file")}
          description={msg(
            "config.general.auth.log_file_desc",
            "Path to the file to output logging to. Blank to disable file logging. Requires restart.",
          )}
          value={general.logFile ?? ""}
          onChange={(v) => save({ logFile: v })}
        />
        <SettingSwitch
          label={msg("config.general.auth.log_to_terminal", "Log to terminal")}
          description={msg(
            "config.general.auth.log_to_terminal_desc",
            "Logs to the terminal in addition to a file. Always true if file logging is disabled. Requires restart.",
          )}
          checked={general.logOut}
          onChange={(v) => save({ logOut: v })}
        />
        <SettingSelect
          label={msg("config.logs.log_level", "Log level")}
          value={general.logLevel}
          options={["Trace", "Debug", "Info", "Warning", "Error"].map((l) => ({
            value: l,
            label: l,
          }))}
          onChange={(v) => save({ logLevel: v })}
        />
        <SettingSwitch
          label={msg("config.general.auth.log_http", "Log HTTP access")}
          description={msg(
            "config.general.auth.log_http_desc",
            "Logs HTTP access to the terminal. Requires restart.",
          )}
          checked={general.logAccess}
          onChange={(v) => save({ logAccess: v })}
        />
        <SettingNumber
          label={msg(
            "config.general.auth.log_file_max_size",
            "Log file max size (MB)",
          )}
          description={msg(
            "config.general.auth.log_file_max_size_desc",
            "Maximum size of the log file in megabytes before it is rotated.",
          )}
          value={general.logFileMaxSize ?? 10}
          onChange={(v) => save({ logFileMaxSize: v })}
        />
      </SettingsSection>
    </div>
  );
}

export const Route = createFileRoute("/settings/system")({
  component: SettingsSystemPage,
});
