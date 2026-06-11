import { FormattedMessage } from "react-intl";
import { Badge } from "src/components/ui/badge";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "src/components/ui/tooltip";
import { cn } from "src/lib/utils";

export type MediaColorMetadata = {
  bit_depth?: number | null;
  color_range?: string | null;
  color_space?: string | null;
  color_transfer?: string | null;
  color_primaries?: string | null;
};

type MediaColorMode = "hdr" | "sdr" | "unknown";

const HDR_COLOR_TRANSFERS = new Set(["smpte2084", "arib-std-b67"]);
const UNKNOWN_COLOR_VALUES = new Set(["", "unknown", "unspecified", "n/a"]);

function normalizeColorValue(value: string | null | undefined): string {
  return value?.trim().toLocaleLowerCase() ?? "";
}

function hasKnownColorValue(value: string | null | undefined): boolean {
  return !UNKNOWN_COLOR_VALUES.has(normalizeColorValue(value));
}

function mediaColorMode(
  file: MediaColorMetadata | null | undefined,
): MediaColorMode {
  const transfer = normalizeColorValue(file?.color_transfer);
  if (!hasKnownColorValue(transfer)) {
    return "unknown";
  }

  return HDR_COLOR_TRANSFERS.has(transfer) ? "hdr" : "sdr";
}

function mediaColorModeRank(
  file: MediaColorMetadata | null | undefined,
): number {
  switch (mediaColorMode(file)) {
    case "hdr":
      return 2;
    case "sdr":
      return 1;
    case "unknown":
      return 0;
  }
}

function mediaColorLabel(file: MediaColorMetadata | null | undefined): string {
  const bitDepth = file?.bit_depth;
  const bitDepthLabel = bitDepth ? `${bitDepth}-bit` : "";

  switch (mediaColorMode(file)) {
    case "hdr":
      return ["HDR", bitDepthLabel].filter(Boolean).join(" ");
    case "sdr":
      return ["SDR", bitDepthLabel].filter(Boolean).join(" ");
    case "unknown":
      return bitDepthLabel;
  }
}

function mediaColorDetails(file: MediaColorMetadata | null | undefined) {
  return [
    {
      id: "media_info.bit_depth",
      defaultMessage: "Bit Depth",
      value: file?.bit_depth ? `${file.bit_depth}-bit` : undefined,
    },
    {
      id: "media_info.color_transfer",
      defaultMessage: "Transfer",
      value: file?.color_transfer,
    },
    {
      id: "media_info.color_primaries",
      defaultMessage: "Primaries",
      value: file?.color_primaries,
    },
    {
      id: "media_info.color_space",
      defaultMessage: "Color Space",
      value: file?.color_space,
    },
    {
      id: "media_info.color_range",
      defaultMessage: "Range",
      value: file?.color_range,
    },
  ].filter((detail) => hasKnownColorValue(detail.value));
}

export function mediaColorValueKey(
  file: MediaColorMetadata | null | undefined,
): string {
  return [
    mediaColorModeRank(file),
    file?.bit_depth ?? 0,
    normalizeColorValue(file?.color_transfer),
    normalizeColorValue(file?.color_primaries),
    normalizeColorValue(file?.color_space),
    normalizeColorValue(file?.color_range),
  ].join(":");
}

export function compareMediaColor(
  a: MediaColorMetadata | null | undefined,
  b: MediaColorMetadata | null | undefined,
  locale: string,
): number {
  return mediaColorValueKey(a).localeCompare(mediaColorValueKey(b), locale, {
    numeric: true,
    sensitivity: "base",
  });
}

export function MediaColorBadge({
  file,
  className,
}: {
  file: MediaColorMetadata | null | undefined;
  className?: string;
}) {
  const label = mediaColorLabel(file);
  const details = mediaColorDetails(file);
  const badge = (
    <Badge
      variant={
        mediaColorMode(file) === "hdr"
          ? "default"
          : label
            ? "outline"
            : "secondary"
      }
      className={cn(details.length > 0 && "cursor-help", className)}
    >
      {label || <FormattedMessage id="unknown" defaultMessage="Unknown" />}
    </Badge>
  );

  if (details.length === 0) {
    return badge;
  }

  return (
    <Tooltip>
      <TooltipTrigger render={badge} />
      <TooltipContent className="max-w-sm">
        <div className="grid gap-1">
          {details.map((detail) => (
            <div key={detail.id} className="grid grid-cols-[auto_1fr] gap-2">
              <span className="text-muted-foreground">
                <FormattedMessage
                  id={detail.id}
                  defaultMessage={detail.defaultMessage}
                />
              </span>
              <span>{detail.value}</span>
            </div>
          ))}
        </div>
      </TooltipContent>
    </Tooltip>
  );
}
