import { useState, type ComponentType } from "react";
import { useIntl } from "react-intl";
import { Star, X } from "lucide-react";
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
      onValueCommitted={(next) =>
        onCommit(Array.isArray(next) ? (next[0] ?? value) : next)
      }
      onPointerCancel={onCancel}
    />
  );
}

function RatingStars({
  value,
  onSetRating,
  disabled,
  valueRequired,
  size,
}: {
  value: number | null;
  onSetRating: (value: number | null) => void;
  disabled: boolean;
  valueRequired: boolean;
  size: "default" | "touch";
}) {
  const intl = useIntl();
  const [hoverStar, setHoverStar] = useState<number>();
  return (
    <div className="inline-flex items-center gap-1">
      {Array.from({ length: 5 }, (_, index) => index + 1).map((star) => {
        const fill =
          Math.max(0, Math.min(1, (hoverStar ?? value ?? 0) - star + 1)) * 100;
        return (
          <Button
            key={star}
            type="button"
            variant="transparent"
            size="icon-sm"
            disabled={disabled}
            className={cn("relative p-0", size === "touch" && "size-11")}
            onMouseEnter={() => !disabled && setHoverStar(star)}
            onMouseLeave={() => setHoverStar(undefined)}
            onFocus={() => !disabled && setHoverStar(star)}
            onBlur={() => setHoverStar(undefined)}
            onClick={() => {
              onSetRating(star === value && !valueRequired ? null : star);
              setHoverStar(undefined);
            }}
            aria-label={intl.formatMessage(
              {
                id: "rating_control.stars",
                defaultMessage: "{count, plural, one {# star} other {# stars}}",
              },
              { count: star },
            )}
          >
            <span className="relative block size-5">
              <Star
                className="size-5 text-muted-foreground"
                strokeWidth={1.5}
              />
              <span
                aria-hidden
                className="absolute inset-y-0 left-0 overflow-hidden"
                style={{ width: `${fill}%` }}
              >
                <Star
                  className="size-5 shrink-0 fill-yellow-400 text-yellow-400"
                  strokeWidth={1.5}
                />
              </span>
            </span>
          </Button>
        );
      })}
    </div>
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
  return (
    <FieldSet
      aria-label={msg("rating", "Rating")}
      className="flex min-w-0 flex-col gap-1"
    >
      <div className="flex min-w-0 flex-wrap items-center gap-1">
        {stars && (
          <RatingStars
            value={rating}
            onSetRating={setRating}
            disabled={readonly}
            valueRequired={valueRequired}
            size={size}
          />
        )}
        {showNumber && (
          <span className="text-sm tabular-nums text-muted-foreground">
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
            className={size === "touch" ? "size-11" : undefined}
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
          label={msg("rating", "Rating")}
          disabled={readonly}
          onChange={setPreview}
          onCommit={setRating}
          onCancel={() => setPreview(null)}
        />
      )}
    </FieldSet>
  );
}
