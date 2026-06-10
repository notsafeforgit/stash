import type React from "react";
import {
  ArrowDown10,
  ArrowDownWideNarrow,
  ArrowDownZA,
  ArrowUp01,
  ArrowUpAZ,
  ArrowUpNarrowWide,
} from "lucide-react";
import { SortDirectionEnum } from "src/core/generated-graphql";

type SortFieldKind = "numeric" | "lexicographic" | "other";

// Sort fields are well-known strings. Lexicographic and "other" are small
// closed sets — anything else (counts, dates, sizes, durations, ratings,
// boolean-ish stats, etc.) we treat as numeric, which renders the 0-1 / 1-0
// icons.
const LEXICOGRAPHIC_FIELDS = new Set<string>([
  "name",
  "title",
  "path",
  "code",
  "director",
  "studio",
  "measurements",
]);

const OTHER_FIELDS = new Set<string>([
  "random",
  "organized",
  "interactive",
  "favorite",
]);

function getSortFieldKind(value: string): SortFieldKind {
  if (OTHER_FIELDS.has(value)) return "other";
  if (LEXICOGRAPHIC_FIELDS.has(value)) return "lexicographic";
  return "numeric";
}

export function getSortDirectionIcon(
  sortValue: string | undefined,
  direction: SortDirectionEnum,
): React.ComponentType<{ size?: number; className?: string }> {
  const asc = direction === SortDirectionEnum.Asc;
  const kind = sortValue ? getSortFieldKind(sortValue) : "other";
  if (kind === "numeric") return asc ? ArrowUp01 : ArrowDown10;
  if (kind === "lexicographic") return asc ? ArrowUpAZ : ArrowDownZA;
  return asc ? ArrowUpNarrowWide : ArrowDownWideNarrow;
}
