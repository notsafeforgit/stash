import type { CodegenConfig } from "@graphql-codegen/cli";

const config: CodegenConfig = {
  schema: [
    "../../graphql/schema/**/*.graphql",
    "graphql/client-schema.graphql",
  ],
  config: {
    strictScalars: true,
    scalars: {
      ID: "string",
      Time: "string",
      Timestamp: "string",
      Map: "{ [key: string]: unknown }",
      BoolMap: "{ [key: string]: boolean }",
      PluginConfigMap: "{ [id: string]: { [key: string]: unknown } }",
      Any: "unknown",
      Int64: "number",
      Upload: "File",
      UIConfig: 'import("src/core/config").IUIConfig',
      SavedObjectFilter:
        'import("src/models/list-filter/types").SavedObjectFilter',
      SavedFilterAST: 'import("src/models/list-filter/types").SavedFilterAST',
      SavedUIOptions: 'import("src/models/list-filter/types").SavedUIOptions',
    },
    onFieldTypeConflict: (_existing: unknown, other: unknown) => other,
  },
  documents: "graphql/**/*.graphql",
  generates: {
    "src/core/generated-schema.ts": {
      plugins: ["typescript"],
    },
    "src/core/generated-graphql.ts": {
      plugins: [
        "time",
        { add: { content: 'export * from "./generated-schema";' } },
        "typescript-operations",
        "typed-document-node",
      ],
      config: {
        importSchemaTypesFrom: "src/core/generated-schema.ts",
        nonOptionalTypename: true,
        skipTypeNameForRoot: true,
      },
    },
  },
};

export default config;
