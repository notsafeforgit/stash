import { useState } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { Folder, Minus } from "lucide-react";
import type * as GQL from "src/core/generated-graphql";
import {
  useConfigurationContext,
  useConfigureDefaults,
  useConfigureGeneral,
} from "src/hooks/config";
import { useMsg } from "src/hooks/message";
import { Button } from "src/components/ui/button";
import { Checkbox } from "src/components/ui/checkbox";
import { Label } from "src/components/ui/label";
import { DirectorySelectionDialog } from "src/components/shared/directory-selection-dialog";
import {
  SettingsSection,
  SettingStringList,
  SettingSwitch,
  SettingText,
} from "src/components/settings/setting-row";

interface StashConfig {
  path: string;
  excludeVideo: boolean;
  excludeImage: boolean;
}

function StashesEditor({
  stashes,
  onChange,
}: {
  stashes: StashConfig[];
  onChange: (v: StashConfig[]) => void;
}) {
  const intl = useIntl();
  const [addOpen, setAddOpen] = useState(false);

  function setStash(index: number, patch: Partial<StashConfig>) {
    onChange(stashes.map((s, i) => (i === index ? { ...s, ...patch } : s)));
  }

  return (
    <div className="space-y-3">
      {stashes.length === 0 && (
        <p className="text-sm text-muted-foreground">
          {intl.formatMessage({
            id: "setup.folder.no_paths_added",
            defaultMessage: "No library folders added yet.",
          })}
        </p>
      )}
      {stashes.map((stash, i) => (
        <div
          key={stash.path}
          className="flex flex-col gap-2 rounded-lg border p-3 sm:flex-row sm:items-center sm:justify-between"
        >
          <div className="flex min-w-0 items-center gap-2">
            <Folder className="size-4 shrink-0 text-muted-foreground" />
            <code className="truncate text-sm">{stash.path}</code>
          </div>
          <div className="flex shrink-0 items-center gap-4">
            <div className="flex items-center gap-1.5">
              <Checkbox
                id={`stash-${i}-exclude-video`}
                checked={stash.excludeVideo}
                onCheckedChange={(v) =>
                  setStash(i, { excludeVideo: v === true })
                }
              />
              <Label
                htmlFor={`stash-${i}-exclude-video`}
                className="text-xs font-normal"
              >
                {intl.formatMessage({
                  id: "config.general.exclude_video",
                  defaultMessage: "Exclude video",
                })}
              </Label>
            </div>
            <div className="flex items-center gap-1.5">
              <Checkbox
                id={`stash-${i}-exclude-image`}
                checked={stash.excludeImage}
                onCheckedChange={(v) =>
                  setStash(i, { excludeImage: v === true })
                }
              />
              <Label
                htmlFor={`stash-${i}-exclude-image`}
                className="text-xs font-normal"
              >
                {intl.formatMessage({
                  id: "config.general.exclude_image",
                  defaultMessage: "Exclude image",
                })}
              </Label>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={intl.formatMessage({
                id: "actions.delete",
                defaultMessage: "Delete",
              })}
              onClick={() => onChange(stashes.filter((_, j) => j !== i))}
            >
              <Minus className="size-4" />
            </Button>
          </div>
        </div>
      ))}
      <Button type="button" variant="outline" onClick={() => setAddOpen(true)}>
        {intl.formatMessage({
          id: "actions.add_directory",
          defaultMessage: "Add directory",
        })}
        …
      </Button>
      <DirectorySelectionDialog
        open={addOpen}
        onOpenChange={setAddOpen}
        title={intl.formatMessage({
          id: "actions.add_directory",
          defaultMessage: "Add directory",
        })}
        onConfirm={(paths) => {
          setAddOpen(false);
          const existing = new Set(stashes.map((s) => s.path));
          const added = paths
            .filter((p) => !existing.has(p))
            .map((path) => ({
              path,
              excludeVideo: false,
              excludeImage: false,
            }));
          if (added.length > 0) onChange([...stashes, ...added]);
        }}
      />
    </div>
  );
}

function commaListToArray(value: string): string[] {
  return value
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s !== "");
}

function SettingsLibraryPage() {
  const { configuration } = useConfigurationContext();
  const general = configuration.general;
  const defaults = configuration.defaults;
  const [configureGeneral] = useConfigureGeneral();
  const [configureDefaults] = useConfigureDefaults();

  function saveGeneral(input: Partial<GQL.ConfigGeneralInput>) {
    void configureGeneral({ variables: { input } });
  }

  function saveDefaults(input: Partial<GQL.ConfigDefaultSettingsInput>) {
    void configureDefaults({ variables: { input } });
  }

  const msg = useMsg();

  return (
    <div className="max-w-3xl space-y-8 p-6">
      <SettingsSection
        title={msg("library", "Library")}
        description={msg(
          "config.general.directory_locations_to_your_content",
          "Directory locations to your content",
        )}
      >
        <StashesEditor
          stashes={general.stashes.map((s) => ({
            path: s.path,
            excludeVideo: s.excludeVideo,
            excludeImage: s.excludeImage,
          }))}
          onChange={(v) => saveGeneral({ stashes: v })}
        />
      </SettingsSection>

      <SettingsSection
        title={msg(
          "config.library.media_content_extensions",
          "Media content extensions",
        )}
      >
        <SettingText
          label={msg("config.general.video_ext_head", "Video extensions")}
          description={msg(
            "config.general.video_ext_desc",
            "Comma-delimited list of file extensions that will be identified as videos.",
          )}
          value={(general.videoExtensions ?? []).join(", ")}
          onChange={(v) =>
            saveGeneral({ videoExtensions: commaListToArray(v) })
          }
        />
        <SettingText
          label={msg("config.general.image_ext_head", "Image extensions")}
          description={msg(
            "config.general.image_ext_desc",
            "Comma-delimited list of file extensions that will be identified as images.",
          )}
          value={(general.imageExtensions ?? []).join(", ")}
          onChange={(v) =>
            saveGeneral({ imageExtensions: commaListToArray(v) })
          }
        />
        <SettingText
          label={msg("config.general.gallery_ext_head", "Gallery extensions")}
          description={msg(
            "config.general.gallery_ext_desc",
            "Comma-delimited list of file extensions that will be identified as gallery zip files.",
          )}
          value={(general.galleryExtensions ?? []).join(", ")}
          onChange={(v) =>
            saveGeneral({ galleryExtensions: commaListToArray(v) })
          }
        />
      </SettingsSection>

      <SettingsSection title={msg("config.library.exclusions", "Exclusions")}>
        <SettingStringList
          label={msg(
            "config.general.excluded_video_patterns_head",
            "Excluded video patterns",
          )}
          description={msg(
            "config.general.excluded_video_patterns_desc",
            "Regexps of video files/paths to exclude from scan and add to clean",
          )}
          value={general.excludes ?? []}
          onChange={(v) => saveGeneral({ excludes: v })}
          defaultNewValue="sample\.mp4$"
        />
        <SettingStringList
          label={msg(
            "config.general.excluded_image_gallery_patterns_head",
            "Excluded image/gallery patterns",
          )}
          description={msg(
            "config.general.excluded_image_gallery_patterns_desc",
            "Regexps of image and gallery files/paths to exclude from scan and add to clean",
          )}
          value={general.imageExcludes ?? []}
          onChange={(v) => saveGeneral({ imageExcludes: v })}
          defaultNewValue="sample\.jpg$"
        />
      </SettingsSection>

      <SettingsSection
        title={msg(
          "config.library.gallery_and_image_options",
          "Gallery and image options",
        )}
      >
        <SettingSwitch
          label={msg(
            "config.general.create_galleries_from_folders_label",
            "Create galleries from folders containing images",
          )}
          description={msg(
            "config.general.create_galleries_from_folders_desc",
            "If true, creates galleries from folders containing images.",
          )}
          checked={general.createGalleriesFromFolders}
          onChange={(v) => saveGeneral({ createGalleriesFromFolders: v })}
        />
        <SettingSwitch
          label={msg(
            "config.ui.images.options.write_image_thumbnails.heading",
            "Write image thumbnails",
          )}
          description={msg(
            "config.ui.images.options.write_image_thumbnails.description",
            "Write image thumbnails to disk when generated on the fly",
          )}
          checked={general.writeImageThumbnails}
          onChange={(v) => saveGeneral({ writeImageThumbnails: v })}
        />
        <SettingSwitch
          label={msg(
            "config.ui.images.options.create_image_clips_from_videos.heading",
            "Create image clips from videos",
          )}
          description={msg(
            "config.ui.images.options.create_image_clips_from_videos.description",
            "If true, creates image clips from video files in zip galleries and image-only paths.",
          )}
          checked={general.createImageClipsFromVideos}
          onChange={(v) => saveGeneral({ createImageClipsFromVideos: v })}
        />
        <SettingText
          label={msg(
            "config.general.gallery_cover_regex_label",
            "Gallery cover pattern",
          )}
          description={msg(
            "config.general.gallery_cover_regex_desc",
            "Regexp of image filenames to use as gallery cover",
          )}
          value={general.galleryCoverRegex}
          onChange={(v) => saveGeneral({ galleryCoverRegex: v })}
        />
      </SettingsSection>

      <SettingsSection
        title={msg("config.ui.delete_options.heading", "Delete options")}
      >
        <SettingSwitch
          label={msg(
            "config.ui.delete_options.options.delete_file",
            "Delete file by default",
          )}
          checked={defaults.deleteFile ?? false}
          onChange={(v) => saveDefaults({ deleteFile: v })}
        />
        <SettingSwitch
          label={msg(
            "config.ui.delete_options.options.delete_generated_supporting_files",
            "Delete generated supporting files by default",
          )}
          description={msg(
            "config.ui.delete_options.description",
            "Default delete-dialog options when deleting items.",
          )}
          checked={defaults.deleteGenerated ?? false}
          onChange={(v) => saveDefaults({ deleteGenerated: v })}
        />
      </SettingsSection>
    </div>
  );
}

export const Route = createFileRoute("/settings/library")({
  component: SettingsLibraryPage,
});
