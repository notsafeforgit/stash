import { FormattedMessage } from "react-intl";
import { Dialog } from "@/components/ui/dialog";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { tvModeSchema, type TvMode } from "@/core/tv/settings";
import { useMsg } from "@/hooks/message";
import { TvDialogContent } from "./tv-dialog";

export const tvModeLabels = {
  scenes: { id: "tv.text.scenes", defaultMessage: "Scenes" },
  markers: { id: "tv.text.markers", defaultMessage: "Markers" },
} satisfies Record<TvMode, { id: string; defaultMessage: string }>;

export function TvFeedMenu({
  mode,
  open,
  onOpenChange,
  onChange,
}: {
  mode: TvMode;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onChange: (mode: TvMode) => void;
}) {
  const msg = useMsg();
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <TvDialogContent
        title={msg("tv.action.feed", "Feed")}
        description={msg(
          "tv.feed.description",
          "Switch between scenes and markers for this viewing session.",
        )}
      >
        <ToggleGroup<TvMode>
          variant="outline"
          aria-label={msg("tv.action.feed", "Feed")}
          value={[mode]}
          onValueChange={(values) => {
            const next = tvModeSchema.safeParse(values[0]);
            if (next.success) onChange(next.data);
          }}
          className="w-full"
        >
          {tvModeSchema.options.map((value) => (
            <ToggleGroupItem
              key={value}
              value={value}
              className="min-h-11 min-w-0 flex-1"
            >
              <FormattedMessage {...tvModeLabels[value]} />
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </TvDialogContent>
    </Dialog>
  );
}
