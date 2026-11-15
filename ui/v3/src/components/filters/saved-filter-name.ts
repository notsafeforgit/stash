export type NamedSavedFilter = { id: string; name: string };

/**
 * Finds a saved filter whose name matches case-insensitively. The save dialog
 * offers the match for overwrite; rename rejects it. `excludeId` lets a rename
 * keep its own name in another case.
 */
export function findSavedFilterByName<T extends NamedSavedFilter>(
  filters: readonly T[],
  name: string,
  locale: string,
  excludeId?: string,
): T | undefined {
  const wanted = name.trim().toLocaleLowerCase(locale);
  return filters.find(
    (filter) =>
      filter.id !== excludeId &&
      filter.name.toLocaleLowerCase(locale) === wanted,
  );
}

export type SavedFilterRenameCheck =
  | { status: "empty" }
  | { status: "unchanged" }
  | { status: "duplicate"; match: NamedSavedFilter }
  | { status: "valid"; name: string };

export function checkSavedFilterRename<T extends NamedSavedFilter>(
  filters: readonly T[],
  current: NamedSavedFilter,
  input: string,
  locale: string,
): SavedFilterRenameCheck {
  const name = input.trim();
  if (!name) return { status: "empty" };
  if (name === current.name) return { status: "unchanged" };
  const match = findSavedFilterByName(filters, name, locale, current.id);
  if (match) return { status: "duplicate", match };
  return { status: "valid", name };
}
