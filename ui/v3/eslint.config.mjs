import parser from "@typescript-eslint/parser";
import reactHooks from "eslint-plugin-react-hooks";

// Biome owns formatting, general lint and hook dependencies. This narrow pass
// adds React's compiler-backed purity checks and protects shared type contracts.
export default [
  {
    ignores: [
      "**/*.gen.*",
      "src/core/generated-graphql.ts",
      "**/*.test.*",
      "**/*.d.ts",
    ],
  },
  {
    files: ["src/**/*.{ts,tsx}", "tests/browser/**/*.{ts,tsx}"],
    languageOptions: { parser, parserOptions: { ecmaFeatures: { jsx: true } } },
    plugins: { "react-hooks": reactHooks },
    rules: {
      "react-hooks/refs": "error",
      "react-hooks/purity": "error",
      "no-restricted-syntax": [
        "error",
        {
          selector: "TSAsExpression[typeAnnotation.type='TSNeverKeyword']",
          message:
            "Preserve the contract or isolate a validated external adapter; do not cast to never.",
        },
        {
          selector: "TSAsExpression > TSAsExpression",
          message:
            "A double assertion bypasses the contract. Narrow unknown input or use a structurally checked projection.",
        },
      ],
    },
  },
];
