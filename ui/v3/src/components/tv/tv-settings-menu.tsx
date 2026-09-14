import { ArrowRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { createTvAction } from "@/core/tv/action-config";
import { useMsg } from "@/hooks/message";
import { tvActionIcon, tvActionLabels } from "./tv-action-labels";
import type { TvPlaybackMenuAction } from "./tv-playback-menu";

const playbackActions = [
  "quality",
  "speed",
  "volume",
  "subtitles",
] as const satisfies readonly TvPlaybackMenuAction[];

export function TvSettingsMenu({
  openPlayback,
  openSettings,
}: {
  openPlayback: (action: TvPlaybackMenuAction) => void;
  openSettings: () => void;
}) {
  const msg = useMsg();
  return (
    <div className="flex flex-col gap-2">
      {playbackActions.map((action) => {
        const Icon = tvActionIcon(createTvAction(action, action));
        return (
          <Button
            key={action}
            variant="ghost"
            className="min-h-11 justify-start"
            onClick={() => openPlayback(action)}
          >
            <Icon data-icon="inline-start" />
            {msg(`tv.action.${action}`, tvActionLabels[action])}
          </Button>
        );
      })}
      <Separator className="my-2" />
      <Button variant="outline" className="min-h-11" onClick={openSettings}>
        {msg("tv.text.open_tv_settings", "Open TV settings")}
        <ArrowRight data-icon="inline-end" />
      </Button>
    </div>
  );
}
