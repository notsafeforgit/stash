// Adapted from stashapp/stash 92ea03bd36b7 (PR #7259).
// Package indexes describe supported studios in `scrapes` and supported URL
// fragments in `*_urls`. These are search hints, not executable regexes.
export interface SearchablePackage {
  package_id: string;
  name: string;
  metadata?: Record<string, unknown> | null;
}

export interface PackageMatch {
  via?: string;
}

function strings(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.filter(
    (item): item is string => typeof item === "string" && !!item.trim(),
  );
}

export function matchPackage(
  pkg: SearchablePackage,
  filter: string,
): PackageMatch | undefined {
  const query = filter.trim().toLowerCase();
  if (
    !query ||
    pkg.name.toLowerCase().includes(query) ||
    pkg.package_id.toLowerCase().includes(query)
  ) {
    return {};
  }

  const metadata = pkg.metadata ?? {};
  const studio = strings(metadata.scrapes).find((name) =>
    name.toLowerCase().includes(query),
  );
  if (studio) return { via: studio };

  for (const [key, value] of Object.entries(metadata)) {
    if (!key.endsWith("_urls")) continue;
    const pattern = strings(value).find((url) => {
      const normalized = url.trim().toLowerCase();
      return normalized.includes(query) || query.includes(normalized);
    });
    if (pattern) return { via: pattern };
  }
  return undefined;
}
