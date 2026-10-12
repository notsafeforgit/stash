export interface DurationFile {
  duration?: number | null;
}

export interface DurationSource extends DurationFile {
  file?: DurationFile | null;
}

function validDuration(value: number | null | undefined): value is number {
  return typeof value === "number" && Number.isFinite(value) && value > 0;
}

// Provider durations are useful even when the provider has no fingerprints.
// Compare actual local files; do not turn a similar duration into a hash match.
export function compareSceneDuration(
  source: DurationSource,
  localFiles: readonly DurationFile[] = [],
) {
  const remote = [source.duration, source.file?.duration].find(validDuration);
  if (remote === undefined) return undefined;
  const durations = localFiles
    .map((file) => file.duration)
    .filter(validDuration);
  const local = durations.reduce<number | undefined>(
    (closest, duration) =>
      closest === undefined ||
      Math.abs(remote - duration) < Math.abs(remote - closest)
        ? duration
        : closest,
    undefined,
  );
  return {
    remote,
    local,
    localCount: durations.length,
    difference: local === undefined ? undefined : Math.abs(remote - local),
    matches: local === undefined ? undefined : Math.abs(remote - local) <= 5,
  };
}
