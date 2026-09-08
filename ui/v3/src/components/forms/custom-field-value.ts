/** Match the single-entity editor's decimal coercion; preserve other text. */
export function coerceCustomFieldValue(value: string): string | number {
  if (/^-?(?:0|(?:[1-9][0-9]*))(?:\.[0-9]+)?$/.test(value)) {
    const number = Number(value);
    // Non-finite numbers serialize to null in JSON, losing the entered value.
    if (Number.isFinite(number)) return number;
  }
  return value;
}
