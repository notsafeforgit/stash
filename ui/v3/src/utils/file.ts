// Typescript currently does not implement the intl Unit interface
type Unit =
  | "byte"
  | "kibibyte"
  | "mebibyte"
  | "gibibyte"
  | "tebibyte"
  | "pebibyte";
const Units: Unit[] = [
  "byte",
  "kibibyte",
  "mebibyte",
  "gibibyte",
  "tebibyte",
  "pebibyte",
];
const shortUnits = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];

export function fileSize(bytes: number = 0): { size: number; unit: Unit } {
  if (Number.isNaN(parseFloat(String(bytes))) || !Number.isFinite(bytes))
    return { size: 0, unit: Units[0] };

  let unit = 0;
  let count = bytes;
  while (count >= 1024 && unit + 1 < Units.length) {
    count /= 1024;
    unit++;
  }

  return { size: count, unit: Units[unit] };
}

export function formatFileSizeUnit(u: Unit): string {
  const i = Units.indexOf(u);
  return shortUnits[i];
}

// Returns 0 for MB and under, 1 for GB and over.
export function fileSizeFractionalDigits(unit: Unit): number {
  if (Units.indexOf(unit) >= 3) {
    return 1;
  }
  return 0;
}

export function fileNameFromPath(path: string): string {
  if (!!path === false) return "No File Name";
  return path.replace(/^.*[\\/]/, "");
}

/** Basename without the final extension. Used for display titles derived from file paths. */
export function fileStemFromPath(path: string): string {
  const base = path.replace(/^.*[\\/]/, "");
  const dot = base.lastIndexOf(".");
  return dot > 0 ? base.slice(0, dot) : base;
}

export function bitRate(bitrate: number): string {
  const megabits = bitrate / 1000000;
  return `${megabits.toFixed(2)} megabits per second`;
}

export function resolution(width: number, height: number): string | undefined {
  const number = width > height ? height : width;
  if (number >= 6144) return "HUGE";
  if (number >= 3840) return "8K";
  if (number >= 3584) return "7K";
  if (number >= 3000) return "6K";
  if (number >= 2560) return "5K";
  if (number >= 1920) return "4K";
  if (number >= 1440) return "1440p";
  if (number >= 1080) return "1080p";
  if (number >= 720) return "720p";
  if (number >= 540) return "540p";
  if (number >= 480) return "480p";
  if (number >= 360) return "360p";
  if (number >= 240) return "240p";
  if (number >= 144) return "144p";
}
