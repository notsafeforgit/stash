interface Identified {
  id: string;
}

const TINTS = [
  "bg-sky-500/10 dark:bg-sky-400/15",
  "bg-emerald-500/10 dark:bg-emerald-400/15",
  "bg-amber-500/10 dark:bg-amber-400/15",
  "bg-rose-500/10 dark:bg-rose-400/15",
  "bg-violet-500/10 dark:bg-violet-400/15",
  "bg-cyan-500/10 dark:bg-cyan-400/15",
  "bg-lime-500/10 dark:bg-lime-400/15",
  "bg-orange-500/10 dark:bg-orange-400/15",
  "bg-fuchsia-500/10 dark:bg-fuchsia-400/15",
  "bg-teal-500/10 dark:bg-teal-400/15",
] as const;

export function compareText(a: string, b: string, locale: string): number {
  return a.localeCompare(b, locale, { numeric: true, sensitivity: "base" });
}
function stableHash(value: string): number {
  let hash = 0;
  for (const char of value) hash = (hash * 31 + char.charCodeAt(0)) >>> 0;
  return hash;
}

/** Equal values share a tint within each group; distinct values cycle through
 * the palette. Hashing varies the initial colour between unrelated groups. */
export function groupValueTints<T extends Identified>(
  groups: readonly (readonly T[])[],
  column: string,
  valueKey: (item: T) => string,
): Map<string, string> {
  const result = new Map<string, string>();
  for (const group of groups) {
    const counts = new Map<string, number>();
    for (const item of group) {
      const key = valueKey(item);
      counts.set(key, (counts.get(key) ?? 0) + 1);
    }
    if (![...counts.values()].some((count) => count > 1)) continue;
    const offset = stableHash(
      `${group.map((item) => item.id).join(":")}:${column}`,
    );
    const tints = new Map<string, string>();
    for (const item of group) {
      const key = valueKey(item);
      const tint =
        tints.get(key) ??
        TINTS[(offset + tints.size) % TINTS.length] ??
        TINTS[0];
      tints.set(key, tint);
      result.set(item.id, tint);
    }
  }
  return result;
}

export function selectAllButRetained<T extends Identified>(
  groups: readonly T[][],
  keep: (group: T[]) => T | undefined,
  allow: (group: T[]) => boolean = () => true,
): T[] {
  return groups.flatMap((group) => {
    if (!allow(group)) return [];
    const retained = keep(group);
    if (!retained || !group.some((item) => item.id === retained.id)) return [];
    return group.filter((item) => item.id !== retained.id);
  });
}
