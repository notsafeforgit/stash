import type { ProviderMetadataSelectionInput } from "@/core/generated-graphql";
import type { ScrapeSource } from "./use-available-scrapers";

type Provider = Pick<ProviderMetadataSelectionInput, "endpoint" | "remote_id">;
export interface ProviderPatch {
  provider: Provider;
  /** Form field names, before conversion to the mutation input. */
  fields: string[];
  mergedFields: string[];
}

const sceneFields = new Set([
  "title",
  "code",
  "details",
  "director",
  "date",
  "production_date",
  "urls",
  "studio",
  "performers",
  "tags",
  "groups",
]);
const performerFields = new Set([
  "name",
  "disambiguation",
  "gender",
  "birthdate",
  "death_date",
  "country",
  "ethnicity",
  "eye_color",
  "hair_color",
  "height_cm",
  "weight",
  "measurements",
  "penis_length",
  "circumcised",
  "fake_tits",
  "career_start",
  "career_end",
  "tattoos",
  "piercings",
  "details",
  "urls",
  "aliases",
  "tags",
  "image",
]);
const mergeableFields = new Set([
  "urls",
  "aliases",
  "performers",
  "tags",
  "groups",
]);

export function providerFromScrape(
  source: ScrapeSource | null | undefined,
  remoteId: string | null | undefined,
): Provider | undefined {
  if (source?.kind !== "stashBox") return undefined;
  if (!remoteId?.trim()) throw new Error("missing_provider_remote_id");
  return { endpoint: source.endpoint, remote_id: remoteId };
}

export function providerPatch(
  kind: "scene" | "performer",
  source: ScrapeSource | null | undefined,
  remoteId: string | null | undefined,
  patch: object,
  mergeMode: (field: string) => string,
): ProviderPatch | undefined {
  const provider = providerFromScrape(source, remoteId);
  if (!provider) return undefined;
  const allowed = kind === "scene" ? sceneFields : performerFields;
  const fields = Object.keys(patch).filter((field) => allowed.has(field));
  return {
    provider,
    fields,
    mergedFields: fields.filter(
      (field) => mergeableFields.has(field) && mergeMode(field) === "merge",
    ),
  };
}

/** Related entities are created now, independently of saving the parent form.
 * Their own remote IDs identify the source, never the parent scene's ID. */
export function providerCreationFields(
  source: ScrapeSource | null | undefined,
  item: { remote_site_id?: string | null; name?: string | null },
  name: string,
) {
  const provider = providerFromScrape(source, item.remote_site_id);
  if (!provider) return {};
  return {
    stash_ids: [{ endpoint: provider.endpoint, stash_id: provider.remote_id }],
    provider_metadata:
      name.trim() === item.name?.trim()
        ? [{ ...provider, fields: ["name"] }]
        : [],
  };
}

/** Pending choices are confined to this form instance. Observing any subsequent
 * change drops that field's attribution permanently, even if it is typed back
 * to its imported value. A new Apply can explicitly attribute it again. */
export class ProviderMetadataDraft {
  private fields = new Map<
    string,
    { snapshot: string | undefined; providers: Map<string, Provider> }
  >();
  private applying = false;

  clear() {
    this.fields.clear();
  }

  observe(values: object) {
    if (this.applying) return;
    const current = values as Record<string, unknown>;
    for (const [field, saved] of this.fields) {
      if (JSON.stringify(current[field]) !== saved.snapshot)
        this.fields.delete(field);
    }
  }

  apply(
    patch: object,
    selection: ProviderPatch | undefined,
    write: () => void,
  ) {
    this.applying = true;
    try {
      write();
      for (const [field, value] of Object.entries(patch)) {
        const previous = this.fields.get(field);
        this.fields.delete(field);
        if (!selection?.fields.includes(field)) continue;
        const providers = selection.mergedFields.includes(field)
          ? new Map(previous?.providers)
          : new Map<string, Provider>();
        const provider = selection.provider;
        providers.set(
          JSON.stringify([provider.endpoint, provider.remote_id]),
          provider,
        );
        this.fields.set(field, { snapshot: JSON.stringify(value), providers });
      }
    } finally {
      this.applying = false;
    }
  }

  selections(values: object): ProviderMetadataSelectionInput[] {
    this.observe(values);
    const imports = new Map<string, ProviderMetadataSelectionInput>();
    for (const [field, saved] of this.fields) {
      for (const [key, provider] of saved.providers) {
        const entry = imports.get(key) ?? { ...provider, fields: [] };
        entry.fields.push(field === "height_cm" ? "height" : field);
        imports.set(key, entry);
      }
    }
    return [...imports.values()].map((entry) => ({
      ...entry,
      fields: entry.fields.sort(),
    }));
  }
}
