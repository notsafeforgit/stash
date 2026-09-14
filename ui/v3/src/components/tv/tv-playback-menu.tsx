import { FormattedMessage } from "react-intl";
import { useMsg } from "@/hooks/message";
import { Link } from "@tanstack/react-router";
import { Button, buttonVariants } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Dialog } from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import { TvDialogContent } from "./tv-dialog";
import { TvSettingsMenu } from "./tv-settings-menu";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { objectTitle } from "@/core/files";
import {
  useScenePlayerControls,
  useScenePlayerValue,
  useScenePlayerSourcesMenu,
} from "@/components/player/scene-player-controls";
import { ScenePlayerDeviceControls } from "@/components/player/scene-player-device-controls";
import { TvSlider } from "./tv-slider";
import { TvSelect } from "./tv-select";
import { tvActionLabels } from "./tv-action-labels";
import type { TvScene, useTvMutations } from "./use-tv-mutations";
export type TvPlaybackMenuAction =
  | "settings"
  | "info"
  | "rating"
  | "counter"
  | "volume"
  | "speed"
  | "subtitles"
  | "quality";

export function TvPlaybackMenu({
  action,
  scene,
  close,
  mutations,
  selectAction,
  openSettings,
}: {
  action: TvPlaybackMenuAction;
  scene: TvScene;
  close: () => void;
  mutations: ReturnType<typeof useTvMutations>;
  selectAction: (action: TvPlaybackMenuAction) => void;
  openSettings: () => void;
}) {
  const msg = useMsg();
  const controls = useScenePlayerControls();
  const volume = useScenePlayerValue("volume");
  const rate = useScenePlayerValue("rate");
  const muted = useScenePlayerValue("muted");
  const { sources, activeSource } = useScenePlayerSourcesMenu();
  // Share one modal across quick settings and playback panels so an outgoing
  // dialog cannot restore focus over the next panel's controls.
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <TvDialogContent
        title={
          action === "settings"
            ? msg("tv.text.quick_settings", "Quick settings")
            : msg(`tv.action.${action}`, tvActionLabels[action])
        }
      >
        {action === "settings" && (
          <TvSettingsMenu
            openPlayback={selectAction}
            openSettings={openSettings}
          />
        )}
        {action === "info" && (
          <div className="flex min-w-0 flex-col gap-4">
            <Link
              to="/scenes/$sceneId"
              params={{ sceneId: scene.id }}
              search={{ t: Math.floor(controls.read().position) }}
              className={cn(
                buttonVariants({ variant: "outline" }),
                "h-auto min-w-0 max-w-full justify-start whitespace-normal py-2 text-left [overflow-wrap:anywhere]",
              )}
            >
              {objectTitle(scene)}
            </Link>
            <p className="whitespace-pre-wrap">{scene.details}</p>
            <div className="flex flex-wrap gap-2">
              {scene.performers.map((performer) => (
                <Link
                  key={performer.id}
                  to="/performers/$performerId"
                  params={{ performerId: performer.id }}
                  className="min-w-0 max-w-full"
                >
                  <Badge
                    variant="secondary"
                    className="h-auto max-w-full whitespace-normal py-1 text-left [overflow-wrap:anywhere]"
                  >
                    {performer.name}
                  </Badge>
                </Link>
              ))}
            </div>
            {scene.studio && (
              <Link
                to="/studios/$studioId"
                params={{ studioId: scene.studio.id }}
              >
                {scene.studio.name}
              </Link>
            )}
            <div className="flex flex-wrap gap-2">
              {scene.tags.map((tag) => (
                <Link
                  key={tag.id}
                  to="/tags/$tagId"
                  params={{ tagId: tag.id }}
                  className="min-w-0 max-w-full"
                >
                  <Badge
                    variant="outline"
                    className="h-auto max-w-full whitespace-normal py-1 text-left [overflow-wrap:anywhere]"
                  >
                    {tag.name}
                  </Badge>
                </Link>
              ))}
            </div>
          </div>
        )}
        {action === "quality" && (
          <FieldGroup>
            <Field>
              <FieldLabel>
                <FormattedMessage
                  id="tv.text.current_item_quality"
                  defaultMessage="Current item quality"
                />
              </FieldLabel>
              <TvSelect
                label={msg(
                  "tv.text.current_item_quality",
                  "Current item quality",
                )}
                value={activeSource?.src ?? ""}
                options={sources.map((source) => ({
                  value: source.src,
                  label: source.label ?? "Stream",
                }))}
                onChange={controls.selectSource}
              />
            </Field>
            <Button variant="outline" onClick={controls.resetQuality}>
              <FormattedMessage
                id="tv.text.use_tv_default"
                defaultMessage="Use TV default"
              />
            </Button>
            <ScenePlayerDeviceControls />
            <p className="text-muted-foreground">
              <FormattedMessage
                id="tv.text.this_choice_applies_to_this_item_change_the_saved_default"
                defaultMessage="This choice applies to this item. Change the saved default in Settings → TV."
              />
            </p>
          </FieldGroup>
        )}
        {action === "volume" && (
          <FieldGroup>
            <TvSlider
              label={msg("tv.text.volume_2", "Volume")}
              value={volume}
              min={0}
              max={1}
              step={0.01}
              onChange={controls.setVolume}
            />
            <Button variant="outline" onClick={controls.toggleMuted}>
              {muted
                ? msg("tv.text.unmute", "Unmute")
                : msg("tv.text.mute", "Mute")}
            </Button>
          </FieldGroup>
        )}
        {action === "speed" && (
          <TvSelect
            label={msg("tv.text.playback_speed", "Playback speed")}
            value={String(rate)}
            options={[0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2, 3, 4].map(
              (rate) => ({ value: String(rate), label: `${rate}×` }),
            )}
            onChange={(value) => controls.setRate(Number(value))}
          />
        )}
        {action === "subtitles" && (
          <div className="flex flex-col gap-2">
            <Button variant="outline" onClick={() => controls.setCaption(null)}>
              <FormattedMessage id="tv.text.off" defaultMessage="Off" />
            </Button>
            {(scene.captions ?? []).map((caption, index) => (
              <Button
                variant="outline"
                key={`${caption.language_code}:${caption.caption_type}`}
                onClick={() => controls.setCaption(index)}
              >
                {caption.language_code} {caption.caption_type}
              </Button>
            ))}
            {!scene.captions?.length && (
              <p>
                <FormattedMessage
                  id="tv.text.no_subtitles_available"
                  defaultMessage="No subtitles available."
                />
              </p>
            )}
          </div>
        )}
        {action === "rating" && (
          <FieldGroup>
            <Field>
              <FieldLabel>
                <FormattedMessage
                  id="tv.text.scene_rating_0_100"
                  defaultMessage="Scene rating (0–100)"
                />
              </FieldLabel>
              <TvSlider
                label={msg("tv.text.scene_rating", "Scene rating")}
                value={scene.rating100 ?? 0}
                min={0}
                max={100}
                step={5}
                disabled={mutations.busy}
                onCommit={(rating100) => {
                  void mutations.run(scene.id, () =>
                    mutations.updateScene(scene.id, { rating100 }),
                  );
                }}
              />
            </Field>
            <p>{scene.rating100 ?? msg("tv.text.unrated", "Unrated")}</p>
            <Button
              variant="outline"
              onClick={() => {
                void mutations.run(scene.id, () =>
                  mutations.updateScene(scene.id, { rating100: null }),
                );
              }}
            >
              <FormattedMessage
                id="tv.text.clear_rating"
                defaultMessage="Clear rating"
              />
            </Button>
          </FieldGroup>
        )}
        {action === "counter" && (
          <div className="flex flex-wrap items-center gap-3">
            <Badge variant="secondary">{scene.o_counter ?? 0}</Badge>
            {(["add", "subtract", "reset"] as const).map((operation) => (
              <Button
                key={operation}
                variant="outline"
                disabled={mutations.busy}
                onClick={() => {
                  void mutations.run(scene.id, () =>
                    mutations.counter(scene.id, operation),
                  );
                }}
              >
                {operation === "add"
                  ? "+1"
                  : operation === "subtract"
                    ? "−1"
                    : msg("tv.text.reset", "Reset")}
              </Button>
            ))}
          </div>
        )}
      </TvDialogContent>
    </Dialog>
  );
}
