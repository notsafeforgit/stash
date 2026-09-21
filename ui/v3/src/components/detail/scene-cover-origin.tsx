import { useIntl } from "react-intl";
import {
  SceneCoverOriginStatus,
  type SceneDataFragment,
} from "@/core/generated-graphql";
import { secondsToTimestamp } from "@/utils/duration";
import { MetaRow } from "./meta-row";

export function SceneCoverOrigin({
  scene,
}: {
  scene: Pick<SceneDataFragment, "cover_origin">;
}) {
  const intl = useIntl();
  const origin = scene.cover_origin;
  if (!origin) return null;
  const descriptions = {
    [SceneCoverOriginStatus.Available]: {
      id: "scene_cover.available",
      defaultMessage: "Regeneration reuses this frame from the original video.",
    },
    [SceneCoverOriginStatus.Changed]: {
      id: "scene_cover.changed",
      defaultMessage:
        "The original video has changed or was removed. The cover is kept; select a new frame to regenerate it.",
    },
    [SceneCoverOriginStatus.Unavailable]: {
      id: "scene_cover.unavailable",
      defaultMessage:
        "The original video is unavailable. The cover is kept; restore the video or select a new frame.",
    },
    [SceneCoverOriginStatus.Unknown]: {
      id: "scene_cover.unknown",
      defaultMessage:
        "The original frame is unknown. The cover is kept; select a frame to enable regeneration.",
    },
  } satisfies Record<
    SceneCoverOriginStatus,
    { id: string; defaultMessage: string }
  >;
  return (
    <MetaRow
      label={intl.formatMessage({
        id: "scene_cover.frame",
        defaultMessage: "Cover frame",
      })}
    >
      <div className="flex flex-col gap-1">
        {origin.at != null && (
          <span data-selectable-text>
            {secondsToTimestamp(origin.at, true)}
          </span>
        )}
        <span className="text-muted-foreground">
          {intl.formatMessage(descriptions[origin.status])}
        </span>
      </div>
    </MetaRow>
  );
}
