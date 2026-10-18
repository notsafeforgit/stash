// Native controls belong to canonical wrappers. Semantic anchors, forms and
// media elements remain ordinary HTML; tests/fixtures are outside this rule.
const controls = new Set([
  "button",
  "input",
  "select",
  "textarea",
  "dialog",
  "details",
  "summary",
]);

export default {
  meta: {
    type: "problem",
    schema: [],
    messages: {
      control: "Use a shared UI component instead of a bare <{{tag}}>.",
      primitive:
        "Import Base UI primitives only in src/components/ui; use its shared wrappers here.",
      radix:
        "This application uses Base UI shadcn components; use the shared UI layer instead of Radix.",
      dialog: "Use a shared dialog instead of a blocking browser {{name}}().",
    },
  },
  create(context) {
    const filename = context.filename.replaceAll("\\", "/");
    const shared = filename.includes("/src/components/ui/");
    const textarea = filename.endsWith("/src/components/ui/textarea.tsx");
    function checkSource(node, source) {
      const name = source?.value;
      if (typeof name !== "string") return;
      if (name.startsWith("@radix-ui/")) {
        context.report({ node, messageId: "radix" });
      } else if (name.startsWith("@base-ui/") && !shared) {
        context.report({ node, messageId: "primitive" });
      }
    }
    return {
      JSXOpeningElement(node) {
        const tag = node.name.type === "JSXIdentifier" ? node.name.name : null;
        if (controls.has(tag) && !(textarea && tag === "textarea")) {
          context.report({ node, messageId: "control", data: { tag } });
        }
      },
      ImportDeclaration(node) {
        checkSource(node, node.source);
      },
      ExportNamedDeclaration(node) {
        checkSource(node, node.source);
      },
      ExportAllDeclaration(node) {
        checkSource(node, node.source);
      },
      ImportExpression(node) {
        checkSource(node, node.source);
      },
      CallExpression(node) {
        if (
          node.callee.type === "Identifier" &&
          node.callee.name === "require"
        ) {
          checkSource(node, node.arguments[0]);
        }
        const callee = node.callee;
        const name =
          callee.type === "Identifier"
            ? callee.name
            : callee.type === "MemberExpression" &&
                callee.object.type === "Identifier" &&
                ["window", "globalThis"].includes(callee.object.name)
              ? callee.computed
                ? callee.property.value
                : callee.property.name
              : null;
        if (["alert", "confirm", "prompt"].includes(name)) {
          context.report({ node, messageId: "dialog", data: { name } });
        }
      },
    };
  },
};
