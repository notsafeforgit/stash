#!/usr/bin/env node
// Verifies that every `id: "..."` / `<FormattedMessage id="..." />` referenced
// from `src/` resolves to a string in `src/locales/en-GB.json`.
//
// Run as `npm run check-i18n`; exits non-zero on any missing key so it can
// gate `npm run validate`. Reports source file:line for each miss so you can
// jump straight to the offending call site.
//
// Dynamic IDs (template literals like `scrape.skip_${type}`) can't be
// statically checked; the script lists them so you can audit by hand. They
// don't fail the run on their own.

import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(__dirname, "..");
const SRC = join(ROOT, "src");
const LOCALE_PATH = join(ROOT, "src/locales/en-GB.json");

// ── Locale loading ────────────────────────────────────────────────────────────

/** Build a Set of every flattened dotted key whose leaf is a string. */
function flattenKeys(obj, prefix = "", out = new Set()) {
  for (const [k, v] of Object.entries(obj)) {
    const path = prefix ? `${prefix}.${k}` : k;
    if (typeof v === "string") {
      out.add(path);
    } else if (v && typeof v === "object") {
      flattenKeys(v, path, out);
    }
  }
  return out;
}

const locale = JSON.parse(readFileSync(LOCALE_PATH, "utf8"));
const localeKeys = flattenKeys(locale);

// ── Source scanning ───────────────────────────────────────────────────────────

function* walk(dir) {
  for (const entry of readdirSync(dir)) {
    if (entry === "node_modules" || entry.startsWith(".")) continue;
    const full = join(dir, entry);
    const s = statSync(full);
    if (s.isDirectory()) {
      yield* walk(full);
    } else if (/\.(ts|tsx)$/.test(entry)) {
      yield full;
    }
  }
}

// Matches:
//   id: "foo.bar"          (intl.formatMessage object literal)
//   id: 'foo.bar'
//   <FormattedMessage id="foo.bar" .../>
//   <FormattedMessage id='foo.bar' .../>
// And the dynamic `id: \`foo.${x}\`` form, captured separately so we can
// surface it without forcing a hard failure.
const STATIC_ID_RE =
  /\b(?:id\s*:\s*|id\s*=\s*\{?\s*)["']([a-zA-Z_][\w.]*)["']/g;
const DYNAMIC_ID_RE =
  /\b(?:id\s*:\s*|id\s*=\s*\{?\s*)`([^`]*\$\{[^`]*}[^`]*)`/g;

// Limit lookups to call sites that are actually intl IDs. The regex above is
// loose to keep it simple, but `id:` shows up in plenty of unrelated places
// (route params, GraphQL types, dnd-kit sortables, …). We narrow with a small
// allowlist of context keywords on the same line: any of these means it's
// almost certainly a react-intl reference.
const INTL_CONTEXT_RE =
  /\b(?:formatMessage|FormattedMessage|defaultMessage|useIntl|intl|MessageDescriptor)\b/;

const RELATIVE = (p) => p.replace(`${ROOT}/`, "");

const missing = []; // { id, file, line }
const dynamicHits = []; // { expr, file, line }

for (const file of walk(SRC)) {
  // Skip the locale files themselves, generated graphql, and tests.
  if (file.includes("/locales/")) continue;
  if (file.endsWith(".d.ts")) continue;

  const content = readFileSync(file, "utf8");
  const lines = content.split("\n");

  // Build a small per-file context window: the full text of each line plus a
  // few neighbours either side, so we can sniff for `formatMessage` /
  // `FormattedMessage` even when the `id:` line itself doesn't carry the
  // context word (e.g. multiline `intl.formatMessage({\n  id: "x",\n  ... })`).
  function lineNumberAt(offset) {
    // 1-indexed line number for a character offset into `content`.
    let n = 1;
    for (let i = 0; i < offset && i < content.length; i++) {
      if (content[i] === "\n") n++;
    }
    return n;
  }
  function contextWindow(lineNo, span = 3) {
    const lo = Math.max(0, lineNo - 1 - span);
    const hi = Math.min(lines.length, lineNo - 1 + span + 1);
    return lines.slice(lo, hi).join("\n");
  }

  for (const m of content.matchAll(STATIC_ID_RE)) {
    const id = m[1];
    const lineNo = lineNumberAt(m.index);
    const ctx = contextWindow(lineNo);
    if (!INTL_CONTEXT_RE.test(ctx)) continue;
    if (!localeKeys.has(id)) {
      missing.push({ id, file: RELATIVE(file), line: lineNo });
    }
  }

  for (const m of content.matchAll(DYNAMIC_ID_RE)) {
    const expr = m[1];
    const lineNo = lineNumberAt(m.index);
    const ctx = contextWindow(lineNo);
    if (!INTL_CONTEXT_RE.test(ctx)) continue;
    dynamicHits.push({ expr, file: RELATIVE(file), line: lineNo });
  }
}

// ── Output ────────────────────────────────────────────────────────────────────

if (dynamicHits.length > 0) {
  console.log(
    `i18n: ${dynamicHits.length} dynamic id(s) (template literal) — verify the prefix family is in en-GB.json:`,
  );
  for (const { expr, file, line } of dynamicHits) {
    console.log(`  ${file}:${line}  \`${expr}\``);
  }
  console.log("");
}

if (missing.length === 0) {
  console.log(
    `i18n: OK — every static id resolves to a key in ${RELATIVE(LOCALE_PATH)}.`,
  );
  process.exit(0);
}

// Group misses by id so a widely-used missing key shows once with all its
// call sites.
const byId = new Map();
for (const { id, file, line } of missing) {
  if (!byId.has(id)) byId.set(id, []);
  byId.get(id).push(`${file}:${line}`);
}

console.error(
  `i18n: ${byId.size} missing key(s) — add to ${RELATIVE(LOCALE_PATH)}:\n`,
);
for (const [id, sites] of [...byId.entries()].sort()) {
  console.error(`  ${id}`);
  for (const s of sites) console.error(`      ${s}`);
}
process.exit(1);
