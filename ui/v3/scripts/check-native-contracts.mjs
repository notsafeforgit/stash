#!/usr/bin/env node
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
const read = (path) => readFileSync(resolve(root, path), "utf8");
function files(path) {
  return readdirSync(resolve(root, path), { withFileTypes: true }).flatMap(
    (entry) => {
      const name = `${path}/${entry.name}`;
      return entry.isDirectory() ? files(name) : [name];
    },
  );
}

// The extension contract has only two opaque scalars and its own root fields.
// Do not drag the old upstream application schema into this retained boundary.
const pluginSchema = (source) =>
  buildSchema(`
    scalar Any
    scalar Map
    type Query { _contract: Boolean }
    type Mutation { _contract: Boolean }
    ${source}
  `);
const previousPlugin = pluginSchema(
  read("ui/v3/scripts/native-plugin-contract.graphql"),
);
const currentPlugin = pluginSchema(
  read("graphql/schema/types/plugin-v3.graphql"),
);
const failures = findBreakingChanges(previousPlugin, currentPlugin).map(
  (change) => change.description,
);
failures.push(
  ...findDangerousChanges(previousPlugin, currentPlugin)
    .filter((change) => change.type.includes("DEFAULT_VALUE"))
    .map((change) => change.description),
);

const schema = buildSchema(
  files("graphql/schema")
    .filter((path) => path.endsWith(".graphql"))
    .map(read)
    .join("\n"),
);
const operations = files("ui/v3/graphql").filter(
  (path) =>
    path.endsWith(".graphql") && !path.endsWith("client-schema.graphql"),
);
const document = parse(operations.map(read).join("\n"));
const rules = specifiedRules.filter((rule) => rule !== NoUnusedFragmentsRule);
failures.push(
  ...validate(schema, document, rules).map((error) => error.message),
);

if (failures.length) {
  console.error(
    `Native contract validation failed:\n${failures.map((message) => `- ${message}`).join("\n")}`,
  );
  process.exitCode = 1;
} else {
  console.log(
    `Native contracts passed: retained v3 extension API and ${operations.length} current application operation files.`,
  );
}
