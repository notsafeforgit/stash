import type { Plugin } from "vite";

/** Precache only the offline entry's dependency graph, plus its styles/fonts.
 * Keeping the regular application out avoids caching server-dependent shells
 * or eagerly downloading every settings page and locale on mobile. */
export function collectOfflineAssets(assets: Set<string>): Plugin {
  return {
    name: "stash-offline-assets",
    generateBundle(_options, bundle) {
      assets.clear();
      const visit = (name: string) => {
        if (assets.has(name)) return;
        assets.add(name);
        const chunk = bundle[name];
        if (chunk?.type === "chunk") {
          for (const dependency of [...chunk.imports, ...chunk.dynamicImports])
            visit(dependency);
        }
      };
      for (const [name, item] of Object.entries(bundle)) {
        if (
          item.type === "chunk" &&
          item.facadeModuleId?.endsWith("/offline.html")
        )
          visit(name);
        if (/\.(?:css|woff2)$/.test(name)) assets.add(name);
      }
      assets.add("offline.html");
      assets.add("apple-touch-icon.png");
    },
  };
}
