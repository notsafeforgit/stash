/**
 * Force the browser to reload a stable stream URL after the underlying file
 * changes. Keep the query parameter before any Media Fragments suffix: a
 * cachebuster appended after `#t=` becomes part of the fragment value and can
 * invalidate a direct stream's resume timestamp.
 */
export function injectReloadNonce(src: string, nonce: number): string {
  if (nonce === 0) return src;
  const fragmentIndex = src.indexOf("#");
  const base = fragmentIndex >= 0 ? src.slice(0, fragmentIndex) : src;
  const fragment = fragmentIndex >= 0 ? src.slice(fragmentIndex) : "";
  const separator = base.includes("?") ? "&" : "?";
  return `${base}${separator}_r=${nonce}${fragment}`;
}
