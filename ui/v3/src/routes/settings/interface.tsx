import { createFileRoute } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useTheme } from "src/components/theme-provider";
import { OfflineSettingsSection } from "src/components/offline/offline-settings-section";
import {
  useConfigurationContext,
  useConfigureInterface,
  useConfigureUISetting,
} from "src/hooks/config";
import { useMsg } from "src/hooks/message";
import { PreviewDefaultType } from "src/core/generated-graphql";
import type { ConfigInterfaceInput } from "src/core/generated-graphql";
import type { IUIConfig } from "src/core/config";
import {
  defaultRatingStarPrecision,
  defaultRatingSystemType,
  type RatingStarPrecision,
  ratingStarPrecisionIntlMap,
  RatingSystemType,
  ratingSystemIntlMap,
} from "src/utils/rating";
import {
  SettingNumber,
  SettingsSection,
  SettingSelect,
  SettingSwitch,
  SettingText,
} from "src/components/settings/setting-row";

// Language options match the locale files shipped in src/locales.
const LANGUAGES: { value: string; label: string }[] = [
  { value: "af-ZA", label: "Afrikaans (Preview)" },
  { value: "ar", label: "Arabic (Preview)" },
  { value: "bg-BG", label: "Bulgarian (Preview)" },
  { value: "bn-BD", label: "বাংলা (বাংলাদেশ) (Preview)" },
  { value: "ca-ES", label: "Catalan (Preview)" },
  { value: "cs-CZ", label: "Čeština (Česko)" },
  { value: "da-DK", label: "Dansk (Danmark)" },
  { value: "de-DE", label: "Deutsch (Deutschland)" },
  { value: "en-GB", label: "English (United Kingdom)" },
  { value: "en-US", label: "English (United States)" },
  { value: "es-ES", label: "Español (España)" },
  { value: "et-EE", label: "Eesti" },
  { value: "fa-IR", label: "فارسی (ایران) (Preview)" },
  { value: "fi-FI", label: "Suomi" },
  { value: "fr-FR", label: "Français (France)" },
  { value: "hi-IN", label: "हिन्दी (Preview)" },
  { value: "hr-HR", label: "Hrvatski (Preview)" },
  { value: "hu-HU", label: "Magyar (Preview)" },
  { value: "id-ID", label: "Indonesian (Preview)" },
  { value: "it-IT", label: "Italiano" },
  { value: "ja-JP", label: "日本語 (日本)" },
  { value: "ko-KR", label: "한국어 (대한민국)" },
  { value: "lt-LT", label: "Lithuanian (Preview)" },
  { value: "lv-LV", label: "Latviešu (Preview)" },
  { value: "nb-NO", label: "Norsk bokmål" },
  { value: "nl-NL", label: "Nederlands (Nederland)" },
  { value: "nn-NO", label: "Nynorsk (Preview)" },
  { value: "pl-PL", label: "Polski" },
  { value: "pt-BR", label: "Português (Brasil)" },
  { value: "ro-RO", label: "Română (Preview)" },
  { value: "ru-RU", label: "Русский (Россия)" },
  { value: "sk-SK", label: "Slovenčina (Preview)" },
  { value: "sv-SE", label: "Svenska" },
  { value: "th-TH", label: "ภาษาไทย (ไทย)" },
  { value: "tr-TR", label: "Türkçe (Türkiye)" },
  { value: "uk-UA", label: "Ukrainian (Україна)" },
  { value: "ur-PK", label: "Urdu (Preview)" },
  { value: "vi-VN", label: "Tiếng Việt (Preview)" },
  { value: "zh-CN", label: "简体中文 (中国)" },
  { value: "zh-TW", label: "繁體中文 (台灣)" },
];

function SettingsInterfacePage() {
  const intl = useIntl();
  const { theme, setTheme } = useTheme();
  const { configuration } = useConfigurationContext();
  const [configureInterface, { loading: savingInterface }] =
    useConfigureInterface();
  const [saveUISetting, { loading: savingUI }] = useConfigureUISetting();

  const iface = configuration.interface;
  const ui = configuration.ui as IUIConfig;

  const msg = useMsg();

  function saveInterface(input: ConfigInterfaceInput) {
    void configureInterface({ variables: { input } });
  }

  function saveUI(key: string, value: unknown) {
    void saveUISetting({ variables: { key, value } });
  }

  const autostartVideo = iface.autostartVideo ?? true;
  const previewDefault = iface.previewDefault ?? PreviewDefaultType.Video;
  const playVideoOnHover = iface.playVideoOnHover ?? true;
  const alwaysStartFromBeginning = ui.alwaysStartFromBeginning ?? false;
  const showRangeMarkers = ui.showRangeMarkers ?? true;
  const trackActivity = ui.trackActivity ?? true;
  const minimumPlayPercent = ui.minimumPlayPercent ?? 0;
  const abbreviateCounters = ui.abbreviateCounters ?? false;
  const ratingSystemType =
    ui.ratingSystemOptions?.type ?? defaultRatingSystemType;
  const ratingStarPrecision =
    ui.ratingSystemOptions?.starPrecision ?? defaultRatingStarPrecision;

  const themeLabels: Record<typeof theme, string> = {
    system: msg("theme.system", "System"),
    light: msg("theme.light", "Light"),
    dark: msg("theme.dark", "Dark"),
  };

  return (
    <div className="max-w-2xl space-y-8 p-6">
      <SettingsSection
        title={msg("config.ui.basic_settings", "Basic settings")}
      >
        <SettingSelect
          label={msg("config.ui.language.heading", "Language")}
          value={iface.language ?? "en-GB"}
          options={LANGUAGES}
          onChange={(v) => saveInterface({ language: v })}
          triggerClassName="w-60"
        />
        <SettingSelect
          label={msg("theme.label", "Theme")}
          description={msg(
            "theme.description",
            "Choose your preferred colour scheme.",
          )}
          value={theme}
          options={(["system", "light", "dark"] as const).map((t) => ({
            value: t,
            label: themeLabels[t],
          }))}
          onChange={(v) => setTheme(v as typeof theme)}
          triggerClassName="w-32"
        />
        <SettingText
          label={msg("config.ui.custom_title.heading", "Custom title")}
          description={msg(
            "config.ui.custom_title.description",
            "Custom app title for the navigation header and browser tab titles.",
          )}
          value={ui.title ?? ""}
          onChange={(v) => saveUI("title", v)}
          disabled={savingUI}
        />
        <SettingSwitch
          label={msg(
            "config.ui.abbreviate_counters.heading",
            "Abbreviate counters",
          )}
          description={msg(
            "config.ui.abbreviate_counters.description",
            "Display counters such as 1.2K instead of the full number.",
          )}
          checked={abbreviateCounters}
          onChange={(v) => saveUI("abbreviateCounters", v)}
          disabled={savingUI}
        />
      </SettingsSection>

      <SettingsSection
        title={msg("config.ui.scene_player.heading", "Scene player")}
      >
        <SettingSwitch
          label={msg(
            "config.ui.scene_player.options.auto_start_video",
            "Auto-start video",
          )}
          description={msg(
            "auto_start_video.description",
            "Start playback automatically when a scene opens.",
          )}
          checked={autostartVideo}
          onChange={(v) => saveInterface({ autostartVideo: v })}
          disabled={savingInterface}
        />
        <SettingSwitch
          label={msg(
            "config.ui.scene_player.options.always_start_from_beginning",
            "Always start from the beginning",
          )}
          description={msg(
            "config.ui.scene_player.options.always_start_from_beginning_description",
            "Ignore the saved resume position when opening a scene and start from the beginning instead. Deep links and marker clicks still seek to their target.",
          )}
          checked={alwaysStartFromBeginning}
          onChange={(v) => saveUI("alwaysStartFromBeginning", v)}
          disabled={savingUI}
        />
        <SettingSwitch
          label={msg(
            "config.ui.scene_player.options.show_range_markers",
            "Show range markers",
          )}
          checked={showRangeMarkers}
          onChange={(v) => saveUI("showRangeMarkers", v)}
          disabled={savingUI}
        />
        <SettingSwitch
          label={msg(
            "config.ui.scene_player.options.track_activity",
            "Track activity",
          )}
          checked={trackActivity}
          onChange={(v) => saveUI("trackActivity", v)}
          disabled={savingUI}
        />
        <SettingNumber
          label={msg(
            "config.ui.minimum_play_percent.heading",
            "Minimum play percent",
          )}
          description={msg(
            "config.ui.minimum_play_percent.description",
            "The minimum percentage a scene must be played before the play count is incremented.",
          )}
          value={minimumPlayPercent}
          onChange={(v) => saveUI("minimumPlayPercent", v)}
          min={0}
          max={100}
          integer
          disabled={savingUI || !trackActivity}
        />
      </SettingsSection>

      <SettingsSection
        title={msg("config.ui.scene_list.heading", "Scene list")}
      >
        <SettingSelect
          label={msg(
            "config.ui.preview_default.heading",
            "Card preview default",
          )}
          description={msg(
            "config.ui.preview_default.description",
            "What list cards (grid and wall) show when not hovered.",
          )}
          value={previewDefault}
          options={[
            {
              value: PreviewDefaultType.Image,
              label: msg("config.ui.preview_default.image", "Static image"),
            },
            {
              value: PreviewDefaultType.Animated,
              label: msg(
                "config.ui.preview_default.animated",
                "Animated (WebP)",
              ),
            },
            {
              value: PreviewDefaultType.Video,
              label: msg("config.ui.preview_default.video", "Video"),
            },
          ]}
          onChange={(v) =>
            saveInterface({ previewDefault: v as PreviewDefaultType })
          }
          disabled={savingInterface}
          triggerClassName="w-40"
        />
        <SettingSwitch
          label={msg(
            "config.ui.play_video_on_hover.heading",
            "Play video on hover",
          )}
          description={msg(
            "config.ui.play_video_on_hover.description",
            "Swap card previews to the video when you hover them. No effect when the default is already Video.",
          )}
          checked={playVideoOnHover}
          onChange={(v) => saveInterface({ playVideoOnHover: v })}
          disabled={
            savingInterface || previewDefault === PreviewDefaultType.Video
          }
        />
      </SettingsSection>

      <SettingsSection title={msg("config.ui.editing.heading", "Editing")}>
        <SettingSelect
          label={msg(
            "config.ui.editing.rating_system.type.label",
            "Rating system",
          )}
          value={ratingSystemType}
          options={Array.from(ratingSystemIntlMap.entries()).map(
            ([value, intlId]) => ({
              value,
              label: intl.formatMessage({ id: intlId }),
            }),
          )}
          onChange={(v) =>
            saveUI("ratingSystemOptions", {
              ...ui.ratingSystemOptions,
              type: v as RatingSystemType,
            })
          }
          disabled={savingUI}
          triggerClassName="w-36"
        />
        {ratingSystemType === RatingSystemType.Stars && (
          <SettingSelect
            label={msg(
              "config.ui.editing.rating_system.star_precision.label",
              "Star precision",
            )}
            value={ratingStarPrecision}
            options={Array.from(ratingStarPrecisionIntlMap.entries()).map(
              ([value, intlId]) => ({
                value,
                label: intl.formatMessage({ id: intlId }),
              }),
            )}
            onChange={(v) =>
              saveUI("ratingSystemOptions", {
                type: ratingSystemType,
                starPrecision: v as RatingStarPrecision,
              })
            }
            disabled={savingUI}
            triggerClassName="w-36"
          />
        )}
      </SettingsSection>

      <OfflineSettingsSection />
    </div>
  );
}

export const Route = createFileRoute("/settings/interface")({
  component: SettingsInterfacePage,
});
