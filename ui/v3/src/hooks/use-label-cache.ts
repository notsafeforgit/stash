import { useCallback, useState } from "react";

type LabelEntries = readonly (readonly [string, string])[];

function mergeLabels(
  previous: ReadonlyMap<string, string>,
  entries: LabelEntries,
) {
  let next: Map<string, string> | undefined;
  for (const [id, label] of new Map(entries)) {
    if (previous.get(id) === label) continue;
    next ??= new Map(previous);
    next.set(id, label);
  }
  return next ?? previous;
}

/** Retain names for selected IDs after search results change. React owns the
 * snapshot, so a suspended search cannot change the visible selection's labels. */
export function useLabelCache(entries: LabelEntries) {
  const [known, setKnown] = useState<ReadonlyMap<string, string>>(
    () => new Map(entries),
  );
  const next = mergeLabels(known, entries);
  if (next !== known) setKnown(next);
  const remember = useCallback(
    (values: LabelEntries) =>
      setKnown((previous) => mergeLabels(previous, values)),
    [],
  );
  return [next, remember] as const;
}
