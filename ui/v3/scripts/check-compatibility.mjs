#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  buildSchema,
  findBreakingChanges,
  findDangerousChanges,
  NoUnusedFragmentsRule,
  parse,
  specifiedRules,
  validate,
} from "graphql";

const root = fileURLToPath(new URL("../../../", import.meta.url));
const { revision } = JSON.parse(
  readFileSync(
    new URL("./compatibility-baseline.json", import.meta.url),
    "utf8",
  ),
);
const git = (...args) =>
  execFileSync("git", args, {
    cwd: root,
    encoding: "utf8",
    maxBuffer: 16 * 1024 * 1024,
  });
const oldFile = (path) => git("show", `${revision}:${path}`);
const currentFile = (path) => readFileSync(resolve(root, path), "utf8");
const oldPaths = git("ls-tree", "-r", "--name-only", revision)
  .trim()
  .split("\n");
function files(path) {
  return readdirSync(resolve(root, path), { withFileTypes: true }).flatMap(
    (entry) => {
      const name = `${path}/${entry.name}`;
      return entry.isDirectory() ? files(name) : [name];
    },
  );
}

const schemas = oldPaths.filter(
  (p) => p.startsWith("graphql/schema/") && p.endsWith(".graphql"),
);
const legacySchema = buildSchema(schemas.map(oldFile).join("\n"));
const schema = buildSchema(
  files("graphql/schema")
    .filter((p) => p.endsWith(".graphql"))
    .map(currentFile)
    .join("\n"),
);
const failures = findBreakingChanges(legacySchema, schema).map(
  (change) => change.description,
);
// Added enum values are intentionally additive; changed argument defaults can
// silently change requests from old clients even when validation still passes.
failures.push(
  ...findDangerousChanges(legacySchema, schema)
    .filter((change) => change.type.includes("DEFAULT_VALUE"))
    .map((change) => change.description),
);

const operations = oldPaths.filter(
  (p) =>
    p.startsWith("ui/v2.5/graphql/") &&
    p.endsWith(".graphql") &&
    !p.endsWith("client-schema.graphql"),
);
const document = parse(operations.map(oldFile).join("\n"));
const rules = specifiedRules.filter((rule) => rule !== NoUnusedFragmentsRule);
failures.push(
  ...validate(schema, document, rules).map((error) => error.message),
);

const primaryMigration = (path) =>
  /^pkg\/sqlite\/migrations\/\d.*\.sql$/.test(path);
const baselineMigrations = oldPaths.filter(primaryMigration);
const currentMigrations = files("pkg/sqlite/migrations").filter(
  primaryMigration,
);
for (const path of new Set([...baselineMigrations, ...currentMigrations])) {
  if (
    !baselineMigrations.includes(path) ||
    !currentMigrations.includes(path) ||
    oldFile(path) !== currentFile(path)
  ) {
    failures.push(
      `Upstream migration changed: ${path}. Use fork sidecars or explicitly update the upstream baseline.`,
    );
  }
}
const version = (contents) =>
  contents.match(/var appSchemaVersion uint = (\d+)/)?.[1];
if (
  !version(oldFile("pkg/sqlite/database.go")) ||
  version(oldFile("pkg/sqlite/database.go")) !==
    version(currentFile("pkg/sqlite/database.go"))
) {
  failures.push(
    "The primary database version differs from the v2.5 rollback baseline.",
  );
}

if (failures.length) {
  console.error(
    `v2.5 compatibility failed against ${revision}:\n${failures.map((message) => `- ${message}`).join("\n")}`,
  );
  process.exitCode = 1;
} else {
  console.log(
    `v2.5 compatibility passed: additive schema, ${operations.length} legacy operation files, ${baselineMigrations.length} unchanged primary migrations (${revision.slice(0, 12)}).`,
  );
}
