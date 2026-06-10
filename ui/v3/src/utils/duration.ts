class DurationUnit {
  static readonly SECOND: DurationUnit = new DurationUnit("s", 1);
  static readonly MINUTE: DurationUnit = new DurationUnit("m", 60);
  static readonly HOUR: DurationUnit = new DurationUnit(
    "h",
    DurationUnit.MINUTE.secs * 60,
  );
  static readonly DAY: DurationUnit = new DurationUnit(
    "D",
    DurationUnit.HOUR.secs * 24,
  );
  static readonly WEEK: DurationUnit = new DurationUnit(
    "W",
    DurationUnit.DAY.secs * 7,
  );
  static readonly MONTH: DurationUnit = new DurationUnit(
    "M",
    DurationUnit.DAY.secs * 30,
  );
  static readonly YEAR: DurationUnit = new DurationUnit(
    "Y",
    DurationUnit.DAY.secs * 365,
  );

  static readonly DURATIONS: DurationUnit[] = [
    DurationUnit.SECOND,
    DurationUnit.MINUTE,
    DurationUnit.HOUR,
    DurationUnit.DAY,
    DurationUnit.WEEK,
    DurationUnit.MONTH,
    DurationUnit.YEAR,
  ];

  private constructor(
    private readonly shortString: string,
    public secs: number,
  ) {}

  toString() {
    return this.shortString;
  }
}

class DurationCount {
  public constructor(
    public readonly count: number,
    public readonly duration: DurationUnit,
  ) {}

  toString() {
    return this.count.toString() + this.duration.toString();
  }
}

function secondsAsTime(seconds: number = 0): DurationCount[] {
  if (Number.isNaN(parseFloat(String(seconds))) || !Number.isFinite(seconds))
    return [new DurationCount(0, DurationUnit.DURATIONS[0])];

  const result = [];
  let remainingSeconds = seconds;
  for (let i = DurationUnit.DURATIONS.length - 1; i >= 0; i--) {
    const q = Math.floor(remainingSeconds / DurationUnit.DURATIONS[i].secs);
    if (q !== 0) {
      remainingSeconds %= DurationUnit.DURATIONS[i].secs;
      result.push(new DurationCount(q, DurationUnit.DURATIONS[i]));
    }
  }
  return result;
}

export function secondsAsTimeString(
  seconds: number = 0,
  maxUnitCount: number = 2,
): string {
  return secondsAsTime(seconds).slice(0, maxUnitCount).join(" ");
}

// Converts seconds to a [hh:]mm:ss[.ffff] where hh is only shown if hours is
// non-zero, and ffff is shown only if includeMS is set and seconds has a
// fractional component. A negative input results in a -hh:mm:ss or -mm:ss output.
export function secondsToTimestamp(
  secondsInput: number,
  includeMS?: boolean,
): string {
  const neg = secondsInput < 0;
  const absSeconds = neg ? -secondsInput : secondsInput;

  const fracSeconds = absSeconds % 1;
  const ms = Math.round(fracSeconds * 1000);

  let seconds = Math.trunc(absSeconds);

  const s = seconds % 60;
  seconds = (seconds - s) / 60;

  const m = seconds % 60;
  seconds = (seconds - m) / 60;

  const h = seconds;

  let ret = String(s).padStart(2, "0");
  if (h === 0) {
    ret = String(m) + ":" + ret;
  } else {
    ret = String(m).padStart(2, "0") + ":" + ret;
    ret = String(h) + ":" + ret;
  }

  if (includeMS && ms > 0) {
    ret += "." + ms.toString().padStart(3, "0");
  }

  return neg ? "-" + ret : ret;
}

export function formatTimestampRange(
  start: number,
  end: number | undefined,
): string {
  if (end === undefined) {
    return secondsToTimestamp(start);
  }
  return `${secondsToTimestamp(start)}-${secondsToTimestamp(end)}`;
}

/**
 * Humanise a positive duration in seconds to a short English phrase
 * suitable for an "ETA" or similar reading-friendly display. Mirrors the
 * thresholds of moment.js's `moment.duration(...).humanize()` so prior
 * users feel at home, without pulling in moment as a dependency.
 *
 * Thresholds (cumulative):
 *   <   5s   → "a few seconds"
 *   <  45s   → "less than a minute"
 *   <  90s   → "a minute"
 *   <  45m   → "N minutes"
 *   <  90m   → "an hour"
 *   <  22h   → "N hours"
 *   <  36h   → "a day"
 *   <  26d   → "N days"
 *   < 320d   → "a month"
 *   else     → "N months"
 *
 * Use `secondsAsTimeString` instead when you need precise compact units
 * (e.g. scene durations); use this when "about how long" is what the
 * reader actually wants.
 */
export function humanizeSeconds(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return "";
  if (seconds < 5) return "a few seconds";
  if (seconds < 45) return "less than a minute";
  if (seconds < 90) return "a minute";
  const minutes = Math.round(seconds / 60);
  if (minutes < 45) return `${minutes} minutes`;
  if (minutes < 90) return "an hour";
  const hours = Math.round(minutes / 60);
  if (hours < 22) return `${hours} hours`;
  if (hours < 36) return "a day";
  const days = Math.round(hours / 24);
  if (days < 26) return `${days} days`;
  if (days < 320) return "a month";
  const months = Math.round(days / 30);
  return `${months} months`;
}

export function timestampToSeconds(
  v: string | null | undefined,
): number | null {
  if (!v) {
    return null;
  }

  const splits = v.split(":");

  if (splits.length > 3) {
    return null;
  }

  let secondsPart = splits[splits.length - 1];
  let msFrac = 0;
  if (secondsPart.includes(".")) {
    const secondsParts = secondsPart.split(".");
    if (secondsParts.length !== 2) {
      return null;
    }

    secondsPart = secondsParts[0];

    const msPart = parseInt(secondsParts[1], 10);
    if (Number.isNaN(msPart)) {
      return null;
    }

    msFrac = msPart / 1000;
  }

  let seconds = 0;
  let factor = 1;
  while (splits.length > 0) {
    const thisSplit = splits.pop();
    if (thisSplit === undefined) {
      return null;
    }

    const thisInt = parseInt(thisSplit, 10);
    if (Number.isNaN(thisInt)) {
      return null;
    }

    seconds += factor * thisInt;
    factor *= 60;
  }

  return seconds + msFrac;
}
