import { useState, type ComponentType } from "react";
import { useIntl } from "react-intl";
import { Rating } from "@ui-sdk/react/rating";
import { X } from "lucide-react";
import { useConfigurationContextOptional } from "@/hooks/config";
import { useMsg } from "@/hooks/message";
import {
  convertFromRatingFormat,
  convertToRatingFormat,
  defaultRatingSystemOptions,
  getRatingPrecision,
  RatingStarPrecision,
  RatingSystemType,
} from "@/utils/rating";
import { Slider } from "@/components/ui/slider";
import { Button } from "@/components/ui/button";
import { FieldSet } from "@/components/ui/field";
import { cn } from "@/lib/utils";

/** A small value/commit boundary lets rotated media surfaces supply their
 * coordinate-aware slider without coupling shared ratings to a player. */
export interface RatingSliderProps {
  value: number;
  min: number;
  max: number;
  step: number;
  label: string;
  disabled?: boolean;
  onChange: (value: number) => void;
  onCommit: (value: number) => void;
  onCancel: () => void;
}

export interface IRatingSystemProps {
  value: number | null | undefined;
  onSetRating?: (value: number | null) => void;
  disabled?: boolean;
  valueRequired?: boolean;
  clickToRate?: boolean;
  withoutContext?: boolean;
  size?: "default" | "touch";
  SliderComponent?: ComponentType<RatingSliderProps>;
}

function RatingSlider({
  value,
  label,
  onChange,
  onCommit,
  onCancel,
  ...props
}: RatingSliderProps) {
  return (
    <Slider
      {...props}
      aria-label={label}
      value={[value]}
      className="py-3"
      onValueChange={(next) =>
        onChange(Array.isArray(next) ? (next[0] ?? value) : next)
      }
      onValueCommit={(next) =>
        onCommit(Array.isArray(next) ? (next[0] ?? value) : next)
      }
      onPointerCancel={onCancel}
    />
  );
}

export function RatingSystem({
  value,
  onSetRating,
  disabled = false,
  valueRequired = false,
  size = "default",
  SliderComponent = RatingSlider,
}: IRatingSystemProps) {
  const ctx = useConfigurationContextOptional();
  const msg = useMsg();
  const intl = useIntl();
  const options =
    ctx?.configuration.ui.ratingSystemOptions ?? defaultRatingSystemOptions;
  const stars = options.type === RatingSystemType.Stars;
  const precision = options.starPrecision ?? RatingStarPrecision.Full;
  const step = stars ? getRatingPrecision(precision) : 0.1;
  const readonly = disabled || !onSetRating;
  // Pointer previews are local. Persist one rating on release/keyboard commit,
  // rather than starting a mutation for every intermediate slider position.
  const [preview, setPreview] = useState<number | null>(null);
  const rating = preview ?? convertToRatingFormat(value, options);
  const setRating = (next: number | null) => {
    setPreview(null);
    onSetRating?.(
      next == null || next === 0
        ? null
        : convertFromRatingFormat(next, options.type),
    );
  };
  const showNumber = !stars || precision !== RatingStarPrecision.Full;
  const label = msg("rating", "Rating");
  return (
    <FieldSet aria-label={label} className="flex min-w-0 flex-col gap-1">
      <div className="flex min-w-0 flex-wrap items-center gap-1">
        {stars && (
          <Rating
            aria-label={onSetRating ? label : undefined}
            data-size={size === "touch" ? "touch" : undefined}
            value={rating}
            max={5}
            step={precision === RatingStarPrecision.Half ? 0.5 : 1}
            disabled={disabled}
            readOnly={!onSetRating}
            deselectable={!valueRequired}
            getValueLabel={(count) =>
              intl.formatMessage(
                {
                  id: "rating_control.stars",
                  defaultMessage:
                    "{count, plural, one {# star} other {# stars}}",
                },
                { count },
              )
            }
            onValueChange={setRating}
          />
        )}
        {showNumber && (
          <span data-slot="rating-value">
            {rating == null
              ? "—"
              : intl.formatNumber(rating, {
                  minimumFractionDigits: step < 1 ? 1 : 0,
                  maximumFractionDigits: 2,
                })}
          </span>
        )}
        {onSetRating && !valueRequired && value != null && (
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            className={cn(size === "touch" && "size-11")}
            disabled={disabled}
            onClick={() => setRating(null)}
            aria-label={msg("rating_control.clear", "Clear rating")}
          >
            <X />
          </Button>
        )}
      </div>
      {showNumber && (onSetRating || !stars) && (
        <SliderComponent
          value={rating ?? 0}
          min={valueRequired ? step : 0}
          max={stars ? 5 : 10}
          step={step}
          label={label}
          disabled={readonly}
          onChange={setPreview}
          onCommit={setRating}
          onCancel={() => setPreview(null)}
        />
      )}
    </FieldSet>
  );
}
