import { useIntl } from "react-intl";
import type * as GQL from "src/core/generated-graphql";
import { Input } from "src/components/ui/input";
import { Label } from "src/components/ui/label";

export type VideoPreviewSettingsInput = Pick<
  GQL.GeneratePreviewOptionsInput,
  | "previewSegments"
  | "previewSegmentDuration"
  | "previewExcludeStart"
  | "previewExcludeEnd"
>;

interface IProps {
  value: VideoPreviewSettingsInput;
  onChange: (v: VideoPreviewSettingsInput) => void;
}

/**
 * Per-run overrides for video preview generation. Each field is optional —
 * unset values fall through to system config at submit time.
 *
 * Mirrors v2.5's Settings/GeneratePreviewOptions VideoPreviewInput.
 */
export function VideoPreviewInput({ value, onChange }: IProps) {
  const intl = useIntl();

  function set(v: Partial<VideoPreviewSettingsInput>) {
    onChange({ ...value, ...v });
  }

  return (
    <div className="space-y-3">
      <div className="space-y-1">
        <Label htmlFor="vpi-segments">
          {intl.formatMessage({
            id: "dialogs.scene_gen.preview_seg_count_head",
            defaultMessage: "Number of segments in preview",
          })}
        </Label>
        <Input
          id="vpi-segments"
          type="number"
          min={1}
          value={value.previewSegments ?? ""}
          onChange={(e) => {
            const raw = e.currentTarget.value;
            set({
              previewSegments:
                raw === "" ? undefined : Number.parseInt(raw, 10),
            });
          }}
        />
        <p className="text-xs text-muted-foreground">
          {intl.formatMessage({
            id: "dialogs.scene_gen.preview_seg_count_desc",
            defaultMessage: "Number of segments in preview files.",
          })}
        </p>
      </div>

      <div className="space-y-1">
        <Label htmlFor="vpi-duration">
          {intl.formatMessage({
            id: "dialogs.scene_gen.preview_seg_duration_head",
            defaultMessage: "Preview segment duration",
          })}
        </Label>
        <Input
          id="vpi-duration"
          type="number"
          step="0.1"
          min={0}
          value={value.previewSegmentDuration ?? ""}
          onChange={(e) => {
            const raw = e.currentTarget.value;
            set({
              previewSegmentDuration:
                raw === "" ? undefined : Number.parseFloat(raw),
            });
          }}
        />
        <p className="text-xs text-muted-foreground">
          {intl.formatMessage({
            id: "dialogs.scene_gen.preview_seg_duration_desc",
            defaultMessage: "Duration of each preview segment, in seconds.",
          })}
        </p>
      </div>

      <div className="space-y-1">
        <Label htmlFor="vpi-exclude-start">
          {intl.formatMessage({
            id: "dialogs.scene_gen.preview_exclude_start_time_head",
            defaultMessage: "Exclude start time",
          })}
        </Label>
        <Input
          id="vpi-exclude-start"
          value={value.previewExcludeStart ?? ""}
          onChange={(e) =>
            set({ previewExcludeStart: e.currentTarget.value || undefined })
          }
          spellCheck={false}
        />
        <p className="text-xs text-muted-foreground">
          {intl.formatMessage({
            id: "dialogs.scene_gen.preview_exclude_start_time_desc",
            defaultMessage:
              "Exclude the first x seconds from scene previews. Value in seconds or a percentage (e.g. 2%).",
          })}
        </p>
      </div>

      <div className="space-y-1">
        <Label htmlFor="vpi-exclude-end">
          {intl.formatMessage({
            id: "dialogs.scene_gen.preview_exclude_end_time_head",
            defaultMessage: "Exclude end time",
          })}
        </Label>
        <Input
          id="vpi-exclude-end"
          value={value.previewExcludeEnd ?? ""}
          onChange={(e) =>
            set({ previewExcludeEnd: e.currentTarget.value || undefined })
          }
          spellCheck={false}
        />
        <p className="text-xs text-muted-foreground">
          {intl.formatMessage({
            id: "dialogs.scene_gen.preview_exclude_end_time_desc",
            defaultMessage:
              "Exclude the last x seconds from scene previews. Value in seconds or a percentage (e.g. 2%).",
          })}
        </p>
      </div>
    </div>
  );
}
