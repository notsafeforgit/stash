import { RuleTester } from "eslint";
import parser from "@typescript-eslint/parser";
import { describe, it } from "vitest";
import rule from "./eslint-ui-components.mjs";

RuleTester.describe = describe;
RuleTester.it = it;
RuleTester.itOnly = it.only;
const filename = "/project/src/components/settings/example.tsx";
const shared = "/project/src/components/ui/example.tsx";
const tester = new RuleTester({
  languageOptions: { parser, parserOptions: { ecmaFeatures: { jsx: true } } },
});
tester.run("shared UI boundaries", rule, {
  valid: [
    {
      filename,
      code: 'import { Button } from "@/components/ui/button"; const x = <Button />;',
    },
    {
      filename,
      code: 'const x = <form><a href="/settings">Settings</a><video /><label>Name</label></form>;',
    },
    {
      filename: shared,
      code: 'import { Button } from "@ui-sdk/react/button"; const x = <Button />;',
    },
    {
      filename: "/project/src/components/ui/textarea.tsx",
      code: "const x = <textarea />;",
    },
    {
      filename: "C:\\project\\src\\components\\ui\\textarea.tsx",
      code: "const x = <textarea />;",
    },
  ],
  invalid: [
    ...[
      "button",
      "input",
      "select",
      "textarea",
      "dialog",
      "details",
      "summary",
    ].map((tag) => ({
      filename,
      code: `const x = <${tag} />;`,
      errors: [{ messageId: "control" }],
    })),
    {
      filename: shared,
      code: "const x = <button />;",
      errors: [{ messageId: "control" }],
    },
    {
      filename: "/project/src/components/ui/textarea.tsx",
      code: "const x = <input />;",
      errors: [{ messageId: "control" }],
    },
    ...[
      'import { Button as RawButton } from "@ui-sdk/react/button";',
      'export { Button } from "@ui-sdk/react/button";',
      'export * from "@ui-sdk/core";',
      'const x = import("@ui-sdk/react/button");',
      'const x = require("@ui-sdk/react/button");',
    ].map((code) => ({ filename, code, errors: [{ messageId: "primitive" }] })),
    ...[
      'import * as Dialog from "@radix-ui/react-dialog";',
      'import { Button } from "@base-ui/react/button";',
    ].map((code) => ({
      filename: shared,
      code,
      errors: [{ messageId: "foreign" }],
    })),
    ...[
      'window.confirm("Delete?")',
      'alert("Failed")',
      'globalThis["prompt"]("Name")',
    ].map((code) => ({
      filename,
      code,
      errors: [{ messageId: "dialog" }],
    })),
  ],
});
