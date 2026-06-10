import { MetaRow } from "src/components/detail/meta-row";

interface CustomFieldsRowsProps {
  values: { [key: string]: unknown } | null | undefined;
}

function valueToString(v: unknown): string {
  if (v === null || v === undefined) return "";
  if (typeof v === "string") return v;
  if (typeof v === "number") return v.toString();
  if (typeof v === "boolean") return v ? "true" : "false";
  if (Array.isArray(v)) return v.join(", ");
  return JSON.stringify(v);
}

/**
 * Renders one MetaRow per entry in `values`. Returns null when the map
 * is empty so the caller can drop it directly into a <dl> without
 * worrying about empty sections.
 */
export function CustomFieldsRows({ values }: CustomFieldsRowsProps) {
  if (!values) return null;
  const entries = Object.entries(values);
  if (entries.length === 0) return null;

  // Sort for stable display order, matching v2.5.
  entries.sort(([a], [b]) => a.localeCompare(b));

  return (
    <>
      {entries.map(([key, value]) => (
        <MetaRow key={key} label={key}>
          <span className="whitespace-pre-wrap break-words">
            {valueToString(value)}
          </span>
        </MetaRow>
      ))}
    </>
  );
}
