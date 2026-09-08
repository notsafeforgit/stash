const utf8Prefix = "u.";

/** Version the byte encoding: older URLs stored JSON as Latin-1 bytes. */
export function encodeURLJSON(value: unknown): string {
  const json = JSON.stringify(value);
  if (json === undefined) throw new Error("Cannot encode undefined JSON");
  const bytes = new TextEncoder().encode(json);
  const binary = Array.from(bytes, (byte) => String.fromCharCode(byte)).join(
    "",
  );
  return (
    utf8Prefix +
    btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")
  );
}

export function decodeURLJSON(value: string): unknown {
  const utf8 = value.startsWith(utf8Prefix);
  const encoded = utf8 ? value.slice(utf8Prefix.length) : value;
  const binary = atob(encoded.replace(/-/g, "+").replace(/_/g, "/"));
  const json = utf8
    ? new TextDecoder("utf-8", { fatal: true }).decode(
        Uint8Array.from(binary, (char) => char.charCodeAt(0)),
      )
    : binary;
  return JSON.parse(json);
}
