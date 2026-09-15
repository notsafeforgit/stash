import { FormattedMessage } from "react-intl";
import { useMsg } from "@/hooks/message";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { RatingSystem } from "@/components/ui/rating-system";
import { TvDialogContent } from "./tv-dialog";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
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
  | "rating"
  | "volume"
  | "speed"
  | "subtitles"
  | "quality";

export function TvPlaybackMenu({
  action,
  scene,
  close,
  mutations,
}: {
  action: TvPlaybackMenuAction;
  scene: TvScene;
  close: () => void;
  mutations: ReturnType<typeof useTvMutations>;
}) {
  const msg = useMsg();
  const controls = useScenePlayerControls();
  const volume = useScenePlayerValue("volume");
  const rate = useScenePlayerValue("rate");
  const muted = useScenePlayerValue("muted");
  const { sources, activeSource } = useScenePlayerSourcesMenu();
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <TvDialogContent
        title={msg(`tv.action.${action}`, tvActionLabels[action])}
      >
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
          <RatingSystem
            value={scene.rating100}
            size="touch"
            SliderComponent={TvSlider}
            disabled={mutations.busy}
            onSetRating={(rating100) => {
              void mutations.run(scene.id, () =>
                mutations.updateScene(scene.id, { rating100 }),
              );
            }}
          />
        )}
      </TvDialogContent>
    </Dialog>
  );
}
