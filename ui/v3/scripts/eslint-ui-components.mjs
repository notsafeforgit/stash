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

// web.css matches `[aria-label]:has(> svg:only-child)`, and :only-child ignores
// text nodes, so an icon beside visible text is squeezed to icon width.
function iconWithText(children) {
  const elements = children.filter((child) => child.type === "JSXElement");
  if (elements.length !== 1) return false;
  const name = elements[0].openingElement.name;
  if (name.type !== "JSXIdentifier" || !/^(svg|\w+Icon)$/.test(name.name))
    return false;
  return children.some((child) =>
    child.type === "JSXText"
      ? child.value.trim() !== ""
      : child.type === "JSXExpressionContainer" &&
        [
          "CallExpression",
          "Identifier",
          "Literal",
          "MemberExpression",
          "TemplateLiteral",
        ].includes(child.expression.type),
  );
}

export default {
  meta: {
    type: "problem",
    schema: [],
    messages: {
      control: "Use a shared UI component instead of a bare <{{tag}}>.",
      primitive:
        "Import ui-sdk only in src/components/ui; use its shared wrappers here.",
      foreign:
        "This application builds its UI on ui-sdk; use the shared UI layer instead of {{name}}.",
      dialog: "Use a shared dialog instead of a blocking browser {{name}}().",
      labelledText:
        "This control shows text, so that text is its name: remove aria-label. web.css sizes a labelled control whose only element is an icon as a square icon button.",
    },
  },
  create(context) {
    const filename = context.filename.replaceAll("\\", "/");
    const shared = filename.includes("/src/components/ui/");
    const textarea = filename.endsWith("/src/components/ui/textarea.tsx");
    function checkSource(node, source) {
      const name = source?.value;
      if (typeof name !== "string") return;
      const foreign = ["@radix-ui/", "@base-ui/"].find((prefix) =>
        name.startsWith(prefix),
      );
      if (foreign) {
        context.report({
          node,
          messageId: "foreign",
          data: { name: foreign.slice(0, -1) },
        });
      } else if (name.startsWith("@ui-sdk/") && !shared) {
        context.report({ node, messageId: "primitive" });
      }
    }
    return {
      JSXOpeningElement(node) {
        const tag = node.name.type === "JSXIdentifier" ? node.name.name : null;
        if (controls.has(tag) && !(textarea && tag === "textarea")) {
          context.report({ node, messageId: "control", data: { tag } });
        }
        const labelled = node.attributes.some(
          (attribute) =>
            attribute.type === "JSXAttribute" &&
            attribute.name.name === "aria-label",
        );
        if (labelled && iconWithText(node.parent.children)) {
          context.report({ node, messageId: "labelledText" });
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
