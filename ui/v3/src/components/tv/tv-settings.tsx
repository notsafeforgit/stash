import { FormattedMessage, useIntl } from "react-intl";
import { lazy, Suspense, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useForm, useStore } from "@tanstack/react-form";
import { useTvSettings } from "@/hooks/use-tv-settings";
import { useMsg } from "@/hooks/message";
import {
  defaultTvSettings,
  tvSettingsSchema,
  tvModeSchema,
  type TvSettings,
} from "@/core/tv/settings";
import { qualityTiers, qualityHeight } from "@/core/player-quality";
import { getFilterOptions } from "@/models/list-filter/factory";
import {
  formatSortLabel,
  formatSortOptions,
} from "@/models/list-filter/labels";
import { tvFilterMode } from "@/core/tv/feed-query";
import { TvFilterSelect } from "@/components/tv/tv-filter-select";
import { TvSelect } from "@/components/tv/tv-select";
import { Button, buttonVariants } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  SettingNumber,
  SettingSelect,
  SettingsSection,
  SettingSwitch,
} from "@/components/settings/setting-row";
import { Field, FieldLabel, FieldGroup } from "@/components/ui/field";
import { DestructiveConfirmDialog } from "@/components/shared/destructive-confirm-dialog";

const TvRailEditor = lazy(() => import("@/components/tv/tv-rail-editor"));

function TvSettingsForm({ initial }: { initial: TvSettings }) {
  const adapter = useTvSettings();
  const intl = useIntl();
  const msg = useMsg();
  const [railOpened, setRailOpened] = useState(false);
  const [resetOpen, setResetOpen] = useState(false);
  const form = useForm({
    defaultValues: { settings: initial },
    onSubmit: async ({ value, formApi }) => {
      const parsed = tvSettingsSchema.safeParse(value.settings);
      if (!parsed.success) return;
      if (!(await adapter.save(parsed.data))) return;
      formApi.reset({ settings: parsed.data });
    },
  });
  const values = useStore(form.store, (state) => state.values.settings);
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const validation = tvSettingsSchema.safeParse(values);
  const set = <K extends keyof TvSettings>(key: K, value: TvSettings[K]) =>
    form.setFieldValue("settings", (previous) => ({
      ...previous,
      [key]: value,
    }));
  const modes = [
    { value: "scenes", label: msg("tv.text.scenes", "Scenes") },
    { value: "markers", label: msg("tv.text.markers", "Markers") },
  ];
  const sortOptions = formatSortOptions(
    intl,
    getFilterOptions(tvFilterMode(values.mode)).sortByOptions,
  );
  if (
    values.sort &&
    !sortOptions.some((option) => option.value === values.sort)
  ) {
    sortOptions.push({
      value: values.sort,
      label: formatSortLabel(intl, undefined),
    });
  }
  return (
    <form
      className="flex max-w-3xl flex-col gap-8 p-6"
      onSubmit={(event) => {
        event.preventDefault();
        event.stopPropagation();
        void form.handleSubmit();
      }}
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-xl font-semibold">{msg("tv.title", "TV")}</h2>
        <Link to="/tv" className={buttonVariants({ variant: "outline" })}>
          {msg("tv.open", "Open TV")}
        </Link>
      </div>
      <SettingsSection title={msg("tv.settings.feed", "Feed")}>
        <SettingSelect
          label={msg("tv.settings.mode", "Default feed")}
          value={values.mode}
          options={modes}
          onChange={(next) => {
            const result = tvModeSchema.safeParse(next);
            if (result.success) {
              const options = getFilterOptions(tvFilterMode(result.data));
              form.setFieldValue("settings", (previous) => ({
                ...previous,
                mode: result.data,
                sort: options.sortByOptions.some(
                  (option) => option.value === previous.sort,
                )
                  ? previous.sort
                  : null,
              }));
            }
          }}
        />
        <Field>
          <FieldLabel>
            {msg("tv.settings.scene_filter", "Scene filter")}
          </FieldLabel>
          <TvFilterSelect
            mode="scenes"
            label={msg("tv.text.scene_filter", "Scene filter")}
            value={values.sceneFilter}
            onChange={(next) => set("sceneFilter", next)}
          />
        </Field>
        <Field>
          <FieldLabel>
            {msg("tv.settings.marker_filter", "Marker filter")}
          </FieldLabel>
          <TvFilterSelect
            mode="markers"
            label={msg("tv.text.marker_filter", "Marker filter")}
            value={values.markerFilter}
            onChange={(next) => set("markerFilter", next)}
          />
        </Field>
        <SettingSelect
          label={msg("tv.settings.sort", "Sort order")}
          value={values.sort ?? "saved"}
          options={[
            {
              value: "saved",
              label: msg(
                "tv.text.use_saved_filter_order",
                "Use saved filter order",
              ),
            },
            ...sortOptions,
          ]}
          onChange={(value) => set("sort", value === "saved" ? null : value)}
        />
        <SettingSelect
          label={msg("tv.settings.direction", "Sort direction")}
          value={values.direction}
          options={[
            { value: "ASC", label: msg("tv.text.ascending", "Ascending") },
            { value: "DESC", label: msg("tv.text.descending", "Descending") },
          ]}
          onChange={(value) => {
            if (value === "ASC" || value === "DESC") set("direction", value);
          }}
          disabled={!values.sort}
        />
        <SettingSelect
          label={msg("tv.settings.orientation", "Media orientation")}
          value={values.orientation}
          options={[
            {
              value: "all",
              label: msg("tv.text.all_orientations", "All orientations"),
            },
            {
              value: "match",
              label: msg(
                "tv.text.match_the_viewing_surface",
                "Match the viewing surface",
              ),
            },
            {
              value: "portrait",
              label: msg("tv.text.portrait_and_square", "Portrait and square"),
            },
            {
              value: "landscape",
              label: msg(
                "tv.text.landscape_and_square",
                "Landscape and square",
              ),
            },
          ]}
          onChange={(value) => {
            const parsed = tvSettingsSchema.shape.orientation.safeParse(value);
            if (parsed.success) set("orientation", parsed.data);
          }}
        />
        <SettingNumber
          label={msg("tv.settings.page_size", "Items per page")}
          value={values.pageSize}
          min={5}
          max={50}
          onChange={(value) => set("pageSize", value)}
        />
        <SettingNumber
          label={msg(
            "tv.settings.prefetch",
            "Load the next page with this many items remaining",
          )}
          value={values.prefetch}
          min={1}
          max={5}
          onChange={(value) => set("prefetch", value)}
        />
        <SettingSwitch
          label={msg("tv.settings.limit_enabled", "Limit items per session")}
          checked={values.itemLimit !== null}
          onChange={(value) => set("itemLimit", value ? 100 : null)}
        />
        {values.itemLimit !== null && (
          <SettingNumber
            label={msg("tv.settings.limit", "Item limit")}
            value={values.itemLimit}
            min={1}
            max={100000}
            onChange={(value) => set("itemLimit", value)}
          />
        )}
      </SettingsSection>
      <SettingsSection title={msg("tv.settings.playback", "Playback")}>
        <SettingSwitch
          label={msg("tv.settings.autoplay", "Autoplay")}
          description={msg(
            "tv.settings.autoplay_description",
            "Also respects the app’s Auto-start video setting and your browser’s playback permission.",
          )}
          checked={values.autoplay}
          onChange={(value) => set("autoplay", value)}
        />
        <SettingSelect
          label={msg(
            "tv.settings.quality",
            "Default quality for scenes and markers",
          )}
          description={msg(
            "tv.settings.quality_description",
            "Lower resolutions use the normal scene stream. A fixed quality never silently falls back to a higher resolution or the original file.",
          )}
          value={
            values.defaultQuality.kind === "best"
              ? "best"
              : values.defaultQuality.resolution
          }
          options={[
            {
              value: "best",
              label: msg("tv.text.best_available", "Best available"),
            },
            ...qualityTiers.map((value) => ({
              value,
              label: value === "FOUR_K" ? "4K" : `${qualityHeight[value]}p`,
            })),
          ]}
          onChange={(value) => {
            const tier = qualityTiers.find((tier) => tier === value);
            if (value === "best") set("defaultQuality", { kind: "best" });
            else if (tier)
              set("defaultQuality", { kind: "fixed", resolution: tier });
          }}
        />
        <SettingSelect
          label={msg("tv.settings.start", "Start scenes at")}
          value={values.start}
          options={[
            {
              value: "resume",
              label: msg(
                "tv.text.saved_resume_position",
                "Saved resume position",
              ),
            },
            {
              value: "beginning",
              label: msg("tv.text.beginning", "Beginning"),
            },
            {
              value: "random-marker",
              label: msg(
                "tv.text.random_marker_or_random_position",
                "Random marker, or random position",
              ),
            },
            {
              value: "random-position",
              label: msg("tv.text.random_position", "Random position"),
            },
          ]}
          onChange={(value) => {
            const parsed = tvSettingsSchema.shape.start.safeParse(value);
            if (parsed.success) set("start", parsed.data);
          }}
        />
        <SettingSelect
          label={msg("tv.settings.window", "Scene playback length")}
          value={values.window.kind}
          options={[
            {
              value: "full",
              label: msg("tv.text.to_scene_end", "To scene end"),
            },
            {
              value: "fixed",
              label: msg("tv.text.fixed_length", "Fixed length"),
            },
            {
              value: "random",
              label: msg("tv.text.random_length", "Random length"),
            },
          ]}
          onChange={(kind) => {
            if (kind === "full") set("window", { kind });
            else if (kind === "fixed") set("window", { kind, seconds: 60 });
            else if (kind === "random")
              set("window", { kind, min: 30, max: 90 });
          }}
        />
        {values.window.kind === "fixed" && (
          <SettingNumber
            label={msg("tv.settings.seconds", "Length in seconds")}
            value={values.window.seconds}
            min={1}
            max={86400}
            onChange={(seconds) => set("window", { kind: "fixed", seconds })}
          />
        )}
        {values.window.kind === "random" && (
          <>
            <SettingNumber
              label={msg("tv.settings.min_seconds", "Minimum seconds")}
              value={values.window.min}
              min={1}
              max={86400}
              onChange={(min) => {
                if (values.window.kind === "random")
                  set("window", { ...values.window, min });
              }}
            />
            <SettingNumber
              label={msg("tv.settings.max_seconds", "Maximum seconds")}
              value={values.window.max}
              min={1}
              max={86400}
              onChange={(max) => {
                if (values.window.kind === "random")
                  set("window", { ...values.window, max });
              }}
            />
          </>
        )}
        <SettingSelect
          label={msg("tv.settings.completion", "At the end of playback")}
          value={values.completion}
          options={[
            { value: "advance", label: msg("tv.text.next_item", "Next item") },
            { value: "loop", label: msg("tv.text.loop_2", "Loop") },
            { value: "normal", label: msg("tv.text.stop", "Stop") },
          ]}
          onChange={(value) => {
            const parsed = tvSettingsSchema.shape.completion.safeParse(value);
            if (parsed.success) set("completion", parsed.data);
          }}
        />
      </SettingsSection>
      <SettingsSection title={msg("tv.settings.presentation", "Presentation")}>
        <SettingSelect
          label={msg("tv.settings.fit", "Video fit")}
          value={values.fit}
          options={[
            {
              value: "contain",
              label: msg("tv.text.fit_entire_video", "Fit entire video"),
            },
            {
              value: "cover",
              label: msg("tv.text.fill_and_crop", "Fill and crop"),
            },
          ]}
          onChange={(value) => {
            if (value === "contain" || value === "cover") set("fit", value);
          }}
        />
        <SettingSwitch
          label={msg(
            "tv.settings.left_handed",
            "Place the action rail on the left",
          )}
          checked={values.leftHanded}
          onChange={(value) => set("leftHanded", value)}
        />
        <SettingSwitch
          label={msg("tv.settings.ui_visible", "Show controls initially")}
          checked={values.uiVisible}
          onChange={(value) => set("uiVisible", value)}
        />
        <SettingSelect
          label={msg("tv.settings.rotation", "Rotation on this browser")}
          description={msg(
            "tv.settings.rotation_description",
            "Saved only for this browser and server. The video remains inline, with the TV controls available.",
          )}
          value={adapter.rotation}
          options={[
            { value: "normal", label: msg("tv.text.normal_2", "Normal") },
            {
              value: "clockwise",
              label: msg("tv.text.clockwise", "Clockwise"),
            },
            {
              value: "counterclockwise",
              label: msg("tv.text.counterclockwise", "Counterclockwise"),
            },
          ]}
          onChange={(value) => {
            if (
              value === "normal" ||
              value === "clockwise" ||
              value === "counterclockwise"
            )
              adapter.setRotation(value);
          }}
        />
      </SettingsSection>
      <SettingsSection
        title={msg("tv.settings.rules", "Additional feed rules")}
        description={msg(
          "tv.settings.rules_description",
          "Combine saved filters with AND. Their nested include/exclude criteria are preserved. Text search and ordering belong to the main feed filter.",
        )}
      >
        {values.rules.map((rule, index) => (
          <FieldGroup key={`${rule.mode}:${index}`}>
            <TvSelect
              label={msg("tv.text.rule_feed", "Rule feed")}
              value={rule.mode}
              options={[
                { value: "scenes", label: msg("tv.text.scenes", "Scenes") },
                { value: "markers", label: msg("tv.text.markers", "Markers") },
              ]}
              onChange={(mode) =>
                set(
                  "rules",
                  values.rules.map((item, i) =>
                    i === index ? { ...item, mode } : item,
                  ),
                )
              }
            />
            <TvFilterSelect
              savedOnly
              label={msg("tv.text.additional_filter", "Additional filter")}
              mode={rule.mode}
              value={{ kind: "saved", id: rule.filterId }}
              onChange={(choice) => {
                if (choice.kind === "saved")
                  set(
                    "rules",
                    values.rules.map((item, i) =>
                      i === index ? { ...item, filterId: choice.id } : item,
                    ),
                  );
              }}
            />
            <Button
              type="button"
              variant="outline"
              onClick={() =>
                set(
                  "rules",
                  values.rules.filter((_, i) => i !== index),
                )
              }
            >
              <FormattedMessage
                id="tv.text.remove_rule"
                defaultMessage="Remove rule"
              />
            </Button>
          </FieldGroup>
        ))}
        <Button
          type="button"
          variant="outline"
          onClick={() =>
            set("rules", [
              ...values.rules,
              { kind: "filter", mode: values.mode, filterId: "" },
            ])
          }
        >
          <FormattedMessage
            id="tv.text.add_filter_rule"
            defaultMessage="Add filter rule"
          />
        </Button>
      </SettingsSection>
      <SettingsSection title={msg("tv.settings.rail", "Action rail")}>
        <Button
          type="button"
          variant="outline"
          onClick={() => setRailOpened(true)}
        >
          {msg("tv.settings.customize_rail", "Customize action rail")}
        </Button>
        {railOpened && (
          <Suspense fallback={<Spinner />}>
            <TvRailEditor
              value={values.rail}
              onChange={(value) => set("rail", value)}
            />
          </Suspense>
        )}
      </SettingsSection>
      {!validation.success && (
        <Alert variant="destructive">
          <AlertTitle>
            <FormattedMessage
              id="tv.text.check_tv_settings"
              defaultMessage="Check TV settings"
            />
          </AlertTitle>
          <AlertDescription>
            {validation.error.issues.map((issue) => issue.message).join("; ")}
          </AlertDescription>
        </Alert>
      )}
      <div className="sticky bottom-0 flex flex-wrap gap-3 border-t bg-background py-3">
        <Button type="submit" disabled={submitting || !validation.success}>
          {submitting && <Spinner data-icon="inline-start" />}
          {msg("tv.settings.save", "Save TV settings")}
        </Button>
        <Button
          type="button"
          variant="outline"
          onClick={() => setResetOpen(true)}
        >
          {msg("tv.settings.reset", "Reset TV settings")}
        </Button>
      </div>
      <DestructiveConfirmDialog
        open={resetOpen}
        onOpenChange={setResetOpen}
        title={msg("tv.text.reset_tv_settings", "Reset TV settings?")}
        onConfirm={async () => {
          if (!(await adapter.reset())) return;
          adapter.setRotation("normal");
          form.reset({ settings: defaultTvSettings });
          setResetOpen(false);
        }}
      >
        <FormattedMessage
          id="tv.text.restore_the_default_feed_playback_and_action_rail_settings"
          defaultMessage="Restore the default feed, playback and action rail settings."
        />
      </DestructiveConfirmDialog>
    </form>
  );
}

export function SettingsTvPage() {
  const msg = useMsg();
  const adapter = useTvSettings();
  const [resetOpen, setResetOpen] = useState(false);
  if (adapter.result.kind === "ready")
    return <TvSettingsForm initial={adapter.result.settings} />;
  return (
    <div className="flex max-w-2xl flex-col gap-4 p-6">
      <Alert>
        <AlertTitle>
          <FormattedMessage
            id="tv.text.tv_settings_need_recovery"
            defaultMessage="TV settings need recovery"
          />
        </AlertTitle>
        <AlertDescription>
          {adapter.result.message}
          <FormattedMessage
            id="tv.text.the_original_settings_are_preserved_including_unknown_action_types_or"
            defaultMessage="The original settings are preserved, including unknown action types or future versions."
          />
        </AlertDescription>
      </Alert>
      <Button onClick={() => setResetOpen(true)}>
        <FormattedMessage
          id="tv.text.reset_tv_settings_2"
          defaultMessage="Reset TV settings"
        />
      </Button>
      <DestructiveConfirmDialog
        open={resetOpen}
        onOpenChange={setResetOpen}
        title={msg(
          "tv.text.replace_unreadable_tv_settings",
          "Replace unreadable TV settings?",
        )}
        onConfirm={async () => {
          if (!(await adapter.reset())) return;
          setResetOpen(false);
        }}
      >
        <FormattedMessage
          id="tv.text.this_replaces_the_saved_tv_document_with_defaults_other_app"
          defaultMessage="This replaces the saved TV document with defaults. Other app settings are preserved."
        />
      </DestructiveConfirmDialog>
    </div>
  );
}
