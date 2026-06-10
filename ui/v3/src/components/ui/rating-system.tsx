import type React from "react";
import { useState } from "react";
import { Star, X } from "lucide-react";
import { useConfigurationContextOptional } from "src/hooks/config";
import {
  convertFromRatingFormat,
  convertToRatingFormat,
  defaultRatingSystemOptions,
  getRatingPrecision,
  RatingStarPrecision,
  RatingSystemType,
} from "src/utils/rating";
import { Slider } from "src/components/ui/slider";
import { Button } from "src/components/ui/button";
import { cn } from "src/lib/utils";

export interface IRatingSystemProps {
  value: number | null | undefined;
  onSetRating?: (value: number | null) => void;
  disabled?: boolean;
  valueRequired?: boolean;
  clickToRate?: boolean;
  withoutContext?: boolean;
}

// ── RatingStars ────────────────────────────────────────────────────────────────

interface RatingStarsProps {
  value: number | null;
  onSetRating?: (value: number | null) => void;
  disabled?: boolean;
  precision: RatingStarPrecision;
  valueRequired?: boolean;
}

const MAX_STARS = 5;

function RatingStars({
  value,
  onSetRating,
  disabled,
  precision,
  valueRequired,
}: RatingStarsProps) {
  const [hoverStar, setHoverStar] = useState<number | undefined>();
  const readonly = disabled || !onSetRating;

  const rating = convertToRatingFormat(value, {
    type: RatingSystemType.Stars,
    starPrecision: precision,
  });
  const currentStars = rating ? Math.floor(rating) : 0;
  const currentFraction = rating ? ((rating * 10) % 10) / 10 : 0;

  /** Fill percentage (0–100) for button `thisStar` (1-based). */
  function getFillPercent(thisStar: number): number {
    // While hovering, show whole-star preview
    if (hoverStar !== undefined) {
      return thisStar <= hoverStar ? 100 : 0;
    }
    // Otherwise reflect the actual value including any fraction
    if (thisStar <= currentStars) return 100;
    if (thisStar === currentStars + 1) return currentFraction * 100;
    return 0;
  }

  function handleClick(thisStar: number) {
    if (!onSetRating) return;

    const isCurrentStar =
      thisStar === currentStars + (currentFraction > 0 ? 1 : 0) ||
      (thisStar === currentStars && currentFraction === 0);

    if (isCurrentStar && !valueRequired) {
      onSetRating(null);
      setHoverStar(undefined);
      return;
    }

    onSetRating(convertFromRatingFormat(thisStar, RatingSystemType.Stars));
  }

  const displayRating =
    hoverStar !== undefined ? hoverStar : rating != null ? rating : undefined;

  const step = getRatingPrecision(precision);
  const showNumber = precision !== RatingStarPrecision.Full;

  return (
    <div className="inline-flex items-center gap-1">
      {Array.from({ length: MAX_STARS }, (_, i) => i + 1).map((thisStar) => {
        const fillPct = getFillPercent(thisStar);
        return (
          <button
            key={thisStar}
            type="button"
            disabled={readonly}
            className={cn(
              "relative p-0 size-7 bg-transparent border-0 flex items-center justify-center",
              "focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring/60 focus-visible:rounded-sm",
              !readonly &&
                "cursor-pointer hover:scale-110 transition-transform",
              readonly && "cursor-default opacity-70",
            )}
            onClick={() => handleClick(thisStar)}
            onMouseEnter={() => !readonly && setHoverStar(thisStar)}
            onMouseLeave={() => !readonly && setHoverStar(undefined)}
            aria-label={`${thisStar} star${thisStar !== 1 ? "s" : ""}`}
          >
            {/* Unfilled / outline star */}
            <Star className="size-5 text-muted-foreground" strokeWidth={1.5} />
            {/* Filled star — clipped to fillPct width */}
            {fillPct > 0 && (
              <div
                className="absolute inset-0 flex items-center justify-center overflow-hidden"
                style={{ width: `${fillPct}%` }}
              >
                <Star
                  className="size-5 text-yellow-400 fill-yellow-400 shrink-0"
                  strokeWidth={1.5}
                />
              </div>
            )}
          </button>
        );
      })}

      {/* Numeric label for fractional precisions */}
      {showNumber && (
        <span className="ml-0.5 w-6 text-sm tabular-nums text-muted-foreground">
          {displayRating != null && displayRating > 0
            ? displayRating.toFixed(step < 0.1 ? 2 : 1)
            : ""}
        </span>
      )}

      {/* Clear button */}
      {!readonly && value != null && (
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          onClick={() => onSetRating?.(null)}
          aria-label="Clear rating"
        >
          <X className="size-3.5" />
        </Button>
      )}
    </div>
  );
}

// ── RatingNumber (decimal / slider mode) ──────────────────────────────────────

interface RatingNumberProps {
  value: number | null;
  onSetRating?: (value: number | null) => void;
  disabled?: boolean;
}

function RatingNumber({ value, onSetRating, disabled }: RatingNumberProps) {
  const readonly = disabled || !onSetRating;

  return (
    <div className="flex items-center gap-3">
      <Slider
        min={0}
        max={100}
        step={1}
        value={value ?? 0}
        onValueChange={(v) => {
          const n = Array.isArray(v) ? (v as number[])[0] : (v as number);
          onSetRating?.(n === 0 ? null : n);
        }}
        disabled={readonly}
        className="flex-1"
      />
      <span className="w-8 text-right text-sm tabular-nums text-muted-foreground">
        {value != null ? (value / 10).toFixed(1) : "—"}
      </span>
      {!readonly && value != null && (
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          onClick={() => onSetRating?.(null)}
          aria-label="Clear rating"
        >
          <X className="size-3.5" />
        </Button>
      )}
    </div>
  );
}

// ── RatingSystem ───────────────────────────────────────────────────────────────

export const RatingSystem: React.FC<IRatingSystemProps> = ({
  value,
  onSetRating,
  disabled = false,
  valueRequired,
}) => {
  const ctx = useConfigurationContextOptional();
  const ratingSystemOptions =
    ctx?.configuration.ui.ratingSystemOptions ?? defaultRatingSystemOptions;

  if (ratingSystemOptions.type === RatingSystemType.Stars) {
    return (
      <RatingStars
        value={value ?? null}
        onSetRating={onSetRating}
        disabled={disabled}
        precision={
          ratingSystemOptions.starPrecision ?? RatingStarPrecision.Full
        }
        valueRequired={valueRequired}
      />
    );
  }

  return (
    <RatingNumber
      value={value ?? null}
      onSetRating={onSetRating}
      disabled={disabled}
    />
  );
};
