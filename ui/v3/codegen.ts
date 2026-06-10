import type { CodegenConfig } from "@graphql-codegen/cli";

const config: CodegenConfig = {
  schema: [
    "../../graphql/schema/**/*.graphql",
    "graphql/client-schema.graphql",
  ],
  config: {
    onFieldTypeConflict: (_existing: unknown, other: unknown) => other,
  },
  documents: "graphql/**/*.graphql",
  generates: {
    "src/core/generated-graphql.ts": {
      plugins: [
        "time",
        "typescript",
        "typescript-operations",
        "typed-document-node",
      ],
      config: {
        strictScalars: true,
        scalars: {
          Time: "string",
          Timestamp: "string",
          Map: "{ [key: string]: unknown }",
          BoolMap: "{ [key: string]: boolean }",
          PluginConfigMap: "{ [id: string]: { [key: string]: unknown } }",
          Any: "unknown",
          Int64: "number",
          Upload: "File",
          UIConfig: "src/core/config#IUIConfig",
          SavedObjectFilter: "src/models/list-filter/types#SavedObjectFilter",
          SavedFilterAST: "src/models/list-filter/types#SavedFilterAST",
          SavedUIOptions: "src/models/list-filter/types#SavedUIOptions",
        },
      },
    },
  },
};

export default config;
