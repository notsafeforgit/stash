import { useCallback } from "react";
import { useApolloClient } from "@apollo/client/react";
import { MediaRoots } from "@/components/archive/media-roots";
import { mediaRootSearchSchema } from "@/core/native-archive/media-root-api";
import { createFileRoute } from "@tanstack/react-router";
import type * as GQL from "src/core/generated-graphql";
import {
  useConfigurationContext,
  useConfigureDefaults,
  useConfigureGeneral,
} from "src/hooks/config";
import { useMsg } from "src/hooks/message";
import {
  SettingsSection,
  SettingStringList,
  SettingSwitch,
  SettingText,
} from "src/components/settings/setting-row";

function commaListToArray(value: string): string[] {
  return value
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s !== "");
}

function SettingsLibraryPage() {
  const { root, create, ...filter } = Route.useSearch();
  const navigate = Route.useNavigate();
  const client = useApolloClient();
  const refreshFolders = useCallback(() => {
    void client.refetchQueries({ include: ["Configuration"] });
  }, [client]);
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
    <div className="flex max-w-4xl flex-col gap-8 p-4 md:p-6">
      <SettingsSection
        title={msg("media_roots.title", "Library folders")}
        description={msg(
          "media_roots.description",
          "Manage folders used by library scans and downloads.",
        )}
      >
        <MediaRoots
          key={JSON.stringify(filter)}
          embedded
          filter={filter}
          selected={root}
          create={create}
          onChanged={refreshFolders}
          onFilterChange={(filter) => void navigate({ search: filter })}
          onSelect={(root, create) =>
            void navigate({ search: { ...filter, root, create } })
          }
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
  validateSearch: mediaRootSearchSchema,
  component: SettingsLibraryPage,
});
