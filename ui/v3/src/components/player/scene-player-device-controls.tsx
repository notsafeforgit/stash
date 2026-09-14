import { AirPlayButton, CastButton } from "@videojs/react";
import { Airplay, Cast, PictureInPicture } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useMsg } from "@/hooks/message";
import {
  useScenePlayerControls,
  useScenePlayerValue,
} from "./scene-player-controls";

/** Public device controls keep capability detection inside the player layer. */
export function ScenePlayerDeviceControls() {
  const controls = useScenePlayerControls();
  const canPip = useScenePlayerValue("canPip");
  const msg = useMsg();
  return (
    <div className="flex items-center gap-2">
      {canPip && (
        <Button
          variant="outline"
          size="icon-lg"
          aria-label={msg("tv.pip", "Picture in picture")}
          onClick={controls.togglePip}
        >
          <PictureInPicture />
        </Button>
      )}
      <AirPlayButton
        aria-label="AirPlay"
        render={
          <Button variant="outline" size="icon-lg">
            <Airplay />
          </Button>
        }
      />
      <CastButton
        aria-label={msg("tv.cast", "Cast")}
        render={
          <Button variant="outline" size="icon-lg">
            <Cast />
          </Button>
        }
      />
    </div>
  );
}
